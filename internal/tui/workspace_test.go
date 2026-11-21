package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/advnpzn/xlui/internal/config"
	"github.com/advnpzn/xlui/internal/session"
	"github.com/advnpzn/xlui/internal/xen"
	"github.com/gdamore/tcell/v2"
)

func TestSaveOpenAndSwitchSessionsThroughKeyboard(t *testing.T) {
	initial := config.Profile{Name: "default", Type: config.Local}
	store := config.Store{Path: filepath.Join(t.TempDir(), "xlui", "config.json")}
	closed := make(chan config.Profile, 2)
	w := newWorkspace(context.Background(), initial, time.Minute, func(p config.Profile, password string) (session.Source, error) {
		if password != "" {
			t.Fatal("unexpected password on xl connection")
		}
		return session.Source{Read: func(context.Context) ([]xen.Domain, error) {
			return []xen.Domain{{Name: p.Name, UUID: "same", ID: 1}}, nil
		}, Close: func() { closed <- p }}, nil
	}, store)
	defer w.close()
	w.connect(initial, "")
	w.active.view.key(tcell.NewEventKey(tcell.KeyRune, 's', 0))
	key := func(k tcell.Key, r rune) {
		t.Helper()
		if w.dialog == nil {
			t.Fatal("dialog closed early")
		}
		w.dialog.key(tcell.NewEventKey(k, r, 0))
	}
	key(tcell.KeyRune, 'a')
	key(tcell.KeyRune, 's')
	for _, r := range "west" {
		key(tcell.KeyRune, r)
	}
	key(tcell.KeyTab, 0)
	for _, r := range "operator@west" {
		key(tcell.KeyRune, r)
	}
	key(tcell.KeyTab, 0)
	key(tcell.KeyEnter, 0)
	profiles, err := store.Load()
	if err != nil || len(profiles) != 1 || profiles[0].Address != "operator@west" || w.active.profile != profiles[0] || len(w.live) != 2 {
		t.Fatalf("profiles %v, err %v, active %v", profiles, err, w.active)
	}
	for range 2 {
		select {
		case sample := <-w.updates:
			sample.host.view.update(sample.snapshot)
		case <-time.After(time.Second):
			t.Fatal("a host did not sample")
		}
	}
	w.active.view.key(tcell.NewEventKey(tcell.KeyRune, '-', 0))
	w.active.view.key(tcell.NewEventKey(tcell.KeyRune, 'L', 0))
	w.active.view.filter.SetText("west")
	w.active.view.key(tcell.NewEventKey(tcell.KeyTab, 0, 0))
	if w.active.profile != initial || w.active.view.rows.Domains[0].Name != initial.Name || w.active.view.filter.GetText() != "" {
		t.Fatal("cross-session state or sample leaked")
	}
	if w.active.view.layout != Inventory {
		t.Fatal("layout reverted when switching sessions")
	}
	if w.active.view.inventory.Interval() != time.Minute || w.live[profiles[0]].view.inventory.Interval() != time.Minute-intervalStep {
		t.Fatal("changing one session altered the other sampling interval")
	}
	w.disconnect(profiles[0])
	select {
	case p := <-closed:
		if p != profiles[0] {
			t.Fatal("wrong provider closed")
		}
	case <-time.After(time.Second):
		t.Fatal("provider not closed")
	}
	w.connect(config.Profile{Name: "pool", Type: config.XAPI, Address: "https://pool", User: "me"}, "")
	if w.dialog == nil || !strings.Contains(w.dialog.title, "LOGIN") {
		t.Fatal("saved XAPI connection did not prompt")
	}
}

func TestConnectionManagerMouseSelection(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(80, 24)
	w := newWorkspace(context.Background(), config.Profile{Name: "local", Type: config.Local}, time.Second, nil, config.Store{Path: filepath.Join(t.TempDir(), "config.json")})
	defer w.close()
	w.manager("")
	click := func(x, y int) {
		d := w.dialog
		d.draw(screen)
		d.mouse(tcell.NewEventMouse(x, y, tcell.ButtonPrimary, 0))
		d.mouse(tcell.NewEventMouse(x, y, tcell.ButtonNone, 0))
	}
	click(8, 3)
	if w.dialog.title != "ADD CONNECTION" {
		t.Fatal("mouse did not open connection types")
	}
	click(8, 5)
	if w.dialog.title != "CONNECTION SETTINGS" {
		t.Fatal("mouse did not select SSH")
	}
	w.dialog.key(tcell.NewEventKey(tcell.KeyEsc, 0, 0))
	if w.dialog.title != "SESSIONS" {
		t.Fatal("cancel did not return to sessions")
	}
}
