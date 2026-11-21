package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/advnpzn/xlui/internal/config"
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
	if !strings.Contains(ui.inspect.render(80).GetText(false), "sensors") {
		t.Fatal("inspector did not open for the selected domain")
	}
	replacement := xen.Domain{ID: 2, UUID: "c", Name: "replacement"}
	ui.update(session.Snapshot{Domains: []xen.Domain{replacement, a}, ObservedAt: time.Now()})
	if _, ok := ui.selected(); ok || !strings.Contains(ui.inspect.render(80).GetText(false), "no longer") {
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
	ui.update(session.Snapshot{Domains: ui.rows.Domains, ObservedAt: ui.latest, Err: errors.New("offline")})
	if ui.lastError == nil || len(ui.rows.visible) != 1 || ui.rows.Domains[0] != d {
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
		done <- runScreen(ctx, screen, config.Profile{Name: "bench", Type: config.Local}, time.Minute, "", func(config.Profile, string) (session.Source, error) {
			return session.Source{Read: func(ctx context.Context) ([]xen.Domain, error) {
				close(started)
				<-ctx.Done()
				return nil, ctx.Err()
			}}, nil
		}, config.Store{Path: t.TempDir() + "/config.json"})
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

func TestInspectorHistoryStaysWithTheOpenInstance(t *testing.T) {
	d := xen.Domain{ID: 1, UUID: "a", Name: "guest", MemoryMiB: 128}
	start := time.Now()
	s := session.Snapshot{Domains: []xen.Domain{d}, Sequence: 1, ObservedAt: start}
	ui := newBrowser("host")
	ui.update(s)
	ui.key(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	d.MemoryMiB = 256
	s = session.Snapshot{Domains: []xen.Domain{d}, Sequence: 4, ObservedAt: start.Add(time.Second)}
	ui.update(s)
	s.Sequence++
	s.Err = errors.New("offline")
	ui.update(s)
	if line, scale := ui.inspect.memory.Plot(60, "MiB"); line != ".??@?" || scale != "128..256 MiB" {
		t.Fatalf("memory history %q, %q", line, scale)
	}
	// A redraw must never append a sample.
	ui.inspect.render(80)
	ui.inspect.render(22)
	if line, _ := ui.inspect.cpu.Plot(60, "%"); line != "?????" {
		t.Fatalf("CPU history advanced on redraw or fabricated missing data: %q", line)
	}
	ui.key(tcell.NewEventKey(tcell.KeyEsc, 0, tcell.ModNone))
	ui.key(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if line, _ := ui.inspect.memory.Plot(60, "MiB"); line != "?" {
		t.Fatalf("reopened inspector reused previous history or treated stale memory as current: %q", line)
	}
}

func TestTableDistinguishesIdleCPUFromUnknown(t *testing.T) {
	d := xen.Domain{ID: 1, UUID: "a", CPUSeconds: 10, VCPUs: 1}
	input := make(chan []xen.Domain, 1)
	input <- []xen.Domain{d}
	stream, err := session.WatchInventory(context.Background(), 50*time.Millisecond, func(ctx context.Context) ([]xen.Domain, error) {
		select {
		case domains := <-input:
			return domains, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	ui := newBrowser("host")
	readCPU := func() string {
		t.Helper()
		select {
		case snap := <-stream.Updates():
			ui.update(snap)
			return ui.rows.GetCell(1, 4).Text
		case <-time.After(3 * time.Second):
			t.Fatal("missing inventory sample")
		}
		return ""
	}
	if got := readCPU(); got != "—" {
		t.Fatalf("first CPU reading %q", got)
	}
	input <- []xen.Domain{d}
	if got := readCPU(); got != "0.0%" {
		t.Fatalf("idle CPU reading %q", got)
	}
}

func TestChartsReachableOnShortTerminal(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(40, 10)
	d := xen.Domain{ID: 1, UUID: "a", Name: "guest"}
	s := session.Snapshot{Domains: []xen.Domain{d}, ObservedAt: time.Now(), Sequence: 1}
	ui := newBrowser("bench")
	ui.update(s)
	ui.key(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	ui.draw(screen)
	ui.key(tcell.NewEventKey(tcell.KeyEnd, 0, tcell.ModNone))
	ui.draw(screen)
	first, _ := ui.inspect.view.GetScrollOffset()
	if first <= 0 {
		t.Fatal("End did not scroll down to the charts")
	}
	s.Sequence++
	s.ObservedAt = s.ObservedAt.Add(time.Second)
	ui.update(s)
	ui.draw(screen)
	if row, _ := ui.inspect.view.GetScrollOffset(); row < first {
		t.Fatalf("refresh moved view from offset %d to %d", first, row)
	}
	var display strings.Builder
	for y := 0; y < 10; y++ {
		for x := 0; x < 40; x++ {
			ch, _, _, _ := screen.GetContent(x, y)
			display.WriteRune(ch)
		}
		display.WriteByte('\n')
	}
	if !strings.Contains(display.String(), "Last 60 samples while open") {
		t.Fatalf("missing history legend in scrolled view:\n%s", display.String())
	}
}
