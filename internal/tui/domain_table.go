package tui

import (
	"github.com/advnpzn/xlui/internal/session"
	"github.com/advnpzn/xlui/internal/terminal"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"math"
	"strings"
)

type domainRows struct {
	tview.TableContentReadOnly
	session.Snapshot
	visible []int
	meters  bool
	order   domainOrder
}

var columns = [...]string{"DOMAIN", "ID", "STATE", "VCPUS", "CPU LOAD", "ALLOCATED", "GUEST USED"}

func (r *domainRows) GetColumnCount() int { return len(columns) }
func (r *domainRows) GetRowCount() int    { return len(r.visible) + 1 }

// tview requests visible cells, so a repaint formats only rows that fit on screen.
func (r *domainRows) GetCell(row, col int) *tview.TableCell {
	if col < 0 || col >= len(columns) || row < 0 || row > len(r.visible) {
		return nil
	}
	if row == 0 {
		title := columns[col]
		if col == 0 && r.order != orderProvider {
			title += " / " + orderNames[r.order]
		}
		return tview.NewTableCell(title).SetSelectable(false).SetAttributes(tcell.AttrBold).SetTextColor(aqua)
	}
	d := r.Domains[r.visible[row-1]]
	color := ink
	var text string
	switch col {
	case 0:
		return tview.NewTableCell(tview.Escape(terminal.SingleLine(d.Name))).SetMaxWidth(28).SetExpansion(1)
	case 1:
		color = muted
		text = number(d.ID)
	case 2:
		text = tview.Escape(terminal.SingleLine(stateLabel(d.State)))
		switch text {
		case "Running":
			color = aqua
		case "Crashed", "Dying":
			color = tcell.ColorIndianRed
		case "Paused", "Shutdown":
			color = tcell.ColorKhaki
		default:
			color = muted
		}
	case 3:
		text = number(d.VCPUs)
	case 4:
		pct, ok := r.CPUPercent(d)
		text = percent(pct, ok)
		color = aqua
		if r.meters && ok && d.VCPUs > 0 {
			text = meter(pct/(float64(d.VCPUs)*100)) + " " + text
		}
	case 5:
		text = memoryLabel(float64(d.MemoryMiB))
	case 6:
		text = guestMemory(d)
		color = blue
		if r.meters && d.GuestUsedKiB != nil && d.MemoryMiB > 0 {
			text = meter(float64(*d.GuestUsedKiB)/(float64(d.MemoryMiB)*1024)) + " " + text
		}
	}
	return tview.NewTableCell(text).SetTextColor(color)
}

func meter(fraction float64) string {
	n := int(math.Round(min(1, max(0, fraction)) * 6))
	return strings.Repeat("▰", n) + strings.Repeat("▱", 6-n)
}
