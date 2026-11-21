package xapi

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/advnpzn/xlui/internal/terminal"
	"github.com/advnpzn/xlui/internal/xen"
)

func usableRef(ref string) bool {
	return ref != "" && strings.TrimSpace(ref) == ref && ref != "OpaqueRef:NULL"
}

func shutdownEligible(ref string, vm vmRecord, id int) bool {
	return vm.PowerState == "Running" && vm.ControlDomain != nil && !*vm.ControlDomain &&
		xen.GuestUUID(vm.UUID) && id > 0 && usableRef(ref) && usableRef(vm.Resident) &&
		usableRef(vm.Metrics) && slices.Contains(vm.Allowed, "clean_shutdown")
}

// The pool owns a VM across boots. Bind a power action to the running guest
// observed on one host, including its metrics start time and session epoch.
func (m *Monitor) shutdownInstance(ref string, vm vmRecord, id int, start time.Time) string {
	if !shutdownEligible(ref, vm, id) || start.IsZero() {
		return ""
	}
	return fmt.Sprintf("%d/%q/%q/%q/%d/%s", m.epoch, ref, vm.Resident, vm.Metrics, id, start.Format(time.RFC3339Nano))
}

// A halted guest has no live boot or host. Pin the persistent object in the
// current pool session and require the pool to advertise start.
func (m *Monitor) startTarget(ref string, vm vmRecord) string {
	if vm.PowerState != "Halted" || *vm.ControlDomain || !usableRef(ref) || !xen.GuestUUID(vm.UUID) || !slices.Contains(vm.Allowed, "start") {
		return ""
	}
	return fmt.Sprintf("%d/%q", m.epoch, ref)
}

// Caller owns the monitor lock for the inventory, metrics and session reads.
func (m *Monitor) powerTarget(ctx context.Context, c xen.Change) (string, error) {
	if c.Instance == "" || !xen.GuestUUID(c.Target.UUID) {
		return "", errors.New("select a guest with verified power action support")
	}
	records, err := m.records(ctx)
	if err != nil {
		return "", err
	}
	for ref, vm := range records {
		if !strings.EqualFold(vm.UUID, c.Target.UUID) || *vm.Name != c.Name {
			continue
		}
		if c.Action == xen.StartGuest {
			if c.Target.ID == -1 && m.startTarget(ref, vm) == c.Instance {
				return ref, nil
			}
			break
		}
		id, err := strconv.Atoi(vm.DomainID.String())
		if err != nil || id != c.Target.ID || !shutdownEligible(ref, vm, id) {
			break
		}
		var live liveMetrics
		if err := m.rpc.call(ctx, "VM_metrics.get_record", []string{m.session, vm.Metrics}, &live, inventoryLimit); err != nil {
			return "", err
		}
		if m.shutdownInstance(ref, vm, id, bootTime(live.Start)) == c.Instance {
			return ref, nil
		}
		break
	}
	return "", errors.New("guest identity, state or power capability changed; refresh the inventory and select the guest again")
}

func (m *Monitor) preparePower(ctx context.Context, c xen.Change) (xen.Plan, error) {
	if _, err := m.powerTarget(ctx, c); err != nil {
		return xen.Plan{}, err
	}
	description := fmt.Sprintf("Gracefully shut down %q\nUUID %s · domain %d\nThe guest OS will be asked to stop. Its services will become unavailable.", terminal.SingleLine(c.Name), c.Target.UUID, c.Target.ID)
	method, action, goal := "VM.clean_shutdown", "shutdown", "Halted"
	if c.Action == xen.StartGuest {
		description = fmt.Sprintf("Start halted VM %q\nUUID %s\nThe pool will select a host and boot the VM with its configured devices.", terminal.SingleLine(c.Name), c.Target.UUID)
		method, action, goal = "VM.start", "start", "Running"
	}
	return xen.NewPlan(description, func(ctx context.Context) (string, error) {
		m.writeMu.Lock()
		defer m.writeMu.Unlock()
		m.mu.Lock()
		ref, err := m.powerTarget(ctx, c)
		session := m.session
		m.mu.Unlock()
		if err != nil {
			return "", err
		}

		// Power operations can wait on the pool. Allow the sampler to continue
		// while keeping other xlui writes serialized through verification.
		params := []any{session, ref}
		args := params
		if c.Action == xen.StartGuest {
			args = append(args, false, false) // start_paused, force
		}
		if err := m.powerRPC.call(ctx, method, args, nil, sessionLimit); err != nil {
			return "", fmt.Errorf("%s outcome unverified; inspect the pool before retrying: %w", action, err)
		}
		var got vmRecord
		if err := m.powerRPC.call(ctx, "VM.get_record", params, &got, inventoryLimit); err != nil {
			return "", fmt.Errorf("%s accepted; guest state could not be verified: %w", action, err)
		}
		if !strings.EqualFold(got.UUID, c.Target.UUID) || got.Template == nil || *got.Template ||
			got.Snapshot == nil || *got.Snapshot || got.ControlDomain == nil || *got.ControlDomain {
			return "", fmt.Errorf("%s accepted; guest identity could not be verified", action)
		}
		if got.PowerState != goal {
			return "", fmt.Errorf("%s accepted; %s state has not been observed. Inspect the pool before retrying", action, goal)
		}
		return fmt.Sprintf("Verified VM %s is %s.", c.Target.UUID, goal), nil
	}), nil
}
