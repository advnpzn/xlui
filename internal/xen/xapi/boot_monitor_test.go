package xapi_test

import (
	"testing"
	"time"

	"github.com/advnpzn/xlui/internal/xen/xapi"
)

func TestBootTimeWithoutShutdownCapability(t *testing.T) {
	for _, state := range []string{"Running", "Paused", "Halted"} {
		t.Run(state, func(t *testing.T) {
			vm := runningGuest()
			vm["power_state"] = state
			delete(vm, "allowed_operations")
			steps := shutdownInventory(vm, shutdownStart)
			server := xenServer(t, append(steps, rpcStep{method: "session.logout"})...)
			m, err := xapi.NewMonitor(server.URL, testUser, testPassword, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			d, err := m.Read(t.Context())
			if err != nil || len(d) != 1 {
				t.Fatalf("inventory: %v, %v", d, err)
			}
			want := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
			if state == "Halted" {
				want = time.Time{}
			}
			if !d[0].StartedAt.Equal(want) || d[0].ShutdownInstance != "" {
				t.Fatalf("unexpected boot time or capability: %+v", d[0])
			}
		})
	}
}
