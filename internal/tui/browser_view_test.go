package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/advnpzn/xlui/internal/session"
	"github.com/advnpzn/xlui/internal/xen"
	"github.com/gdamore/tcell/v2"
)

func TestRenderedInventoryStates(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(80, 24)
	ui := newBrowser(strings.Repeat("bench", 10))
	render := func() string {
		t.Helper()
		ui.draw(screen)
		var result strings.Builder
		w, h := screen.Size()
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				r, _, _, _ := screen.GetContent(x, y)
				result.WriteRune(r)
			}
			result.WriteByte('\n')
		}
		return result.String()
	}
	expect := func(parts ...string) {
		t.Helper()
		frame := render()
		for _, part := range parts {
			if !strings.Contains(frame, part) {
				t.Fatalf("missing %q in\n%s", part, frame)
			}
		}
	}
	observed := time.Unix(1, 0)
	expect("Loading", "Waiting for the first inventory")
	ui.update(session.Snapshot{Err: errors.New("connection offline")})
	expect("Unavailable", "connection offline", "Retrying automatically.")
	ui.update(session.Snapshot{ObservedAt: observed})
	expect("Live", "No domains in the last sample.")
	domains := []xen.Domain{{ID: 2, Name: "worker", UUID: "a"}}
	ui.update(session.Snapshot{Domains: domains, ObservedAt: observed.Add(time.Second)})
	ui.filter.SetText("missing")
	expect("No matching domains", "Esc to clear")
	ui.key(tcell.NewEventKey(tcell.KeyEsc, 0, tcell.ModNone))
	ui.update(session.Snapshot{Domains: domains, ObservedAt: ui.latest, Err: errors.New("offline")})
	screen.SetSize(26, 6)
	expect("Stale", "worker")
	ui.update(session.Snapshot{Domains: domains, ObservedAt: observed.Add(2 * time.Second)})
	expect("Live", "worker")
	ui.key(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	ui.key(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	expect("UUID")
	ui.key(tcell.NewEventKey(tcell.KeyEnd, 0, tcell.ModNone))
	expect("Last 60 samples while", "newest right")
	ui.update(session.Snapshot{Domains: domains, ObservedAt: observed.Add(3 * time.Second)})
	expect("Last 60 samples while", "newest right")
	ui.key(tcell.NewEventKey(tcell.KeyEsc, 0, tcell.ModNone))
	ui.key(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	expect("UUID")
	screen.SetSize(8, 1)
	expect("Live")
}

func TestCancelFilterRestoresOnlyTheOriginalInstance(t *testing.T) {
	ui := newBrowser("bench")
	a := xen.Domain{ID: 2, Name: "worker", UUID: "a"}
	b := xen.Domain{ID: 3, Name: "other", UUID: "b"}
	observed := time.Unix(1, 0)
	ui.update(session.Snapshot{Domains: []xen.Domain{a, b}, ObservedAt: observed})
	ui.key(tcell.NewEventKey(tcell.KeyRune, '/', tcell.ModNone))
	ui.filter.SetText("other")
	ui.update(session.Snapshot{Domains: []xen.Domain{b, a}, ObservedAt: observed.Add(time.Second)})
	ui.key(tcell.NewEventKey(tcell.KeyEsc, 0, tcell.ModNone))
	if selected, ok := ui.selected(); !ok || selected != keyOf(a) || ui.filter.GetText() != "" {
		t.Fatalf("cancelled edit: selection %v, filter %q", selected, ui.filter.GetText())
	}
	ui.key(tcell.NewEventKey(tcell.KeyRune, '/', tcell.ModNone))
	ui.filter.SetText("worker")
	ui.key(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	ui.key(tcell.NewEventKey(tcell.KeyRune, '/', tcell.ModNone))
	ui.filter.SetText("other")
	a.UUID = "replacement"
	ui.update(session.Snapshot{Domains: []xen.Domain{a, b}, ObservedAt: observed.Add(2 * time.Second)})
	ui.key(tcell.NewEventKey(tcell.KeyEsc, 0, tcell.ModNone))
	if _, ok := ui.selected(); ok || ui.filter.GetText() != "worker" {
		t.Fatal("cancel selected a replacement or lost the applied filter")
	}
}
