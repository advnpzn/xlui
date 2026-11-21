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

func TestInspectorBootObservation(t *testing.T) {
	start := time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)
	vm := xen.Domain{Name: "worker", ID: 5, UUID: "same", State: "Running", StartedAt: start}
	b := newBrowser("pool")
	s := session.Snapshot{Domains: []xen.Domain{vm}, ObservedAt: start.Add(70 * time.Second)}
	b.update(s)
	b.key(tcell.NewEventKey(tcell.KeyEnter, 0, 0))
	render := func() string { return b.inspect.render(80).GetText(false) }
	if text := render(); !strings.Contains(text, "2026-01-01T09:30:00Z") || !strings.Contains(text, "1m10s") {
		t.Fatalf("wrong boot information:\n%s", text)
	}
	s.Err = errors.New("pool offline")
	b.update(s)
	if !strings.Contains(render(), "1m10s") {
		t.Fatal("stale uptime changed")
	}
	s.Err = nil
	s.ObservedAt = s.ObservedAt.Add(time.Minute)
	for _, unknown := range []time.Time{{}, s.ObservedAt.Add(time.Hour)} {
		vm.StartedAt = unknown
		s.Domains = []xen.Domain{vm}
		b.update(s)
		if text := render(); strings.Contains(text, "Uptime") || strings.Contains(text, "Started") {
			t.Fatalf("rendered unavailable boot time:\n%s", text)
		}
	}
	vm.StartedAt = start.Add(time.Minute)
	s.Domains = []xen.Domain{vm}
	b.update(s)
	if !b.inspect.gone || strings.Contains(render(), "Uptime") {
		t.Fatal("inspector followed a different boot with a reused domid")
	}
}
