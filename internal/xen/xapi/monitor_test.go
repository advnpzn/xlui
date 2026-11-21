package xapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/advnpzn/xlui/internal/xen/xapi"
)

func TestMonitorReusesSessionAndRejectsStaleRRD(t *testing.T) {
	var login, logout, frames atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rrd_updates" {
			if r.URL.Query().Get("session_id") != testSession {
				t.Error("RRD without session")
			}
			now := time.Now().Unix()
			if frames.Add(1) == 2 {
				now -= 90
			}
			fmt.Fprintf(w, `<xport><meta><legend><entry>AVERAGE:vm:abc:cpu0</entry><entry>AVERAGE:vm:abc:cpu1</entry><entry>AVERAGE:vm:abc:memory</entry><entry>AVERAGE:vm:abc:memory_internal_free</entry></legend></meta><data><row><t>%d</t><v>0.8</v><v>0.7</v><v>268435456</v><v>65536</v></row></data></xport>`, now)
			return
		}
		var req struct {
			ID     int
			Method string
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		var result any
		switch req.Method {
		case "session.login_with_password":
			login.Add(1)
			result = testSession
		case "VM.get_all_records":
			result = json.RawMessage(`{"ref":{"name_label":"guest","uuid":"abc","domid":"7","metrics":"mr","power_state":"Running","is_control_domain":false,"is_a_template":false,"is_a_snapshot":false}}`)
		case "VM_metrics.get_all_records":
			result = json.RawMessage(`{"mr":{"memory_actual":"268435456","VCPUs_number":"2"}}`)
		case "session.logout":
			logout.Add(1)
		default:
			t.Errorf("unexpected RPC %s", req.Method)
		}
		json.NewEncoder(w).Encode(map[string]any{"id": req.ID, "jsonrpc": "2.0", "result": result})
	}))
	defer server.Close()
	m, err := xapi.NewMonitor(server.URL, testUser, testPassword, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		d, err := m.Read(context.Background())
		if err != nil || len(d) != 1 || d[0].MemoryMiB != 256 || d[0].VCPUs != 2 || d[0].ID != 7 {
			t.Fatalf("inventory %v, %v", d, err)
		}
		if i == 0 && (d[0].CPUPercent == nil || *d[0].CPUPercent != 150 || d[0].GuestUsedKiB == nil || *d[0].GuestUsedKiB != 196608) {
			t.Fatal("incorrect units or guest usage", d[0])
		}
		if i == 1 && (d[0].CPUPercent != nil || d[0].GuestUsedKiB != nil) {
			t.Fatal("presented expired measurements")
		}
	}
	m.Close()
	if login.Load() != 1 || logout.Load() != 1 {
		t.Fatal("incorrect session lifetime")
	}
}

func TestDeniedLoginIsNotRepeatedWhilePolling(t *testing.T) {
	server := xenServer(t, rpcStep{method: "session.login_with_password", fault: map[string]any{"code": 1, "message": "SESSION_AUTHENTICATION_FAILED"}})
	m, err := xapi.NewMonitor(server.URL, testUser, testPassword, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	for range 3 {
		if _, err := m.Read(context.Background()); err == nil {
			t.Fatal("denied session read succeeded")
		}
	}
}
