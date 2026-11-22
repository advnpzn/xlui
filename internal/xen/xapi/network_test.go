package xapi_test

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/advnpzn/xlui/internal/xen"
	"github.com/advnpzn/xlui/internal/xen/xapi"
)

func TestPoolNetworkInventory(t *testing.T) {
	const (
		vifRef      = "OpaqueRef:vif"
		networkRef  = "OpaqueRef:network"
		vifUUID     = "33333333-3333-4333-8333-333333333333"
		networkUUID = "44444444-4444-4444-8444-444444444444"
	)
	record := func(class, ref string, result map[string]any) rpcStep {
		return rpcStep{method: class + ".get_record", params: []any{testSession, ref}, result: result}
	}
	change := func(source map[string]any, key string, value any) map[string]any {
		m := maps.Clone(source)
		m[key] = value
		return m
	}

	vm := change(runningGuest(), "VIFs", []string{vifRef})
	vif := map[string]any{"uuid": vifUUID, "VM": shutdownVM, "network": networkRef, "device": "0",
		"MAC": "00:16:3e:01:02:03", "currently_attached": true}
	netUUID := rpcStep{method: "network.get_uuid", params: []any{testSession, networkRef}, result: networkUUID}
	netName := rpcStep{method: "network.get_name_label", params: []any{testSession, networkRef}, result: "storage"}
	vmRead := func(m map[string]any) rpcStep { return record("VM", shutdownVM, m) }
	vifRead := func(m map[string]any) rpcStep { return record("VIF", vifRef, m) }
	want := xen.PoolVIF{UUID: vifUUID, Device: "0", MAC: "00:16:3e:01:02:03", NetworkUUID: networkUUID, NetworkName: "storage", Attached: true}

	halted := change(vm, "power_state", "Halted")
	halted["domid"], halted["resident_on"] = "-1", "OpaqueRef:NULL"
	unplugged := change(vif, "currently_attached", false)
	detached := want
	detached.Attached = false
	empty := change(vm, "VIFs", []string{})
	// The VM reports an unordered set. Both devices resolve one shared network.
	const secondRef = "OpaqueRef:vif-2"
	const secondUUID = "55555555-5555-4555-8555-555555555555"
	first := vifRead(change(vif, "device", "10"))
	secondRecord := change(vif, "uuid", secondUUID)
	secondRecord["device"], secondRecord["MAC"] = "2", "00:16:3e:01:02:04"
	second := record("VIF", secondRef, secondRecord)
	both := change(vm, "VIFs", []string{secondRef, vifRef})
	reordered := change(vm, "VIFs", []string{vifRef, secondRef})
	device10, device2 := want, want
	device10.Device = "10"
	device2.Device, device2.UUID, device2.MAC = "2", secondUUID, "00:16:3e:01:02:04"

	tooMany := make([]string, 65)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("OpaqueRef:vif-%d", i)
	}
	denied := netName
	denied.fault = map[string]any{"code": 1, "message": "RBAC_PERMISSION_DENIED", "data": []string{testSession, testPassword}}
	for _, tc := range []struct {
		name    string
		sampled map[string]any
		reads   []rpcStep
		want    []xen.PoolVIF
	}{
		{"active interface", vm, []rpcStep{vmRead(vm), vifRead(vif), netUUID, netName, vifRead(vif), vmRead(vm)}, []xen.PoolVIF{want}},
		{"configured on halted guest", halted, []rpcStep{vmRead(halted), vifRead(unplugged), netUUID, netName, vifRead(unplugged), vmRead(halted)}, []xen.PoolVIF{detached}},
		{"empty inventory", empty, []rpcStep{vmRead(empty), vmRead(empty)}, []xen.PoolVIF{}},
		{"shared network with reordered VM membership", both, []rpcStep{vmRead(both), first, netUUID, netName, second, first, second, vmRead(reordered)}, []xen.PoolVIF{device2, device10}},
		{"excessive interface count", vm, []rpcStep{vmRead(change(vm, "VIFs", tooMany))}, nil},
		{"network permission denied", vm, []rpcStep{vmRead(vm), vifRead(vif), netUUID, denied}, nil},
		{"foreign VIF owner", vm, []rpcStep{vmRead(vm), vifRead(change(vif, "VM", "OpaqueRef:other-guest"))}, nil},
		{"domain restarted", vm, []rpcStep{vmRead(change(vm, "domid", "8"))}, nil},
		{"UUID lookup returned another VM", vm, []rpcStep{vmRead(change(vm, "uuid", networkUUID))}, nil},
		{"VIF moved while reading", vm, []rpcStep{vmRead(vm), vifRead(vif), netUUID, netName, vifRead(change(vif, "network", "OpaqueRef:other-net"))}, nil},
		{"VM lost interface while reading", vm, []rpcStep{vmRead(vm), vifRead(vif), netUUID, netName, vifRead(vif), vmRead(empty)}, nil},
		{"missing interface list", vm, []rpcStep{vmRead(runningGuest())}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			steps := shutdownInventory(tc.sampled, shutdownStart)
			steps = append(steps, rpcStep{method: "VM.get_by_uuid", params: []any{testSession, shutdownUUID}, result: shutdownVM})
			steps = append(steps, tc.reads...)
			server := xenServer(t, append(steps, rpcStep{method: "session.logout"})...)
			m, err := xapi.NewMonitor(server.URL, testUser, testPassword, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(m.Close)
			rows, err := m.Read(t.Context())
			if len(rows) != 1 || err != nil {
				t.Fatalf("inventory = %+v, %v", rows, err)
			}
			got, err := m.ReadNetwork(t.Context(), rows[0].Identity())
			if tc.want == nil {
				if err == nil || len(got) != 0 || strings.Contains(err.Error(), testSession) || strings.Contains(err.Error(), testPassword) {
					t.Fatalf("invalid or unauthorized read returned %+v, %v", got, err)
				}
				return
			}
			if err != nil || !slices.Equal(got, tc.want) {
				t.Fatalf("network interfaces = %+v, %v; want %+v", got, err, tc.want)
			}
		})
	}
}
