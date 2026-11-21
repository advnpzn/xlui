package xapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/advnpzn/xlui/internal/xen/xapi"
)

const (
	testUser     = "inventory-user"
	testPassword = "sample-password"
	testSession  = "OpaqueRef:sample-session"
)

type rpcStep struct {
	method     string
	params     []string
	result     any
	fault      any
	responseID any
	onCall     func(context.Context)
}

func xenServer(t *testing.T, steps ...rpcStep) *httptest.Server {
	t.Helper()
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(calls.Add(1)) - 1
		if n >= len(steps) {
			t.Errorf("unexpected request %d", n+1)
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		var req struct {
			Version string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Method  string          `json:"method"`
			Params  []string        `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if r.Method != http.MethodPost || r.URL.RequestURI() != "/jsonrpc" || req.Version != "2.0" || len(req.ID) == 0 || string(req.ID) == "null" {
			t.Errorf("unexpected request: %s %s, version %q, id %s", r.Method, r.URL.RequestURI(), req.Version, req.ID)
		}
		step := steps[n]
		if req.Method != step.method {
			t.Errorf("call %d: got %q, want %q", n+1, req.Method, step.method)
		}
		if req.Method == "session.login_with_password" {
			if len(req.Params) != 4 || req.Params[0] != testUser || req.Params[1] != testPassword || req.Params[2] == "" || req.Params[3] != "xlui" {
				t.Error("unexpected login parameters")
			}
		} else if step.params != nil {
			if !reflect.DeepEqual(req.Params, step.params) {
				t.Errorf("%s did not receive the expected parameters", req.Method)
			}
		} else if !reflect.DeepEqual(req.Params, []string{testSession}) {
			t.Errorf("%s did not receive the session reference as its sole argument", req.Method)
		}
		if step.onCall != nil {
			step.onCall(r.Context())
		}
		reply := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if step.responseID != nil {
			reply["id"] = step.responseID
		}
		if step.fault != nil {
			reply["error"] = step.fault
		} else {
			reply["result"] = step.result
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(reply); err != nil && r.Context().Err() == nil {
			t.Error(err)
		}
	}))
	t.Cleanup(func() {
		server.Close()
		if got := int(calls.Load()); got != len(steps) {
			t.Errorf("got %d calls, want %d", got, len(steps))
		}
	})
	return server
}

func TestPoolInventory(t *testing.T) {
	records := json.RawMessage(`{
		"OpaqueRef:1":{"uuid":"83ea9378-c508-4f49-bb2e-a4256ee97a01","name_label":"web","power_state":"Halted","is_control_domain":false,"is_a_template":false,"is_a_snapshot":false},
		"OpaqueRef:2":{"uuid":"83ea9378-c508-4f49-bb2e-a4256ee97a02","name_label":"template","power_state":"Halted","is_control_domain":false,"is_a_template":true,"is_a_snapshot":false},
		"OpaqueRef:3":{"uuid":"83ea9378-c508-4f49-bb2e-a4256ee97a03","name_label":"snapshot","power_state":"Halted","is_control_domain":false,"is_a_template":false,"is_a_snapshot":true},
		"OpaqueRef:4":{"uuid":"83ea9378-c508-4f49-bb2e-a4256ee97a04","name_label":"dom0","power_state":"Running","is_control_domain":true,"is_a_template":false,"is_a_snapshot":false}
	}`)
	server := xenServer(t,
		rpcStep{method: "session.login_with_password", result: testSession},
		rpcStep{method: "VM.get_all_records", result: records},
		rpcStep{method: "session.logout"},
	)
	got, err := xapi.List(context.Background(), server.URL, testUser, testPassword, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	slices.SortFunc(got, func(a, b xapi.VM) int { return strings.Compare(a.UUID, b.UUID) })
	want := []xapi.VM{
		{UUID: "83ea9378-c508-4f49-bb2e-a4256ee97a01", Name: "web", PowerState: "Halted"},
		{UUID: "83ea9378-c508-4f49-bb2e-a4256ee97a04", Name: "dom0", PowerState: "Running", ControlDomain: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestRejectsInvalidInventory(t *testing.T) {
	for _, tc := range []struct {
		name       string
		result     string
		responseID any
	}{
		{name: "wrong response ID", result: "{}", responseID: "different-call"},
		{name: "null record map", result: "null"},
		{name: "null VM", result: `{"OpaqueRef:1":null}`},
		{name: "missing UUID", result: `{"OpaqueRef:1":{"name_label":"vm","power_state":"Running","is_control_domain":false,"is_a_template":false,"is_a_snapshot":false}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := xenServer(t,
				rpcStep{method: "session.login_with_password", result: testSession},
				rpcStep{method: "VM.get_all_records", result: json.RawMessage(tc.result), responseID: tc.responseID},
				rpcStep{method: "session.logout"},
			)
			if _, err := xapi.List(context.Background(), server.URL, testUser, testPassword, server.Client()); err == nil {
				t.Fatal("expected invalid inventory to fail")
			}
		})
	}
}

func TestInventoryFaultReleasesSession(t *testing.T) {
	server := xenServer(t,
		rpcStep{method: "session.login_with_password", result: testSession},
		rpcStep{method: "VM.get_all_records", fault: map[string]any{
			"code": 1, "message": "SESSION_INVALID", "data": []string{testSession, testPassword},
		}},
		rpcStep{method: "session.logout"},
	)
	_, err := xapi.List(context.Background(), server.URL+"/jsonrpc", testUser, testPassword, server.Client())
	if err == nil || !strings.Contains(err.Error(), "SESSION_INVALID") {
		t.Fatal("expected the XenAPI error code")
	}
	if strings.Contains(err.Error(), testSession) || strings.Contains(err.Error(), testPassword) {
		t.Fatal("error exposed secret data")
	}
}

func TestCancellationReleasesSession(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := xenServer(t,
		rpcStep{method: "session.login_with_password", result: testSession},
		rpcStep{method: "VM.get_all_records", result: map[string]any{}, onCall: func(request context.Context) {
			cancel()
			select {
			case <-request.Done():
			case <-time.After(3 * time.Second):
				t.Error("inventory request remained active after cancellation")
			}
		}},
		rpcStep{method: "session.logout"},
	)
	if _, err := xapi.List(ctx, server.URL, testUser, testPassword, server.Client()); err == nil {
		t.Fatal("expected cancelled inventory to fail")
	}
}

func TestLoginRedirectDoesNotForwardCredentials(t *testing.T) {
	var forwarded atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/jsonrpc" {
			http.Redirect(w, r, "/elsewhere", http.StatusTemporaryRedirect)
			return
		}
		forwarded.Add(1)
		http.Error(w, testPassword, http.StatusForbidden)
	}))
	defer server.Close()
	_, err := xapi.List(context.Background(), server.URL, testUser, testPassword, server.Client())
	if err == nil || strings.Contains(err.Error(), testPassword) || forwarded.Load() != 0 {
		t.Fatal("login redirect must stop at the configured endpoint")
	}
}
