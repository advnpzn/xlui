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

func TestGuestPreviewScrollsWithButtonsInView(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(40, 10)
	h := &liveSession{profile: config.Profile{Name: "test", Type: config.Local}, view: newBrowser("test")}
	w := &workspace{active: h}
	returned := false
	w.confirmGuest(h, xen.Plan{Description: strings.Repeat("Guest setting to review\n", 10) + "Last setting"}, func() { returned = true })
	d := w.dialog
	d.draw(screen)
	form := d.body.(*tview.Form)
	for n := range 2 {
		x, y, width, _ := form.GetButton(n).GetRect()
		if x < 2 || x+width > 38 || width == 0 || y < 3 || y >= 8 {
			t.Fatalf("button %d outside the visible dialog at (%d, %d), width %d", n, x, y, width)
		}
	}
	d.key(tcell.NewEventKey(tcell.KeyEnd, 0, 0))
	d.draw(screen)
	if offset, _ := d.detail.GetScrollOffset(); offset == 0 {
		t.Fatal("the preview did not scroll with Back focused")
	}
	d.key(tcell.NewEventKey(tcell.KeyEnter, 0, 0))
	if !returned {
		t.Fatal("scrolling moved focus from Back")
	}
}

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

func TestGracefulShutdownNeedsExplicitApply(t *testing.T) {
	target := xen.Domain{Name: "guest", UUID: "11111111-1111-4111-8111-111111111111", ID: 7, State: "Running", ShutdownInstance: "boot-1"}
	got := confirmedEdit(t, target, 's', 0, "GUESTS")
	if got.Target != target.Identity() || got.Name != target.Name || got.Instance != target.ShutdownInstance || got.Action != xen.GracefulShutdown {
		t.Fatalf("wrong shutdown target: %+v", got)
	}
}

func TestUnavailableGracefulShutdownIsHidden(t *testing.T) {
	for _, tc := range []struct {
		name, provider, state, instance string
		id                              int
		control                         bool
	}{
		{"local", config.Local, "Running", "boot-1", 7, false},
		{"ssh", config.SSH, "Running", "boot-1", 7, false},
		{"control domain", config.XAPI, "Running", "boot-1", 7, true},
		{"domid zero", config.XAPI, "Running", "boot-1", 0, false},
		{"missing domid", config.XAPI, "Running", "boot-1", -1, false},
		{"halted", config.XAPI, "Halted", "boot-1", 7, false},
		{"paused", config.XAPI, "Paused", "boot-1", 7, false},
		{"missing evidence or permission", config.XAPI, "Running", "", 7, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := xen.Domain{Name: "guest", UUID: "11111111-1111-4111-8111-111111111111", State: tc.state, ID: tc.id, ControlDomain: tc.control, ShutdownInstance: tc.instance}
			h := &liveSession{profile: config.Profile{Type: tc.provider}, view: newBrowser("inventory"), prepare: func(context.Context, xen.Change) (xen.Plan, error) {
				panic("opening menu must not prepare a guest change")
			}}
			h.view.update(session.Snapshot{ObservedAt: time.Now(), Domains: []xen.Domain{target}})
			w := &workspace{active: h}
			w.guestMenu(h)
			list := w.dialog.body.(*tview.List)
			for i := range list.GetItemCount() {
				if label, _ := list.GetItemText(i); label == "Shut down gracefully" {
					t.Fatal("shutdown offered")
				}
			}
		})
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
		case r := <-w.dialogResults:
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
	if input != 0 {
		key(tcell.KeyEnd, 0)
		key(tcell.KeyRune, input)
		key(tcell.KeyTab, 0)
		key(tcell.KeyEnter, 0)
	}
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
	if input == 0 {
		key(tcell.KeyRune, shortcut) // Reopen the preview from Guests.
	} else {
		key(tcell.KeyEnter, 0)
	}
	result()
	key(tcell.KeyTab, 0)
	key(tcell.KeyEnter, 0)
	result()
	if w.dialog.title != "GUEST UPDATED" {
		t.Fatal(w.dialog.title)
	}
	return <-applied
}

func TestStartPoolGuestNeedsExplicitApply(t *testing.T) {
	target := xen.Domain{Name: "guest", UUID: "11111111-1111-4111-8111-111111111111", ID: -1, State: "Halted", StartTarget: "halted-guest"}
	got := confirmedEdit(t, target, 's', 0, "GUESTS")
	if got.Target != target.Identity() || got.Name != target.Name || got.Instance != target.StartTarget || got.Action != xen.StartGuest {
		t.Fatalf("wrong start target: %+v", got)
	}
}
