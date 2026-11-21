package xapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	"github.com/advnpzn/xlui/internal/xen"
	"github.com/advnpzn/xlui/internal/xen/xapi"
)

func TestPoolGuestChanges(t *testing.T) {
	const id = "11111111-1111-4111-8111-111111111111"
	const cloned = "22222222-2222-4222-8222-222222222222"
	var mu sync.Mutex
	guest := map[string]any{"uuid": id, "name_label": "base", "power_state": "Halted", "is_control_domain": false, "is_a_template": false, "is_a_snapshot": false, "VCPUs_max": "8", "VCPUs_at_startup": "2", "memory_static_min": "134217728", "memory_static_max": "1073741824", "memory_dynamic_min": "268435456", "memory_dynamic_max": "536870912"}
	rows := map[string]any{"vm-ref": guest}
	writes := 0
	var omitWrite bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     int
			Method string
			Params []json.RawMessage
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		var result any
		switch req.Method {
		case "session.login_with_password":
			result = testSession
		case "session.logout":
		case "VM.get_all_records":
			result = rows
		case "VM.get_record":
			var ref string
			json.Unmarshal(req.Params[1], &ref)
			result = rows[ref]
		case "VM.set_VCPUs_at_startup", "VM.set_memory_dynamic_range", "VM.clone":
			if string(req.Params[0]) != strconv.Quote(testSession) || string(req.Params[1]) != "\"vm-ref\"" {
				t.Error("wrong mutation target")
			}
			writes++
			switch req.Method {
			case "VM.set_VCPUs_at_startup":
				if string(req.Params[2]) != "4" {
					t.Error("invalid JSON integer")
				}
				if !omitWrite {
					guest["VCPUs_at_startup"] = "4"
				}
			case "VM.set_memory_dynamic_range":
				if string(req.Params[2]) != "805306368" || string(req.Params[2]) != string(req.Params[3]) {
					t.Error("incorrect byte range")
				}
				guest["memory_dynamic_min"] = "805306368"
				guest["memory_dynamic_max"] = "805306368"
			case "VM.clone":
				rows["new-ref"] = map[string]any{"uuid": cloned, "name_label": "copy", "power_state": "Halted", "is_control_domain": false, "is_a_template": false, "is_a_snapshot": false}
				result = "new-ref"
			}
		default:
			t.Errorf("unexpected RPC %s", req.Method)
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	defer server.Close()
	m, err := xapi.NewMonitor(server.URL, "viewer", "test", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	cpu := xen.Change{Action: xen.SetVCPUs, Name: "base", Target: xen.Identity{UUID: id, ID: -1}, VCPUs: 4}
	set := func(key string, v any) { mu.Lock(); defer mu.Unlock(); guest[key] = v }
	p, err := m.Prepare(t.Context(), cpu)
	if err != nil {
		t.Fatal(err)
	}
	set("VCPUs_at_startup", "3")
	if _, err := p.Apply(t.Context()); err == nil {
		t.Fatal("wrote after a conflicting change")
	}
	set("is_control_domain", true)
	if _, err := m.Prepare(t.Context(), cpu); err == nil {
		t.Fatal("accepted control VM")
	}
	set("is_control_domain", false)
	set("power_state", "Running")
	if _, err := m.Prepare(t.Context(), cpu); err == nil {
		t.Fatal("accepted live change")
	}
	set("power_state", "Halted")
	p, err = m.Prepare(t.Context(), cpu)
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	omitWrite = true
	mu.Unlock()
	if _, err := p.Apply(t.Context()); err == nil {
		t.Fatal("claimed success after server ignored the write")
	}
	mu.Lock()
	omitWrite = false
	mu.Unlock()
	for _, c := range []xen.Change{cpu, {Action: xen.SetMemory, Name: "base", Target: cpu.Target, MemoryMiB: 768}, {Action: xen.CreateGuest, Name: "copy", SourceUUID: id}} {
		p, err := m.Prepare(t.Context(), c)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Apply(t.Context()); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Apply(t.Context()); err == nil {
			t.Fatal("confirmation applied twice")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if writes != 4 {
		t.Fatalf("%d mutations", writes)
	}
}
