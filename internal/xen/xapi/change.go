package xapi

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/advnpzn/xlui/internal/terminal"
	"github.com/advnpzn/xlui/internal/xen"
)

// The opaque reference pins a pool object even when names or domids are reused.
type poolEdit struct {
	settings        bootSettings
	vbds, vifs      []string
	ref, uuid, name string
	before, method  string
	values          []any
}

func (m *Monitor) inspectChange(ctx context.Context, c xen.Change) (poolEdit, error) {
	var edit poolEdit
	records, err := m.records(ctx)
	if err != nil {
		return edit, err
	}
	uuid := c.Target.UUID
	if c.Action == xen.CreateGuest {
		if !validLabel(c.Name) {
			return edit, errors.New("enter a printable VM name of 1–255 bytes without surrounding spaces")
		}
		uuid = c.SourceUUID
	}
	if !xen.GuestUUID(uuid) {
		return edit, errors.New("enter a valid guest UUID")
	}
	var vm vmRecord
	for ref, r := range records {
		if c.Action == xen.CreateGuest && *r.Name == c.Name {
			return edit, errors.New("a VM with this name already exists")
		}
		if strings.EqualFold(r.UUID, uuid) {
			edit.ref = ref
			vm = r
		}
	}
	if edit.ref == "" || edit.ref == "OpaqueRef:NULL" || *vm.ControlDomain {
		return edit, errors.New("select a guest VM in this pool")
	}
	edit.uuid = vm.UUID
	edit.name = *vm.Name
	if c.Action == xen.RenameGuest {
		// A label belongs to the persistent VM object across power transitions.
		if edit.name != c.Name {
			return edit, errors.New("guest name changed; review its current name")
		}
		if !validLabel(c.NewName) || c.NewName == c.Name {
			return edit, errors.New("enter a different printable VM name of 1–255 bytes without surrounding spaces")
		}
		edit.before = fmt.Sprintf("Rename VM %q → %q\nUUID %s\nThe pool display name changes immediately.", terminal.SingleLine(edit.name), c.NewName, edit.uuid)
		edit.method = "VM.set_name_label"
		edit.values = []any{c.NewName}
		return edit, nil
	}
	if vm.PowerState != "Halted" {
		return edit, errors.New("select a halted guest VM in this pool")
	}
	edit.settings = vm.bootSettings
	if c.Action != xen.CreateGuest && (c.Target.ID != -1 || c.Name != edit.name) {
		return edit, errors.New("selected guest changed; select it again")
	}
	switch c.Action {
	case xen.CreateGuest:
		edit.vbds, edit.vifs = vm.VBDs, vm.VIFs
		slices.Sort(edit.vbds)
		slices.Sort(edit.vifs)
		edit.before = fmt.Sprintf("Clone halted VM %s\nSource UUID %s\nNew VM %s\nSource devices: %d virtual block devices, %d network interfaces.\nThe pool clones the VM and its disks. The new VM remains halted.", terminal.SingleLine(edit.name), uuid, c.Name, len(edit.vbds), len(edit.vifs))
		edit.method = "VM.clone"
		edit.values = []any{c.Name}
	case xen.SetVCPUs:
		max, e1 := vm.VCPUMax.Int64()
		old, e2 := vm.Startup.Int64()
		if e1 != nil || e2 != nil || old < 1 || c.VCPUs < 1 || c.VCPUs > max {
			return edit, fmt.Errorf("startup vCPUs must be within the VM's reported maximum (%s)", vm.VCPUMax)
		}
		if c.VCPUs == old {
			return edit, errors.New("startup vCPUs already match")
		}
		edit.before = fmt.Sprintf("Startup vCPUs: %d → %d (maximum %d)", old, c.VCPUs, max)
		edit.method = "VM.set_VCPUs_at_startup"
		edit.values = []any{c.VCPUs}
	case xen.SetMemory:
		lo, e1 := vm.StaticMin.Int64()
		hi, e2 := vm.StaticMax.Int64()
		a, e3 := vm.DynamicMin.Int64()
		b, e4 := vm.DynamicMax.Int64()
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil || lo < 0 || hi < lo || a < lo || b < a || b > hi {
			return edit, errors.New("pool returned incomplete memory limits")
		}
		if c.MemoryMiB <= 0 || c.MemoryMiB > math.MaxInt64>>20 {
			return edit, errors.New("enter a positive memory size in MiB")
		}
		size := c.MemoryMiB << 20
		if size < lo || size > hi {
			return edit, fmt.Errorf("memory must fit the VM's static limits (%d to %d bytes)", lo, hi)
		}
		if size == a && size == b {
			return edit, errors.New("memory range already matches")
		}
		edit.before = fmt.Sprintf("Dynamic memory range: %d–%d bytes → fixed %d MiB\nStatic limits: %d–%d bytes", a, b, c.MemoryMiB, lo, hi)
		edit.method = "VM.set_memory_dynamic_range"
		edit.values = []any{size, size}
	default:
		return edit, errors.New("unsupported XenAPI guest change")
	}
	return edit, nil
}

