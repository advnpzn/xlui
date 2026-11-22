package xapi_test

import (
	"maps"
	"strings"
	"testing"

	"github.com/advnpzn/xlui/internal/xen"
	"github.com/advnpzn/xlui/internal/xen/xapi"
)

func TestResidentHostFromObservedVM(t *testing.T) {
	const hostUUID = "22222222-2222-4222-8222-222222222222"
	vm := runningGuest()
	vmRead := func(record map[string]any) rpcStep {
		return rpcStep{method: "VM.get_record", params: []any{testSession, shutdownVM}, result: record}
	}
	host := rpcStep{method: "host.get_record", params: []any{testSession, vm["resident_on"]},
		result: map[string]any{"uuid": hostUUID, "name_label": "lab host"}}
	denied := host
	denied.fault = map[string]any{"code": 1, "message": "RBAC_PERMISSION_DENIED"}
	migrated := maps.Clone(vm)
	migrated["resident_on"] = "OpaqueRef:other-host"
	halted := maps.Clone(vm)
	halted["power_state"], halted["resident_on"], halted["domid"] = "Halted", "OpaqueRef:NULL", "-1"

	for _, tc := range []struct {
		name  string
		reads []rpcStep
		ok    bool
	}{
		{"running", []rpcStep{vmRead(vm), host, vmRead(vm)}, true},
		{"migrated during lookup", []rpcStep{vmRead(vm), host, vmRead(migrated)}, false},
		{"halted since last sample", []rpcStep{vmRead(halted)}, false},
		{"permission denied", []rpcStep{vmRead(vm), denied}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			steps := shutdownInventory(vm, shutdownStart)
			steps = append(steps, rpcStep{method: "VM.get_by_uuid", params: []any{testSession, shutdownUUID}, result: shutdownVM})
			steps = append(steps, tc.reads...)
			server := xenServer(t, append(steps, rpcStep{method: "session.logout"})...)
			m, err := xapi.NewMonitor(server.URL, testUser, testPassword, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(m.Close)
			rows, err := m.Read(t.Context())
			if err != nil || len(rows) != 1 {
				t.Fatalf("inventory = %+v, %v", rows, err)
			}
			got, err := m.ReadResidentHost(t.Context(), rows[0].Identity())
			if tc.ok {
				if err != nil || got.UUID != hostUUID || got.Name != "lab host" {
					t.Fatalf("resident host = %+v, %v", got, err)
				}
			} else if err == nil || got != (xen.ResidentHost{}) || strings.Contains(err.Error(), testSession) {
				t.Fatalf("failed lookup exposed placement or credentials: %+v, %v", got, err)
			}
		})
	}
}
