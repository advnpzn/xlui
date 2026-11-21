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

func openPlacementWorkspace(t *testing.T, vm xen.Domain, read func(context.Context, xen.Identity) ([]xen.VCPU, error)) *workspace {
	t.Helper()
	p := config.Profile{Name: "edge", Type: config.Local}
	src := session.Source{
		Read:      func(context.Context) ([]xen.Domain, error) { return []xen.Domain{vm}, nil },
		ReadVCPUs: read,
	}
	w := newWorkspace(t.Context(), p, time.Hour, func(config.Profile, string) (session.Source, error) { return src, nil }, config.Store{})
	t.Cleanup(w.close)
	w.connect(p, "")
	w.applyUpdate(<-w.updates)
	return w
}

func nextPlacementResult(t *testing.T, w *workspace) dialogResult {
	t.Helper()
	select {
	case result := <-w.dialogResults:
		return result
	case <-time.After(time.Second):
		t.Fatal("placement worker did not return")
		return dialogResult{}
	}
}

func TestReadOnlyPlacementFromInspector(t *testing.T) {
	vm := xen.Domain{Name: "audio guest", UUID: "c15af72e-a3ec-4162-ab73-f4a034631364", ID: 5}
	requests := make(chan xen.Identity, 2)
	w := openPlacementWorkspace(t, vm, func(_ context.Context, id xen.Identity) ([]xen.VCPU, error) {
		requests <- id
		return []xen.VCPU{{ID: 0, CPU: 6, State: "r--", Seconds: 123.4, Hard: "4-7", Soft: "5,6"}}, nil
	})
	b := w.active.view
	placementKey := tcell.NewEventKey(tcell.KeyRune, 'v', 0)
	b.key(placementKey)
	if w.dialog != nil {
		t.Fatal("the inventory unexpectedly initiated a placement request")
	}
	b.key(tcell.NewEventKey(tcell.KeyEnter, 0, 0))
	b.key(placementKey)
	w.applyDialogResult(nextPlacementResult(t, w))
	if target := <-requests; target != vm.Identity() {
		t.Fatalf("placement requested for %+v, want %+v", target, vm.Identity())
	}
	if w.dialog == nil || w.dialog.title != "VCPU PLACEMENT" {
		t.Fatal("placement view did not open")
	}
	text := w.dialog.body.(*tview.TextView).GetText(false)
	for _, value := range []string{"vCPU 0", "pCPU 6", "r--", "123.4", "Hard: 4-7", "Soft: 5,6"} {
		if !strings.Contains(text, value) {
			t.Errorf("missing %q in placement view %q", value, text)
		}
	}
	w.dialog.key(tcell.NewEventKey(tcell.KeyEsc, 0, 0))
	if w.dialog != nil || b.inspect == nil || b.inspect.domain.Identity() != vm.Identity() {
		t.Fatal("Esc did not return to the selected domain inspector")
	}
}

func TestPlacementDropsLateResultAfterIDReuse(t *testing.T) {
	vm := xen.Domain{ID: 5, Name: "audio", UUID: "c15af72e-a3ec-4162-ab73-f4a034631364"}
	w := openPlacementWorkspace(t, vm, func(context.Context, xen.Identity) ([]xen.VCPU, error) {
		return []xen.VCPU{{ID: 0, CPU: 3, State: "r--", Hard: "all", Soft: "all"}}, nil
	})
	h := w.active
	h.view.key(tcell.NewEventKey(tcell.KeyEnter, 0, 0))
	h.view.key(tcell.NewEventKey(tcell.KeyRune, 'v', 0))
	queued := nextPlacementResult(t, w)
	obsolete := queued.dialog.body.(*tview.TextView)
	loading := obsolete.GetText(false)
	replacement := vm
	replacement.UUID = "ef3a824a-5106-4dec-b021-a43d67584a93"
	w.applyUpdate(hostUpdate{h, session.Snapshot{Domains: []xen.Domain{replacement}, ObservedAt: h.view.latest.Add(time.Second)}})
	if w.dialog != nil {
		t.Fatal("old placement dialog survived a domid reuse")
	}
	w.applyDialogResult(queued)
	if w.dialog != nil || !h.view.inspect.gone || obsolete.GetText(false) != loading {
		t.Fatal("late placement results were accepted for a replaced domain")
	}
}
