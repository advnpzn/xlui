package tui

import (
	"context"
	"testing"
	"time"

	"github.com/advnpzn/xlui/internal/config"
	"github.com/advnpzn/xlui/internal/session"
	"github.com/advnpzn/xlui/internal/xen"
	"github.com/gdamore/tcell/v2"
)

func TestGuestEditNeedsExplicitApply(t *testing.T) {
	target := xen.Domain{Name: "guest", UUID: "11111111-1111-4111-8111-111111111111", ID: -1, State: "Halted"}
	got := confirmedEdit(t, target, 'v', '4', "EDIT GUEST")
	if got.Target != target.Identity() || got.VCPUs != 4 || got.Action != xen.SetVCPUs {
		t.Fatalf("wrong dispatch: %+v", got)
	}
}

func TestRenameRunningPoolGuest(t *testing.T) {
	target := xen.Domain{Name: "guest", UUID: "11111111-1111-4111-8111-111111111111", ID: 7, State: "Running"}
	got := confirmedEdit(t, target, 'r', '2', "RENAME GUEST")
	if got.Target != target.Identity() || got.Name != "guest" || got.NewName != "guest2" || got.Action != xen.RenameGuest {
		t.Fatalf("wrong rename: %+v", got)
	}
}

func confirmedEdit(t *testing.T, target xen.Domain, shortcut, input rune, title string) xen.Change {
	t.Helper()
	p := config.Profile{Name: "west-pool", Type: config.XAPI, Address: "https://west.example"}
	applied := make(chan xen.Change, 2)
	src := session.Source{Read: func(context.Context) ([]xen.Domain, error) { return []xen.Domain{target}, nil },
		Prepare: func(_ context.Context, req xen.Change) (xen.Plan, error) {
			return xen.NewPlan("Review change", func(context.Context) (string, error) { applied <- req; return "Verified", nil }), nil
		}}
	w := newWorkspace(t.Context(), p, time.Hour, func(config.Profile, string) (session.Source, error) { return src, nil }, config.Store{})
	defer w.close()
	w.connect(p, "test")
	sample := <-w.updates
	sample.host.view.update(sample.snapshot)
	key := func(k tcell.Key, r rune) {
		ev := tcell.NewEventKey(k, r, 0)
		if w.dialog != nil {
			w.dialog.key(ev)
		} else {
			w.active.view.key(ev)
		}
	}
	result := func() {
		t.Helper()
		select {
		case r := <-w.guestResults:
			if r.host != w.active || r.dialog != w.dialog {
				t.Fatal("result for incorrect context")
			}
			r.show()
		case <-time.After(time.Second):
			t.Fatal("no worker result")
		}
	}
	key(tcell.KeyRune, 'g')
	key(tcell.KeyRune, shortcut)
	key(tcell.KeyEnd, 0)
	key(tcell.KeyRune, input)
	key(tcell.KeyTab, 0)
	key(tcell.KeyEnter, 0)
	result()
	if w.dialog.title != "REVIEW GUEST CHANGE" {
		t.Fatal(w.dialog.title)
	}
	key(tcell.KeyEnter, 0) // Back is initially focused.
	select {
	case <-applied:
		t.Fatal("review submitted by default")
	default:
	}
	if w.dialog.title != title {
		t.Fatal("Back did not restore draft")
	}
	key(tcell.KeyEnter, 0)
	result()
	key(tcell.KeyTab, 0)
	key(tcell.KeyEnter, 0)
	result()
	if w.dialog.title != "GUEST UPDATED" {
		t.Fatal(w.dialog.title)
	}
	return <-applied
}
