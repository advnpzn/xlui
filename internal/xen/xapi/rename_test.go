package xapi_test

import (
	"maps"
	"strings"
	"testing"

	"github.com/advnpzn/xlui/internal/xen"
	"github.com/advnpzn/xlui/internal/xen/xapi"
)

func TestPoolRename(t *testing.T) {
	const uuid = "11111111-1111-4111-8111-111111111111"
	const other = "22222222-2222-4222-8222-222222222222"
	const ref = "OpaqueRef:guest"
	change := xen.Change{Action: xen.RenameGuest, Target: xen.Identity{UUID: uuid, ID: 7}, Name: "bench", NewName: "Bench A"}
	before := map[string]any{
		"uuid": uuid, "name_label": "bench", "domid": "7", "power_state": "Running",
		"is_control_domain": false, "is_a_template": false, "is_a_snapshot": false,
	}
	after := maps.Clone(before)
	after["name_label"] = change.NewName
	list := func(key string, vm map[string]any) rpcStep {
		return rpcStep{method: "VM.get_all_records", result: map[string]any{key: vm}}
	}
	set := rpcStep{method: "VM.set_name_label", params: []any{testSession, ref, change.NewName}}
	read := func(vm map[string]any) rpcStep {
		return rpcStep{method: "VM.get_record", params: []any{testSession, ref}, result: vm}
	}
	// An invalid reply ID makes dispatch ambiguous. The fixture permits
	// logout only, so a repeated mutation would fail the test.
	badReply := set
	badReply.responseID = "unrelated"
	stale := maps.Clone(before)
	stale["name_label"] = "edited elsewhere"
	replacement := maps.Clone(before)
	replacement["uuid"] = other
	wrong := maps.Clone(after)
	wrong["uuid"] = other
	for _, tc := range []struct {
		name  string
		calls []rpcStep
		ok    bool
	}{
		{"running VM", []rpcStep{list(ref, before), set, read(after)}, true},
		{"label changed", []rpcStep{list(ref, stale)}, false},
		{"reference changed", []rpcStep{list("OpaqueRef:replacement", before)}, false},
		{"UUID changed", []rpcStep{list(ref, replacement)}, false},
		{"ambiguous response", []rpcStep{list(ref, before), badReply}, false},
		{"write ignored", []rpcStep{list(ref, before), set, read(before)}, false},
		{"wrong readback identity", []rpcStep{list(ref, before), set, read(wrong)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			steps := []rpcStep{{method: "session.login_with_password", result: testSession}, list(ref, before)}
			steps = append(steps, tc.calls...)
			server := xenServer(t, append(steps, rpcStep{method: "session.logout"})...)
			m, err := xapi.NewMonitor(server.URL, testUser, testPassword, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			plan, err := m.Prepare(t.Context(), change)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{change.Name, change.NewName, uuid} {
				if !strings.Contains(plan.Description, want) {
					t.Fatalf("missing %q in preview %q", want, plan.Description)
				}
			}
			if _, err := plan.Apply(t.Context()); (err == nil) != tc.ok {
				t.Fatalf("apply error %v, want success %t", err, tc.ok)
			}
			if _, err := plan.Apply(t.Context()); err == nil {
				t.Fatal("plan could be applied twice")
			}
		})
	}
	for _, flag := range []string{"is_control_domain", "is_a_template", "is_a_snapshot"} {
		t.Run(flag, func(t *testing.T) {
			protected := maps.Clone(before)
			protected[flag] = true
			server := xenServer(t, rpcStep{method: "session.login_with_password", result: testSession}, list(ref, protected), rpcStep{method: "session.logout"})
			m, err := xapi.NewMonitor(server.URL, testUser, testPassword, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			if _, err := m.Prepare(t.Context(), change); err == nil {
				t.Fatal("accepted rename for protected VM")
			}
		})
	}
}
