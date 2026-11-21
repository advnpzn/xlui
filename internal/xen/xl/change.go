package xl

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"

	"github.com/advnpzn/xlui/internal/xen"
)

//go:embed definition.sh
var definitionScript string

type definitionRunner interface {
	Runner
	RunInput(context.Context, io.Reader, io.Writer, io.Writer, string, ...string) error
}
type definitions struct {
	runner definitionRunner
	dir    string
}

func GuestChanges(r definitionRunner) xen.Prepare { return (definitions{r, "/etc/xen"}).prepare }

type definition struct {
	name, uuid, kernel, initrd, cmdline string
	cpus, mib                           int64
}

var bootPath = regexp.MustCompile(`^/[A-Za-z0-9_./+@-]+$`)

func (d definition) validate() error {
	if err := xen.CheckName(d.name); err != nil {
		return err
	}
	if !xen.GuestUUID(d.uuid) {
		return errors.New("invalid guest UUID")
	}
	if d.cpus < 1 || d.cpus > 8192 || d.mib < 1 || d.mib > 1<<40 {
		return errors.New("enter a positive, supported vCPU count and memory size in MiB")
	}
	for _, p := range []string{d.kernel, d.initrd} {
		if !bootPath.MatchString(p) || len(p) > 4096 || path.Clean(p) != p {
			return errors.New("enter absolute kernel and initramfs file paths on Dom0 using letters, digits or /._+@-")
		}
	}
	if len(d.cmdline) > 2048 || strings.IndexFunc(d.cmdline, func(r rune) bool { return r < ' ' || r > '~' || r == '"' || r == '\\' }) >= 0 {
		return errors.New("kernel arguments must be printable ASCII without quotes or backslashes")
	}
	return nil
}
func (d definition) encode() string {
	return fmt.Sprintf("# xlui guest definition v1\nname = %q\nuuid = %q\nkernel = %q\nramdisk = %q\ncmdline = %q\nvcpus = %d\nmemory = %d\n", d.name, d.uuid, d.kernel, d.initrd, d.cmdline, d.cpus, d.mib)
}
func (m definitions) file(ctx context.Context, mode, name, digest, data string, boot ...string) (string, error) {
	out := cappedOutput{limit: 16384}
	if mode == "inspect-boot" {
		out.limit = 1 << 20
	}
	diagnostic := cappedOutput{limit: 1024}
	args := append([]string{"-c", definitionScript, "xlui", m.dir, name, mode, digest}, boot...)
	err := m.runner.RunInput(ctx, strings.NewReader(data), &out, &diagnostic, "sh", args...)
	if err != nil {
		return "", fmt.Errorf("Dom0 definition: %w (%q)", err, strings.TrimSpace(diagnostic.buf.String()))
	}
	if out.truncated {
		return "", fmt.Errorf("definition output exceeds %d bytes", out.limit)
	}
	return out.buf.String(), nil
}
func (m definitions) prepare(ctx context.Context, c xen.Change) (xen.Plan, error) {
	if err := xen.CheckName(c.Name); err != nil {
		return xen.Plan{}, err
	}
	if c.Action == xen.BootDefinition {
		return m.prepareBoot(ctx, c.Name)
	}
	var d definition
	var digest, summary, data string
	create := c.Action == xen.CreateGuest
	mode := "edit"
	if create {
		mode = "create"
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			return xen.Plan{}, err
		}
		id[6] = id[6]&15 | 64
		id[8] = id[8]&63 | 128
		d = definition{name: c.Name, uuid: fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:]), kernel: c.Kernel, initrd: c.Initrd, cmdline: c.Cmdline, cpus: c.VCPUs, mib: c.MemoryMiB}
		if err := d.validate(); err != nil {
			return xen.Plan{}, err
		}
		data = d.encode()
		summary = fmt.Sprintf("Create and start %s\nUUID %s\n%d vCPUs, %d MiB\nKernel %s\nInitramfs %s\nBoot arguments %s", d.name, d.uuid, d.cpus, d.mib, d.kernel, d.initrd, d.cmdline)
	} else {
		raw, err := m.file(ctx, "read", c.Name, "", "")
		if err != nil {
			return xen.Plan{}, err
		}
		d.name = c.Name
		d.uuid, data, summary, err = editBoot(raw, c)
		if err != nil {
			return xen.Plan{}, err
		}
		digest = fmt.Sprintf("%x", sha256.Sum256([]byte(raw)))
		summary = fmt.Sprintf("Edit %s\nUUID %s\n%s\nEffective on the next creation from this file.", d.name, d.uuid, summary)
	}

	if err := m.checkTarget(ctx, d.name, d.uuid, c.Target, create); err != nil {
		return xen.Plan{}, err
	}
	var boot []string
	check := "check-edit"
	if create {
		check = "check-new"
		boot = []string{d.kernel, d.initrd}
	}
	if _, err := m.file(ctx, check, d.name, digest, data, boot...); err != nil {
		return xen.Plan{}, err
	}
	return xen.NewPlan(summary+"\nDefinition "+path.Join(m.dir, d.name+".cfg"), func(ctx context.Context) (string, error) {
		if err := m.checkTarget(ctx, d.name, d.uuid, c.Target, create); err != nil {
			return "", err
		}
		if _, err := m.file(ctx, mode, d.name, digest, data, boot...); err != nil {
			return "", fmt.Errorf("check the definition and inventory before retrying: %w", err)
		}
		got, err := m.file(ctx, "read", d.name, "", "")
		if err != nil || got != data {
			return "", errors.New("write dispatched; saved definition could not be verified")
		}
		if create {
			return m.verifyStarted(ctx, d.name, d.uuid)
		}
		return "Boot definition saved and verified for " + d.name, nil
	}), nil
}

func (m definitions) checkTarget(ctx context.Context, name, uuid string, target xen.Identity, create bool) error {
	rows, err := ReadDomains(ctx, m.runner)
	if err != nil {
		return err
	}
	found := false
	for _, dom := range rows {
		if dom.Name != name && !strings.EqualFold(dom.UUID, uuid) {
			continue
		}
		if create || dom.ID == 0 || dom.Name != name || !strings.EqualFold(dom.UUID, uuid) || (target.UUID != "" && dom.ID != target.ID) {
			return errors.New("live guest identity or name conflicts with the proposed change")
		}
		found = true
	}
	if !create && target.UUID != "" && !found {
		return errors.New("selected guest instance is no longer present")
	}
	return nil
}