func validLabel(name string) bool {
	return name != "" && len(name) <= 255 && utf8.ValidString(name) && strings.TrimSpace(name) == name &&
		strings.IndexFunc(name, func(r rune) bool { return !unicode.IsPrint(r) }) < 0
}

func (m *Monitor) Prepare(ctx context.Context, c xen.Change) (xen.Plan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c.Action == xen.GracefulShutdown {
		return m.prepareShutdown(ctx, c)
	}
	edit, err := m.inspectChange(ctx, c)
	if err != nil {
		return xen.Plan{}, err
	}
	var summary string
	if c.Action == xen.CreateGuest || c.Action == xen.RenameGuest {
		summary = edit.before
	} else {
		summary = fmt.Sprintf("Edit %s\nUUID %s\n%s\nEffective at next boot.", terminal.SingleLine(edit.name), edit.uuid, edit.before)
	}
	return xen.NewPlan(summary, func(ctx context.Context) (string, error) {
		m.writeMu.Lock()
		defer m.writeMu.Unlock()
		m.mu.Lock()
		defer m.mu.Unlock()
		fresh, err := m.inspectChange(ctx, c)
		if err != nil {
			return "", err
		}
		if fresh.ref != edit.ref || fresh.name != edit.name || fresh.settings != edit.settings || !slices.Equal(fresh.vbds, edit.vbds) || !slices.Equal(fresh.vifs, edit.vifs) {
			return "", errors.New("VM changed since preview; review current settings")
		}
		args := append([]any{m.session, edit.ref}, edit.values...)
		ref := edit.ref
		var result any
		if c.Action == xen.CreateGuest {
			result = &ref
		}
		if err := m.rpc.call(ctx, edit.method, args, result, sessionLimit); err != nil {
			return "", fmt.Errorf("write could not be verified; inspect the pool before retrying: %w", err)
		}
		var got vmRecord
		if ref == "" || ref == "OpaqueRef:NULL" || m.rpc.call(ctx, "VM.get_record", []string{m.session, ref}, &got, inventoryLimit) != nil {
			return "", errors.New("write accepted; pool record could not be verified")
		}
		if got.Name == nil || got.ControlDomain == nil || *got.ControlDomain || got.Template == nil || *got.Template || got.Snapshot == nil || *got.Snapshot {
			return "", errors.New("write accepted; resulting guest identity could not be verified")
		}
		if c.Action == xen.CreateGuest {
			if *got.Name == c.Name && xen.GuestUUID(got.UUID) && !strings.EqualFold(got.UUID, edit.uuid) && got.PowerState == "Halted" {
				return "Created halted VM " + c.Name + " · " + got.UUID, nil
			}
		} else if got.UUID == edit.uuid {
			if c.Action == xen.RenameGuest && *got.Name == c.NewName {
				return "VM name saved and verified: " + c.NewName, nil
			}
			if c.Action == xen.SetVCPUs {
				v, err := got.Startup.Int64()
				if err == nil && v == c.VCPUs {
					return "Startup vCPUs saved and verified.", nil
				}
			}
			if c.Action == xen.SetMemory {
				a, e1 := got.DynamicMin.Int64()
				b, e2 := got.DynamicMax.Int64()
				if e1 == nil && e2 == nil && a == b && a == c.MemoryMiB<<20 {
					return "Memory range saved and verified.", nil
				}
			}
		}
		return "", errors.New("write accepted; resulting settings did not match the preview. Inspect the pool before retrying")
	}), nil
}
