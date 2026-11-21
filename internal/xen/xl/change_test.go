package xl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/advnpzn/xlui/internal/transport"
	"github.com/advnpzn/xlui/internal/xen"
)

func TestDom0CreateAndEdit(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("SIM_DOMAINS", filepath.Join(dir, "running"))
	// Exercise the actual host script, transports, temporary files and rename.
	tool := `#!/bin/sh
set -eu
case "$1" in
list) echo 'Name ID Mem VCPUs State Time(s) UUID Reason-Code Security Label'
      if [ -f "$SIM_DOMAINS" ]; then cat "$SIM_DOMAINS"; fi ;;
create)
    if [ "$2" = -n ]; then
        test "$3" = -q && test -s "$4"
        uuid=$(sed -n 's/^uuid = "\(.*\)"/\1/p' "$4")
        printf '{"c_info":{"name":"worker","uuid":"%s"}}\n' "$uuid"
        exit
    fi
    test "$2" = -q
    uuid=$(sed -n 's/^uuid = "\(.*\)"/\1/p' "$3")
    printf '%s\n' "worker 7 512 2 r----- 0.2 $uuid - -" > "$SIM_DOMAINS" ;;
*) exit 4 ;;
esac`
	if err := os.WriteFile(filepath.Join(dir, "xl"), []byte(tool), 0700); err != nil {
		t.Fatal(err)
	}
	m := definitions{transport.Runner{}, dir}
	create := xen.Change{Action: xen.CreateGuest, Name: "worker", Kernel: "/boot/Image", Initrd: "/boot/root.cpio", Cmdline: "console=hvc0", VCPUs: 2, MemoryMiB: 512}
	create.Kernel = filepath.Join(dir, "Image")
	create.Initrd = filepath.Join(dir, "initrd")
	for _, p := range []string{create.Kernel, create.Initrd} {
		if err := os.WriteFile(p, []byte("test fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := m.prepare(t.Context(), create)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "worker.cfg")
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("preview published a guest definition")
	}
	if _, err := plan.Apply(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := plan.Apply(t.Context()); err == nil {
		t.Fatal("reused confirmation")
	}
	if _, err := m.prepare(t.Context(), create); err == nil {
		t.Fatal("created duplicate name")
	}
	rows, err := ReadDomains(t.Context(), m.runner)
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	req := xen.Change{Action: xen.SetMemory, Name: "worker", Target: rows[0].Identity(), MemoryMiB: 768}
	edit, err := m.prepare(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(edit.Description, "512 → 768") {
		t.Fatal(edit.Description)
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("disk = [ 'format=raw,vdev=xvda,target=/images/w.raw' ] # retain\n")
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := edit.Apply(t.Context()); err == nil {
		t.Fatal("overwrote a concurrent edit")
	}
	edit, err = m.prepare(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := edit.Apply(t.Context()); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(file)
	raw, _ := os.ReadFile(file)
	if info.Mode().Perm() != 0600 || !strings.Contains(string(raw), "memory = 768\n") || !strings.Contains(string(raw), "# retain\n") {
		t.Fatalf("unexpected saved definition %q", raw)
	}
	req.Target.ID++
	if _, err := m.prepare(t.Context(), req); err == nil {
		t.Fatal("accepted a different instance")
	}
	if err := os.Remove(os.Getenv("SIM_DOMAINS")); err != nil {
		t.Fatal(err)
	}
	boot := xen.Change{Action: xen.BootDefinition, Name: "worker"}
	p, err := m.prepare(t.Context(), boot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, append(raw, []byte("# changed during review\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Apply(t.Context()); err == nil {
		t.Fatal("booted a changed definition")
	}
	p, err = m.prepare(t.Context(), boot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Apply(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := m.prepare(t.Context(), boot); err == nil {
		t.Fatal("started a running guest a second time")
	}

}

func TestBootEditPreservesUserFile(t *testing.T) {
	uuid := "12345678-1234-1234-1234-123456789abc"
	raw := "name = 'worker'  # guest\nuuid=\"" + uuid + "\"\ndisk = [ 'a;#b' ]\n  vcpus = 1 # count\nmemory=1024"
	c := xen.Change{Action: xen.SetVCPUs, Name: "worker", VCPUs: 2}
	got, updated, _, err := editBoot(raw, c)
	if err != nil || got != uuid || updated != strings.Replace(raw, "1 # count", "2 # count", 1) {
		t.Fatalf("edit %q %v", updated, err)
	}
	for _, src := range []string{raw + "\nvcpus=4", raw + "\nfoo = 'continued", strings.ReplaceAll(raw, "name = 'worker'", "name='other'"), strings.ReplaceAll(raw, uuid, "00000000-0000-0000-0000-000000000000")} {
		if _, _, _, err := editBoot(src, c); err == nil {
			t.Fatal("accepted ambiguous input", src)
		}
	}
}
