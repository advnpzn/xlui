package tui

import (
	"fmt"
	"math"
	"strings"

	"github.com/advnpzn/xlui/internal/session"
	"github.com/advnpzn/xlui/internal/terminal"
	"github.com/advnpzn/xlui/internal/tui/widgets"
	"github.com/advnpzn/xlui/internal/xen"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

var (
	aqua    = tcell.NewHexColor(0x67e8db)
	blue    = tcell.NewHexColor(0x8db4ff)
	ink     = tcell.NewHexColor(0xe5edf5)
	muted   = tcell.NewHexColor(0xa2b4c7)
	edge    = tcell.NewHexColor(0x405569)
	surface = tcell.NewHexColor(0x101b27)
)

type observation struct{ cpu, ram float64 }

type trend struct{ cpu, ram widgets.Series }

func (t *trend) push(p observation) {
	t.cpu.Push(p.cpu, !math.IsNaN(p.cpu))
	t.ram.Push(p.ram, !math.IsNaN(p.ram))
}
func (t *trend) skip(n uint64) { t.cpu.Skip(n); t.ram.Skip(n) }

type dashboard struct {
	domains           map[domainKey]*trend
	host              trend
	sum               observation
	cpus, mems, vcpus int
}

func (g *dashboard) sample(s session.Snapshot) {
	domains := s.Domains
	hist := make(map[domainKey]*trend, len(domains))
	g.sum = observation{}
	g.cpus, g.mems, g.vcpus = 0, 0, 0
	for _, d := range domains {
		k := keyOf(d)
		t := g.domains[k]
		if t == nil {
			t = &trend{}
		}
		hist[k] = t
		p := observation{math.NaN(), math.NaN()}
		if v, ok := s.CPUPercent(d); ok {
			p.cpu = v
			g.sum.cpu += p.cpu
			g.cpus++
		}
		if d.MemoryMiB >= 0 {
			g.sum.ram += float64(d.MemoryMiB)
			g.mems++
		}
		if d.GuestUsedKiB != nil {
			p.ram = float64(*d.GuestUsedKiB) / 1024
		}
		if d.VCPUs > 0 {
			g.vcpus += d.VCPUs
		}
		t.push(p)
	}
	g.domains = hist
	if g.cpus == 0 {
		g.sum.cpu = math.NaN()
	}
	if g.mems == 0 {
		g.sum.ram = math.NaN()
	}
	g.host.push(g.sum)
}
func (g *dashboard) skip(n uint64) {
	g.host.skip(n)
	for _, t := range g.domains {
		t.skip(n)
	}
}

func paint(s tcell.Screen, x, y, w int, color tcell.Color, text string) {
	if w > 0 {
		tview.Print(s, tview.Escape(terminal.SingleLine(text)), x, y, w, tview.AlignLeft, color)
	}
}
func panel(s tcell.Screen, x, y, w, h int, title string, accent tcell.Color) {
	box := tview.NewBox().SetBackgroundColor(surface).SetBorder(true).SetBorderColor(edge)
	box.SetRect(x, y, w, h)
	box.Draw(s)
	paint(s, x+2, y, w-4, accent, " "+title+" ")
}

func memoryLabel(mib float64) string {
	if math.IsNaN(mib) || mib < 0 {
		return "—"
	}
	if mib >= 1024 {
		return fmt.Sprintf("%.1f GiB", mib/1024)
	}
	return fmt.Sprintf("%.0f MiB", mib)
}
func guestMemory(d xen.Domain) string {
	if d.GuestUsedKiB == nil {
		return "—"
	}
	return memoryLabel(float64(*d.GuestUsedKiB) / 1024)
}
func percent(v float64, valid bool) string {
	if !valid || math.IsNaN(v) {
		return "—"
	}
	return fmt.Sprintf("%.1f%%", v)
}
func number(n int) string {
	if n < 0 {
		return "—"
	}
	return fmt.Sprint(n)
}
func stateLabel(state string) string {
	if len(state) != 6 || !strings.Contains(state, "-") {
		return state
	}
	for i, r := range state {
		if r != '-' {
			switch i {
			case 0:
				return "Running"
			case 1:
				return "Blocked"
			case 2:
				return "Paused"
			case 3:
				return "Shutdown"
			case 4:
				return "Crashed"
			case 5:
				return "Dying"
			}
		}
	}
	return state
}

func (b *browser) drawSummary(s tcell.Screen, w, y int) {
	g := &b.stats
	unit := w / 3
	panel(s, 0, y, unit, 5, "VCPU LOAD", aqua)
	panel(s, unit, y, unit, 5, "ALLOCATED MEMORY", blue)
	panel(s, 2*unit, y, w-2*unit, 5, "INVENTORY", muted)
	cpu := "—"
	if g.cpus > 0 && b.lastError == nil {
		cpu = percent(g.sum.cpu, true)
	}
	mem := "—"
	if g.mems > 0 {
		mem = memoryLabel(g.sum.ram)
	}
	paint(s, 2, y+1, unit-4, ink, cpu)
	paint(s, unit+2, y+1, unit-4, ink, mem)
	paint(s, 2, y+2, unit-4, aqua, g.host.cpu.Bars(unit-4))
	paint(s, unit+2, y+2, unit-4, blue, g.host.ram.Bars(unit-4))
	paint(s, 2, y+3, unit-4, muted, fmt.Sprintf("%d domains reporting", g.cpus))
	paint(s, unit+2, y+3, unit-4, muted, fmt.Sprintf("%d domains reporting", g.mems))
	paint(s, 2*unit+2, y+1, w-2*unit-4, ink, fmt.Sprintf("%d domains", len(b.rows.Domains)))
	paint(s, 2*unit+2, y+2, w-2*unit-4, muted, fmt.Sprintf("%d observed vCPUs", g.vcpus))
	paint(s, 2*unit+2, y+3, w-2*unit-4, muted, fmt.Sprintf("Open sessions  %d", b.openCount))
}

func (b *browser) drawSelected(s tcell.Screen, w, y int) {
	panel(s, 0, y, w, 4, "SELECTED DOMAIN", aqua)
	key, ok := b.selected()
	if !ok {
		paint(s, 2, y+2, w-4, muted, "Use ↑↓ or click a domain to select it.")
		return
	}
	row, _ := b.table.GetSelection()
	d := b.rows.Domains[b.rows.visible[row-1]]
	paint(s, 2, y+1, w-4, ink, fmt.Sprintf("%s   ·   %s   ·   ID %s", d.Name, stateLabel(d.State), number(d.ID)))
	mid := w / 2
	t := b.stats.domains[key]
	paint(s, 2, y+2, mid-4, aqua, "CPU  "+percent(b.rows.CPUPercent(d))+"   "+t.cpu.Bars(mid-22))
	paint(s, mid, y+2, w-mid-2, blue, "Guest used  "+guestMemory(d)+"   "+t.ram.Bars(w-mid-24))
}
