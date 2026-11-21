package xapi_test

import (
	"maps"
	"strings"
	"testing"

	"github.com/advnpzn/xlui/internal/xen"
	"github.com/advnpzn/xlui/internal/xen/xapi"
)

func TestStartFromPoolInventory(t *testing.T) {
	running := runningGuest()
	halted := maps.Clone(running)
	halted["power_state"], halted["domid"] = "Halted", "-1"
	halted["allowed_operations"] = []string{"start"}
	halted["resident_on"] = "OpaqueRef:NULL"
	changeField := func(vm map[string]any, key string, value any) map[string]any {
		changed := maps.Clone(vm)
		changed[key] = value
		return changed
	}
	observed := poolVMs(shutdownVM, halted)
	start := rpcStep{method: "VM.start", params: []any{testSession, shutdownVM, false, false}}
	read := func(vm map[string]any) rpcStep {
		return rpcStep{method: "VM.get_record", params: []any{testSession, shutdownVM}, result: vm}
	}
	ambiguous := start
	ambiguous.responseID = "other-request"
	for _, tc := range []struct {
		name    string
		calls   []rpcStep
		message string
		ok      bool
	}{
		{"running observed", []rpcStep{observed, start, read(running)}, "Running", true},
		{"already started", []rpcStep{poolVMs(shutdownVM, running)}, "", false},
		{"permission revoked", []rpcStep{poolVMs(shutdownVM, changeField(halted, "allowed_operations", []string{}))}, "", false},
		{"VM ref replaced", []rpcStep{poolVMs("other-ref", halted)}, "", false},
		{"renamed", []rpcStep{poolVMs(shutdownVM, changeField(halted, "name_label", "other"))}, "", false},
		{"protected VM", []rpcStep{poolVMs(shutdownVM, changeField(halted, "is_control_domain", true))}, "", false},
		{"template", []rpcStep{poolVMs(shutdownVM, changeField(halted, "is_a_template", true))}, "", false},
		{"snapshot", []rpcStep{poolVMs(shutdownVM, changeField(halted, "is_a_snapshot", true))}, "", false},
		{"reply lost", []rpcStep{observed, ambiguous}, "outcome unverified", false},
		{"remains halted", []rpcStep{observed, start, read(halted)}, "accepted", false},
		{"another VM running", []rpcStep{observed, start, read(changeField(running, "uuid", "22222222-2222-4222-8222-222222222222"))}, "accepted", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, plan := prepareObservedStart(t, halted, tc.calls)
			msg, err := plan.Apply(t.Context())
			if (err == nil) != tc.ok {
				t.Fatalf("start returned %q, %v", msg, err)
			}
			if err != nil {
				msg = err.Error()
			}
			if !strings.Contains(msg, tc.message) {
				t.Fatalf("start result %q, want %q", msg, tc.message)
			}
			if _, err := plan.Apply(t.Context()); err == nil {
				t.Fatal("start dispatched twice")
			}
		})
	}

	t.Run("reauthenticated session", func(t *testing.T) {
		// The new session can return the same token and VM reference.
		// Its confirmation must still require a fresh preview.
		m, plan := prepareObservedStart(t, halted, []rpcStep{
			{method: "VM.get_all_records", fault: map[string]any{"code": 1, "message": "SESSION_INVALID"}},
			{method: "session.login_with_password", result: testSession},
			observed,
		})
		if _, err := m.Read(t.Context()); err == nil {
			t.Fatal("expected session invalidation")
		}
		if _, err := plan.Apply(t.Context()); err == nil {
			t.Fatal("start confirmation survived reauthentication")
		}
	})
}

func prepareObservedStart(t *testing.T, halted map[string]any, calls []rpcStep) (*xapi.Monitor, xen.Plan) {
	t.Helper()
	steps := shutdownInventory(halted, "")
	steps = append(steps, poolVMs(shutdownVM, halted)) // preview
	steps = append(steps, calls...)                    // confirmation and verification
	server := xenServer(t, append(steps, rpcStep{method: "session.logout"})...)
	m, err := xapi.NewMonitor(server.URL, testUser, testPassword, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	domains, err := m.Read(t.Context())
	if err != nil || len(domains) != 1 || domains[0].StartTarget == "" {
		t.Fatalf("missing start capability: %+v, %v", domains, err)
	}
	d := domains[0]
	plan, err := m.Prepare(t.Context(), xen.Change{Action: xen.StartGuest, Name: d.Name, Target: d.Identity(), Instance: d.StartTarget})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.Description, d.Name) || !strings.Contains(plan.Description, d.UUID) {
		t.Fatalf("start preview lacks identity: %q", plan.Description)
	}
	return m, plan
}

func TestStartRequiresAdvertisedCapability(t *testing.T) {
	vm := runningGuest()
	vm["power_state"], vm["domid"], vm["resident_on"] = "Halted", "-1", "OpaqueRef:NULL"
	delete(vm, "allowed_operations")
	steps := shutdownInventory(vm, "")
	server := xenServer(t, append(steps, rpcStep{method: "session.logout"})...)
	m, err := xapi.NewMonitor(server.URL, testUser, testPassword, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	domains, err := m.Read(t.Context())
	if err != nil || len(domains) != 1 || domains[0].StartTarget != "" {
		t.Fatalf("expected inspectable halted VM without start capability: %+v, %v", domains, err)
	}
}
