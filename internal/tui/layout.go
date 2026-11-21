package tui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
)

// Layout specifies how the dashboard arranges live inventory and metrics.
type Layout uint8

const (
	Overview Layout = iota
	Inventory
	Split
)

var layoutNames = [...]string{"Overview", "Inventory", "Split"}

// ParseLayout reads the dashboard name used by --layout.
func ParseLayout(name string) (Layout, error) {
	switch name {
	case "overview":
		return Overview, nil
	case "inventory":
		return Inventory, nil
	case "split":
		return Split, nil
	default:
		return Overview, fmt.Errorf("invalid layout %q; choose overview, inventory or split", name)
	}
}

func (b *browser) setLayout(layout Layout) {
	if b.layout != layout {
		b.layout = layout
		b.pointer = pointerState{}
		b.table.Select(b.table.GetSelection())
	}
}

func (b *browser) drawInventoryTotals(s tcell.Screen, width, y int) {
	paint(s, 0, y, width, muted, fmt.Sprintf("CPU %s  ·  Allocated %s  ·  %d vCPUs", b.totalCPU(), b.totalAllocated(), b.stats.vcpus))
}

const sidebarHostHeight = 7
const minSidebarHeight = sidebarHostHeight + 9

func (b *browser) drawSidebar(s tcell.Screen, x, y, width, height int) {
	guestHeight := height - sidebarHostHeight
	panel(s, x, y, width, guestHeight, "SELECTED DOMAIN", aqua)
	left, innerWidth := x+2, width-4
	d, ok := b.selectedDomain()
	if ok {
		paint(s, left, y+1, innerWidth, ink, d.Name)
		paint(s, left, y+2, innerWidth, muted, fmt.Sprintf("%s · ID %s", stateLabel(d.State), number(d.ID)))
		paint(s, left, y+3, innerWidth, muted, number(d.VCPUs)+" vCPUs")
		paint(s, left, y+4, innerWidth, aqua, "CPU load  "+percent(b.rows.CPUPercent(d)))
		paint(s, left, y+6, innerWidth, ink, "Allocated  "+memoryLabel(float64(d.MemoryMiB)))
		paint(s, left, y+7, innerWidth, blue, "Guest used  "+guestMemory(d))
		if trend := b.stats.domains[keyOf(d)]; trend != nil {
			paint(s, left, y+5, innerWidth, aqua, trend.cpu.Bars(innerWidth))
			if guestHeight >= 10 {
				paint(s, left, y+8, innerWidth, blue, trend.ram.Bars(innerWidth))
			}
		}
	} else {
		paint(s, left, y+1, innerWidth, muted, "Select a domain.")
	}
	y += guestHeight
	panel(s, x, y, width, sidebarHostHeight, "HOST METRICS", blue)
	paint(s, left, y+1, innerWidth, aqua, "CPU load  "+b.totalCPU())
	paint(s, left, y+2, innerWidth, aqua, b.stats.host.cpu.Bars(innerWidth))
	paint(s, left, y+3, innerWidth, blue, "Allocated  "+b.totalAllocated())
	paint(s, left, y+4, innerWidth, blue, b.stats.host.ram.Bars(innerWidth))
	paint(s, left, y+5, innerWidth, muted, fmt.Sprintf("%d observed vCPUs", b.stats.vcpus))
}
