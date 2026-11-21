package tui

import (
	"fmt"
	"time"

	"github.com/advnpzn/xlui/internal/session"
	"github.com/advnpzn/xlui/internal/terminal"
	"github.com/advnpzn/xlui/internal/tui/widgets"
	"github.com/advnpzn/xlui/internal/xen"
	"github.com/rivo/tview"
)

// The inspector keeps two histories for one instance. Closing the inspector
// releases both and a different selection always starts with fresh histories.
type inspector struct {
	key        domainKey
	domain     xen.Domain
	cpu        widgets.Series
	memory     widgets.Series
	sequence   uint64
	observedAt time.Time
	boot       time.Time // last known boot of the inspected instance
	rate       string
	gone       bool
	dirty      bool
	width      int
	view       *tview.TextView
}

func newInspector(key domainKey, s session.Snapshot) *inspector {
	i := &inspector{key: key, view: tview.NewTextView().ScrollToBeginning()}
	i.view.SetBackgroundColor(surface)
	i.view.SetTextColor(ink)
	i.observe(s)
	return i
}

func (i *inspector) observe(s session.Snapshot) {
	if i.gone {
		return
	}
	if i.sequence != 0 && s.Sequence > i.sequence && s.Sequence-i.sequence > 1 {
		missed := s.Sequence - i.sequence - 1
		i.cpu.Skip(missed)
		i.memory.Skip(missed)
	}
	i.sequence = s.Sequence
	i.dirty = true
	for _, d := range s.Domains {
		if keyOf(d) == i.key {
			if !d.StartedAt.IsZero() && !d.StartedAt.After(s.ObservedAt) {
				if !i.boot.IsZero() && !i.boot.Equal(d.StartedAt) {
					i.gone = true
					return
				}
				i.boot = d.StartedAt
			}
			i.domain = d
			i.observedAt = s.ObservedAt
			pct, ok := s.CPUPercent(d)
			i.rate = percent(pct, ok)
			i.cpu.Push(pct, ok)
			i.memory.Push(float64(d.MemoryMiB), s.Err == nil && d.MemoryMiB >= 0)
			return
		}
	}
	i.gone = true
}

// Only rebuild the formatted text when a sample arrives or the terminal width changes.
func (i *inspector) render(width int) *tview.TextView {
	if !i.dirty && i.width == width {
		return i.view
	}
	i.width, i.dirty = width, false
	if i.gone {
		i.view.SetText("This domain instance is no longer in the inventory.")
		return i.view
	}
	d := i.domain
	cpuLine, cpuScale := i.cpu.Plot(width, "%")
	memLine, memScale := i.memory.Plot(width, "MiB")
	duration := "—"
	if d.CPUSeconds >= 0 {
		duration = fmt.Sprintf("%.1f s", d.CPUSeconds)
	}
	boot := ""
	if !d.StartedAt.IsZero() && !d.StartedAt.After(i.observedAt) {
		uptime := i.observedAt.Sub(d.StartedAt).Truncate(time.Second)
		boot = fmt.Sprintf("\nStarted    %s\nUptime     %s", d.StartedAt.UTC().Format(time.RFC3339), uptime)
	}
	i.view.SetText(fmt.Sprintf("%s\n\nUUID       %s\nID         %s\nState      %s%s\nAllocated  %s\nGuest used %s\nvCPUs      %s\nCPU load   %s (100%% = one core)\nCPU time   %s\n\nCPU      %s\n%s\nMemory   %s\n%s\nLast %d samples while open | newest right | ? unavailable",
		terminal.SingleLine(d.Name), terminal.SingleLine(d.UUID), number(d.ID), terminal.SingleLine(stateLabel(d.State)), boot, memoryLabel(float64(d.MemoryMiB)), guestMemory(d), number(d.VCPUs), i.rate, duration, cpuScale, cpuLine, memScale, memLine, widgets.Window))
	return i.view
}
