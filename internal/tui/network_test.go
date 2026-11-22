package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/advnpzn/xlui/internal/config"
	"github.com/advnpzn/xlui/internal/session"
	"github.com/advnpzn/xlui/internal/xen"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestNetworkInspector(t *testing.T) {
	vm := xen.Domain{ID: 9, UUID: "b586dd4f-32ba-478e-a5ca-9c2640c7ef54", Name: "worker"}
	vif := xen.VIF{ID: 2, MAC: "00:16:3e:ab:cd:02", BackendPath: "/local/domain/0/backend/vif/9/2"}
	profile := config.Profile{Name: "edge", Type: config.SSH}
	targets := make(chan xen.Identity, 1)
	src := session.Source{
		Read: func(context.Context) ([]xen.Domain, error) { return []xen.Domain{vm}, nil },
		ReadNetwork: func(_ context.Context, id xen.Identity) ([]xen.VIF, error) {
			targets <- id
			return []xen.VIF{vif}, nil
		},
	}
	w := newWorkspace(t.Context(), profile, time.Hour, func(config.Profile, string) (session.Source, error) { return src, nil }, config.Store{})
	t.Cleanup(w.close)
	w.connect(profile, "")
	w.applyUpdate(<-w.updates)
	b := w.active.view
	key := tcell.NewEventKey(tcell.KeyRune, 'n', 0)
	b.key(key)
	if w.dialog != nil {
		t.Fatal("unexpected network view outside the inspector")
	}
	b.key(tcell.NewEventKey(tcell.KeyEnter, 0, 0))
	b.key(key)
	w.applyDialogResult(nextPlacementResult(t, w))
	if id := <-targets; id != vm.Identity() || w.dialog == nil {
		t.Fatalf("expected network view for %v, requested %v", vm.Identity(), id)
	}
	output := w.dialog.body.(*tview.TextView).GetText(false)
	if !strings.Contains(output, vif.MAC) || !strings.Contains(output, vif.BackendPath) {
		t.Fatalf("missing interface details in %q", output)
	}
	w.dialog.key(tcell.NewEventKey(tcell.KeyEsc, 0, 0))
	if w.dialog != nil || b.inspect == nil || b.inspect.domain.Identity() != vm.Identity() {
		t.Fatal("Esc lost the selected domain")
	}
}

func TestHaltedPoolVMNetworkInspector(t *testing.T) {
	vm := xen.Domain{ID: -1, UUID: "11111111-1111-4111-8111-111111111111", Name: "offline", State: "Halted"}
	vif := xen.PoolVIF{
		UUID: "33333333-3333-4333-8333-333333333333", Device: "0", MAC: "00:16:3e:01:02:03",
		NetworkUUID: "44444444-4444-4444-8444-444444444444", NetworkName: "storage",
	}
	profile := config.Profile{Name: "pool", Type: config.XAPI}
	target := make(chan xen.Identity, 1)
	src := session.Source{
		Read: func(context.Context) ([]xen.Domain, error) { return []xen.Domain{vm}, nil },
		ReadPoolNetwork: func(_ context.Context, id xen.Identity) ([]xen.PoolVIF, error) {
			target <- id
			return []xen.PoolVIF{vif}, nil
		},
	}
	w := newWorkspace(t.Context(), profile, time.Hour, func(config.Profile, string) (session.Source, error) { return src, nil }, config.Store{})
	t.Cleanup(w.close)
	w.connect(profile, "test-password")
	w.applyUpdate(<-w.updates)

	b := w.active.view
	b.key(tcell.NewEventKey(tcell.KeyEnter, 0, 0))
	b.key(tcell.NewEventKey(tcell.KeyRune, 'n', 0))
	w.applyDialogResult(nextPlacementResult(t, w))
	if id := <-target; id != vm.Identity() || w.dialog == nil {
		t.Fatalf("expected network view for %v, requested %v", vm.Identity(), id)
	}
	body := w.dialog.body.(*tview.TextView).GetText(false)
	for _, detail := range []string{vif.UUID, vif.MAC, vif.NetworkName, vif.NetworkUUID, "Detached"} {
		if !strings.Contains(body, detail) {
			t.Errorf("missing pool interface field %q in %q", detail, body)
		}
	}
	w.dialog.key(tcell.NewEventKey(tcell.KeyEsc, 0, 0))
	if w.dialog != nil || b.inspect == nil || b.inspect.domain.Identity() != vm.Identity() {
		t.Fatal("Esc lost the halted VM")
	}
}
