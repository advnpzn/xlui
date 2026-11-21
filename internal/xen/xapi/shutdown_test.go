package xapi_test

import (
	"context"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/advnpzn/xlui/internal/xen"
	"github.com/advnpzn/xlui/internal/xen/xapi"
)

const (
	shutdownVM      = "OpaqueRef:guest"
	shutdownMetrics = "OpaqueRef:metrics"
	shutdownUUID    = "11111111-1111-4111-8111-111111111111"
	shutdownStart   = "20260101T10:00:00Z"
)

func runningGuest() map[string]any {
	return map[string]any{
		"uuid": shutdownUUID, "name_label": "bench", "domid": "7", "power_state": "Running",
		"is_control_domain": false, "is_a_template": false, "is_a_snapshot": false,
		"metrics": shutdownMetrics, "resident_on": "OpaqueRef:host", "allowed_operations": []string{"clean_shutdown"},
	}
}

func poolVMs(ref string, vm map[string]any) rpcStep {
	return rpcStep{method: "VM.get_all_records", result: map[string]any{ref: vm}}
}

func startMetric(start string) rpcStep {
	return rpcStep{method: "VM_metrics.get_record", params: []any{testSession, shutdownMetrics}, result: map[string]any{"start_time": start}}
}

// The operator selects the guest from a real inventory sample. The fixture checks
// every subsequent RPC, including the session and opaque reference on the write.
func shutdownInventory(vm map[string]any, start string) []rpcStep {
	return []rpcStep{
		{method: "session.login_with_password", result: testSession},
		poolVMs(shutdownVM, vm),
		{method: "VM_metrics.get_all_records", result: map[string]any{shutdownMetrics: map[string]any{"start_time": start}}},
		{method: "rrd_updates"},
	}
}

func prepareObservedShutdown(t *testing.T, steps []rpcStep) (*xapi.Monitor, xen.Plan) {
	t.Helper()
	server := xenServer(t, append(steps, rpcStep{method: "session.logout"})...)
	m, err := xapi.NewMonitor(server.URL, testUser, testPassword, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	domains, err := m.Read(t.Context())
	if err != nil || len(domains) != 1 || domains[0].ShutdownInstance == "" {
		t.Fatalf("missing guest shutdown evidence: %+v, %v", domains, err)
	}
	d := domains[0]
	plan, err := m.Prepare(t.Context(), xen.Change{Action: xen.GracefulShutdown, Name: d.Name, Target: d.Identity(), Instance: d.ShutdownInstance})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.Description, shutdownUUID) || !strings.Contains(plan.Description, d.Name) {
		t.Fatalf("shutdown preview did not identify the guest: %q", plan.Description)
	}
	return m, plan
}

func TestGracefulShutdownFromInventory(t *testing.T) {
	before := runningGuest()
	halted := maps.Clone(before)
	halted["power_state"] = "Halted"
	halted["domid"] = "-1"
	wrongVM := maps.Clone(halted)
	wrongVM["uuid"] = "22222222-2222-4222-8222-222222222222"
	revoked := maps.Clone(before)
	revoked["allowed_operations"] = []string{"hard_shutdown"}
	migrated := maps.Clone(before)
	migrated["resident_on"] = "OpaqueRef:other-host"
	normal := []rpcStep{poolVMs(shutdownVM, before), startMetric(shutdownStart)}
	dispatch := rpcStep{method: "VM.clean_shutdown", params: []any{testSession, shutdownVM}}
	lostReply := dispatch
	lostReply.responseID = "unrelated"
	read := func(vm map[string]any) rpcStep {
		return rpcStep{method: "VM.get_record", params: []any{testSession, shutdownVM}, result: vm}
	}
	for _, tc := range []struct {
		name     string
		confirm  []rpcStep
		dispatch []rpcStep
		success  bool
		message  string
	}{
		{"halted guest observed", normal, []rpcStep{dispatch, read(halted)}, true, "Halted"},
		{"reboot reused domid", []rpcStep{poolVMs(shutdownVM, before), startMetric("2026-01-02T10:00:00Z")}, nil, false, "changed"},
		{"shutdown permission revoked", []rpcStep{poolVMs(shutdownVM, revoked)}, nil, false, "changed"},
		{"guest migrated", []rpcStep{poolVMs(shutdownVM, migrated), startMetric(shutdownStart)}, nil, false, "changed"},
		{"VM reference replaced", []rpcStep{poolVMs("OpaqueRef:replacement", before), startMetric(shutdownStart)}, nil, false, "changed"},
		{"reply lost", normal, []rpcStep{lostReply}, false, "outcome unverified"},
		{"still running", normal, []rpcStep{dispatch, read(before)}, false, "Halted state has not been observed"},
		{"readback for another VM", normal, []rpcStep{dispatch, read(wrongVM)}, false, "shutdown accepted; guest identity could not be verified"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			steps := shutdownInventory(before, shutdownStart)
			steps = append(steps, normal...)
			steps = append(steps, tc.confirm...)
			steps = append(steps, tc.dispatch...)
			_, plan := prepareObservedShutdown(t, steps)
			msg, err := plan.Apply(t.Context())
			if (err == nil) != tc.success {
				t.Fatalf("shutdown returned %q, %v", msg, err)
			}
			if err != nil {
				msg = err.Error()
			}
			if !strings.Contains(msg, tc.message) {
				t.Fatalf("shutdown message %q, want %q", msg, tc.message)
			}
			if _, err := plan.Apply(t.Context()); err == nil {
				t.Fatal("shutdown plan could be applied twice")
			}
		})
	}
}

