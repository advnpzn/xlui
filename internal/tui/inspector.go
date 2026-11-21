package tui

import (
	"fmt"

	"github.com/advnpzn/xlui/internal/session"
	"github.com/advnpzn/xlui/internal/terminal"
	"github.com/advnpzn/xlui/internal/tui/widgets"
	"github.com/advnpzn/xlui/internal/xen"
	"github.com/rivo/tview"
)

// The inspector keeps two histories for one instance. Closing the inspector
// releases both and a different selection always starts with fresh histories.
type inspector struct {
	key      domainKey
	domain   xen.Domain
	cpu      widgets.Series
	memory   widgets.Series
	sequence uint64
	rate     string
	gone     bool
	dirty    bool
	width    int
	view     *tview.TextView
}

func newInspector(key domainKey, s session.Snapshot) *inspector {
	i := &inspector{key: key, view: tview.NewTextView().ScrollToBeginning()}
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
			i.domain = d
			pct, ok := s.CPUPercent(d)
			i.rate = formatPercent(pct, ok)
			i.cpu.Push(pct, ok)
			i.memory.Push(float64(d.MemoryMiB), s.Err == nil)
			return
		}
	}
	i.gone = true
}

func formatPercent(pct float64, available bool) string {
	if !available {
		return "--"
	}
	return fmt.Sprintf("%.1f", pct)
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
	i.view.SetText(fmt.Sprintf("%s\n\nUUID     %s\nID       %d\nState    %s\nMemory   %d MiB\nvCPUs    %d\nCPU %%    %s (100%% = one core)\nCPU time %.1f s\n\nCPU      %s\n%s\nMemory   %s\n%s\nLast %d samples while open | newest right | ? unavailable",
		terminal.SingleLine(d.Name), terminal.SingleLine(d.UUID), d.ID, terminal.SingleLine(d.State), d.MemoryMiB, d.VCPUs,
		i.rate, d.CPUSeconds, cpuScale, cpuLine, memScale, memLine, widgets.Window))
	return i.view
}
