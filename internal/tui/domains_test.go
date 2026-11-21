package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/advnpzn/xlui/internal/session"
	"github.com/advnpzn/xlui/internal/xen"
	"github.com/gdamore/tcell/v2"
)

func TestInspectorFollowsTheDomainInstance(t *testing.T) {
	a := xen.Domain{ID: 1, UUID: "a", Name: "dashboard"}
	b := xen.Domain{ID: 2, UUID: "b", Name: "sensors"}
	ui := newBrowser("test")
	ui.update(session.Snapshot{Domains: []xen.Domain{a, b}, ObservedAt: time.Now()})
	ui.table.Select(2, 0)
	ui.update(session.Snapshot{Domains: []xen.Domain{b, a}, ObservedAt: time.Now()})
	if row, _ := ui.table.GetSelection(); row != 1 {
		t.Fatalf("selected row %d after reorder", row)
	}
	ui.key(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if !strings.Contains(ui.detail.GetText(false), "sensors") {
		t.Fatal("inspector did not open for the selected domain")
	}
	replacement := xen.Domain{ID: 2, UUID: "c", Name: "replacement"}
	ui.update(session.Snapshot{Domains: []xen.Domain{replacement, a}, ObservedAt: time.Now()})
	if _, ok := ui.selected(); ok || !strings.Contains(ui.detail.GetText(false), "no longer") {
		t.Fatal("reused domid silently changed selection or inspector")
	}
}

func TestFilterAndFailureKeepLocalSnapshot(t *testing.T) {
	d := xen.Domain{ID: 3, UUID: "bb", Name: "Board [red]\nGuest"}
	ui := newBrowser("test")
	ui.update(session.Snapshot{Domains: []xen.Domain{d, {ID: 4, UUID: "aa", Name: "Other"}}, ObservedAt: time.Now()})
	ui.filter.SetText("bOaRd")
	if len(ui.rows.visible) != 1 || ui.rows.visible[0] != 0 {
		t.Fatalf("visible rows after filter: %v", ui.rows.visible)
	}
	cell := ui.rows.GetCell(1, 0)
	if strings.Contains(cell.Text, "\n") || strings.Contains(cell.Text, "[red]") {
		t.Fatalf("host name escaped incorrectly: %q", cell.Text)
	}
	ui.update(session.Snapshot{Domains: ui.rows.all, ObservedAt: ui.latest, Err: errors.New("offline")})
	if ui.lastError == nil || len(ui.rows.visible) != 1 || ui.rows.all[0] != d {
		t.Fatal("failed refresh cleared the last valid snapshot")
	}
}

func TestQuitCancelsInFlightRead(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(80, 24)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- runScreen(ctx, screen, "bench", time.Minute, func(ctx context.Context) ([]xen.Domain, error) {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		})
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("first inventory request did not start")
	}
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyRune, 'q', tcell.ModNone))
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("quit did not cancel and join the active reader")
	}
}

func BenchmarkFilteredDomainProjection(b *testing.B) {
	ui := newBrowser("bench")
	domains := make([]xen.Domain, 4096)
	for i := range domains {
		domains[i] = xen.Domain{ID: i, Name: fmt.Sprintf("guest-%04d", i), UUID: fmt.Sprintf("uuid-%d", i)}
	}
	ui.update(session.Snapshot{Domains: domains, ObservedAt: time.Now()})
	ui.filter.SetText("guest-1")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ui.project(domainKey{}, false)
	}
}