func TestReauthenticationInvalidatesShutdownPreview(t *testing.T) {
	vm := runningGuest()
	steps := shutdownInventory(vm, shutdownStart)
	steps = append(steps, poolVMs(shutdownVM, vm), startMetric(shutdownStart))
	steps = append(steps, rpcStep{method: "VM.get_all_records", fault: map[string]any{"code": 1, "message": "SESSION_INVALID"}})
	// Even if the reconnected pool returns identical references, the old
	// confirmation belongs to the previous authenticated session.
	steps = append(steps, rpcStep{method: "session.login_with_password", result: testSession}, poolVMs(shutdownVM, vm), startMetric(shutdownStart))
	m, plan := prepareObservedShutdown(t, steps)
	if _, err := m.Read(t.Context()); err == nil {
		t.Fatal("expected expired session")
	}
	if _, err := plan.Apply(t.Context()); err == nil {
		t.Fatal("shutdown confirmation survived reauthentication")
	}
}

// A pool can block the synchronous shutdown RPC while the guest OS stops.
// Inventory must continue on the same monitor during that wait.
func TestGracefulShutdownAllowsInventoryWhileWaiting(t *testing.T) {
	vm := runningGuest()
	steps := shutdownInventory(vm, shutdownStart)
	fresh := []rpcStep{poolVMs(shutdownVM, vm), startMetric(shutdownStart)}
	steps = append(steps, fresh...) // preview
	steps = append(steps, fresh...) // confirm
	started := make(chan struct{})
	release := make(chan struct{})
	steps = append(steps, rpcStep{method: "VM.clean_shutdown", params: []any{testSession, shutdownVM}, onCall: func(ctx context.Context) {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
		}
	}})
	steps = append(steps, shutdownInventory(vm, shutdownStart)[1:]...) // concurrent sample
	halted := maps.Clone(vm)
	halted["power_state"] = "Halted"
	halted["domid"] = "-1"
	steps = append(steps, rpcStep{method: "VM.get_record", params: []any{testSession, shutdownVM}, result: halted})
	m, plan := prepareObservedShutdown(t, steps)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := plan.Apply(ctx)
		done <- err
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("shutdown was never dispatched")
	}
	domains, err := m.Read(ctx)
	close(release)
	if err != nil || len(domains) != 1 || domains[0].State != "Running" {
		t.Fatalf("inventory during shutdown: %+v, %v", domains, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestInventoryWithholdsShutdownWhenEvidenceIsMissing(t *testing.T) {
	for _, tc := range []struct {
		field string
		value any
		start string
	}{
		{"allowed_operations", []string{"hard_shutdown"}, shutdownStart},
		{"resident_on", "OpaqueRef:NULL", shutdownStart},
		{"domid", "0", shutdownStart},
		{"is_control_domain", true, shutdownStart},
		{"power_state", "Paused", shutdownStart},
		{"metrics", "OpaqueRef:NULL", shutdownStart},
		{"start_time", nil, "1970-01-01T00:00:00Z"},
	} {
		t.Run(tc.field, func(t *testing.T) {
			vm := runningGuest()
			if tc.field != "start_time" {
				vm[tc.field] = tc.value
			}
			steps := shutdownInventory(vm, tc.start)
			server := xenServer(t, append(steps, rpcStep{method: "session.logout"})...)
			m, err := xapi.NewMonitor(server.URL, testUser, testPassword, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			domains, err := m.Read(t.Context())
			if err != nil || len(domains) != 1 || domains[0].ShutdownInstance != "" {
				t.Fatalf("ineligible inventory: %+v, %v", domains, err)
			}
		})
	}
}
