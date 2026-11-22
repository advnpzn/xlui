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

func TestExpandedInspectorMemoryCharts(t *testing.T) {
	zero, used := int64(0), int64(1536)
	d := xen.Domain{ID: 4, UUID: "test-vm", Name: "worker", MemoryMiB: 128, GuestUsedKiB: &zero}
	s := session.Snapshot{Sequence: 1, ObservedAt: time.Now(), Domains: []xen.Domain{d}}
	ui := newBrowser("lab")
	ui.update(s)
	enter := tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone)
	ui.key(enter)

	// Usage disappears on the next successful refresh. Allocation is still known.
	d.GuestUsedKiB = nil
	s.Sequence, s.Domains, s.ObservedAt = 2, []xen.Domain{d}, s.ObservedAt.Add(time.Second)
	ui.update(s)
	// A dropped sequence precedes the next pair of independent measurements.
	d.MemoryMiB, d.GuestUsedKiB = 256, &used
	s.Sequence, s.Domains, s.ObservedAt = 4, []xen.Domain{d}, s.ObservedAt.Add(2*time.Second)
	ui.update(s)
	// A failed poll carries the last successful values and timestamp.
	s.Sequence, s.Err = 5, errors.New("offline")
	ui.update(s)

	assertCharts := func(allocated, guest string) {
		t.Helper()
		body := strings.Join(strings.Fields(ui.inspect.render(80).GetText(false)), " ")
		for _, chart := range []string{"Allocated " + allocated, "Guest used " + guest} {
			if !strings.Contains(body, chart) {
				t.Errorf("missing chart %q in inspector:\n%s", chart, body)
			}
		}
	}
	assertCharts("128..256 MiB ..?@?", "0..1.5 MiB .??@?")

	ui.key(tcell.NewEventKey(tcell.KeyEsc, 0, tcell.ModNone))
	ui.key(enter)
	assertCharts("unavailable ?", "unavailable ?")
}
