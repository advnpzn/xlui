package xapi

import (
	"context"
	"encoding/json"
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
func (m *Monitor) shutdownInstance(ref string, vm vmRecord, id int, start json.RawMessage) string {
	if !shutdownEligible(ref, vm, id) {
		return ""
	}
	t := bootTime(start)
	if t.IsZero() {
		return ""
	}
	return fmt.Sprintf("%d/%q/%q/%q/%d/%s", m.epoch, ref, vm.Resident, vm.Metrics, id, t.Format(time.RFC3339Nano))
}

// Caller owns the monitor lock for the inventory, metrics and session reads.
func (m *Monitor) shutdownTarget(ctx context.Context, c xen.Change) (string, error) {
	if c.Instance == "" || c.Target.ID <= 0 || !xen.GuestUUID(c.Target.UUID) {
		return "", errors.New("select a running guest with verified shutdown support")
	}
	records, err := m.records(ctx)
	if err != nil {
		return "", err
	}
	for ref, vm := range records {
		if !strings.EqualFold(vm.UUID, c.Target.UUID) {
			continue
		}
		id, err := strconv.Atoi(vm.DomainID.String())
		if err != nil || id != c.Target.ID || *vm.Name != c.Name || !shutdownEligible(ref, vm, id) {
			break
		}
		var live liveMetrics
		if err := m.rpc.call(ctx, "VM_metrics.get_record", []string{m.session, vm.Metrics}, &live, inventoryLimit); err != nil {
			return "", err
		}
		if m.shutdownInstance(ref, vm, id, live.Start) == c.Instance {
			return ref, nil
		}
		break
	}
	return "", errors.New("guest instance or shutdown capability changed; refresh the inventory and select the guest again")
}

func (m *Monitor) prepareShutdown(ctx context.Context, c xen.Change) (xen.Plan, error) {
	if _, err := m.shutdownTarget(ctx, c); err != nil {
		return xen.Plan{}, err
	}
	description := fmt.Sprintf("Gracefully shut down %q\nUUID %s · domain %d\nThe guest OS will be asked to stop. Its services will become unavailable.", terminal.SingleLine(c.Name), c.Target.UUID, c.Target.ID)
	return xen.NewPlan(description, func(ctx context.Context) (string, error) {
		m.writeMu.Lock()
		defer m.writeMu.Unlock()
		m.mu.Lock()
		ref, err := m.shutdownTarget(ctx, c)
		session := m.session
		m.mu.Unlock()
		if err != nil {
			return "", err
		}

		// clean_shutdown can wait on the guest. Allow the sampler to continue
		// while keeping other xlui writes serialized through verification.
		params := []string{session, ref}
		if err := m.shutdownRPC.call(ctx, "VM.clean_shutdown", params, nil, sessionLimit); err != nil {
			return "", fmt.Errorf("shutdown outcome unverified; inspect the pool before retrying: %w", err)
		}
		var got vmRecord
		if err := m.shutdownRPC.call(ctx, "VM.get_record", params, &got, inventoryLimit); err != nil {
			return "", fmt.Errorf("shutdown accepted; guest state could not be verified: %w", err)
		}
		if !strings.EqualFold(got.UUID, c.Target.UUID) || got.Template == nil || *got.Template ||
			got.Snapshot == nil || *got.Snapshot || got.ControlDomain == nil || *got.ControlDomain {
			return "", errors.New("shutdown accepted; guest identity could not be verified")
		}
		if got.PowerState != "Halted" {
			return "", errors.New("shutdown accepted; Halted state has not been observed. Inspect the pool before retrying")
		}
		return "Graceful shutdown verified. VM " + c.Target.UUID + " is Halted.", nil
	}), nil
}
