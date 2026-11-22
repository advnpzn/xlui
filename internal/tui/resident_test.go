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

func TestReadOnlyResidentHostInspector(t *testing.T) {
	vm := xen.Domain{ID: 7, UUID: "11111111-1111-4111-8111-111111111111", Name: "test VM", State: "Running"}
	target := make(chan xen.Identity, 2)
	host := xen.ResidentHost{Name: "pool member", UUID: "22222222-2222-4222-8222-222222222222"}
	p := config.Profile{Name: "pool", Type: config.XAPI}
	src := session.Source{
		Read:             func(context.Context) ([]xen.Domain, error) { return []xen.Domain{vm}, nil },
		ReadResidentHost: func(_ context.Context, id xen.Identity) (xen.ResidentHost, error) {
			target <- id
			return host, nil
		},
	}
	w := newWorkspace(t.Context(), p, time.Hour, func(config.Profile, string) (session.Source, error) { return src, nil }, config.Store{})
	t.Cleanup(w.close)
	w.connect(p, "test-password")
	w.applyUpdate(<-w.updates)
	h := w.active
	key := tcell.NewEventKey(tcell.KeyRune, 'h', 0)
	h.view.key(tcell.NewEventKey(tcell.KeyEnter, 0, 0))
	h.view.key(key)
	w.applyDialogResult(nextPlacementResult(t, w))
	if id := <-target; id != vm.Identity() || w.dialog == nil || w.dialog.title != "RESIDENT HOST" {
		t.Fatalf("expected resident host dialog for %v, got %v", vm.Identity(), id)
	}
	body := w.dialog.body.(*tview.TextView).GetText(false)
	if !strings.Contains(body, host.Name) || !strings.Contains(body, host.UUID) {
		t.Fatalf("host identity missing in %q", body)
	}
	w.dialog.key(tcell.NewEventKey(tcell.KeyEsc, 0, 0))
	if w.dialog != nil || h.view.inspect == nil || h.view.inspect.domain.Identity() != vm.Identity() {
		t.Fatal("Esc lost the selected VM")
	}

	h.view.key(key)
	late := nextPlacementResult(t, w)
	old := late.dialog.body.(*tview.TextView)
	previous := old.GetText(false)
	replacement := vm
	replacement.UUID = "33333333-3333-4333-8333-333333333333"
	w.applyUpdate(hostUpdate{h, session.Snapshot{Domains: []xen.Domain{replacement}, ObservedAt: h.view.latest.Add(time.Second)}})
	w.applyDialogResult(late)
	if w.dialog != nil || old.GetText(false) != previous {
		t.Fatal("accepted late host placement after replacement")
	}
}
