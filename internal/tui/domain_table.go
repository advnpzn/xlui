package tui

import (
	"math"
	"strings"

	"github.com/advnpzn/xlui/internal/config"
	"github.com/advnpzn/xlui/internal/session"
	"github.com/advnpzn/xlui/internal/terminal"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type domainRows struct {
	tview.TableContentReadOnly
	session.Snapshot
	visible []int
	meters  bool
	order   config.DomainOrder
	columns []domainColumn
	widths  [colGuestUsed + 1]int
}

type domainColumn int

const (
	colName domainColumn = iota
	colID
	colState
	colVCPUs
	colCPU
	colAllocated
	colGuestUsed
)

var columnTitles = [...]string{"DOMAIN", "ID", "STATE", "VCPUS", "CPU LOAD", "ALLOCATED", "GUEST USED"}

// Budget space for the metrics first so long names and changing readings
// cannot push them out of the viewport or shift their positions.
func (r *domainRows) layout(width int, sidebar bool) {
	r.widths = [colGuestUsed + 1]int{0, 6, 9, 5, 9, 10, 10}
	detailWidth, fullWidth := 54, 66
	if sidebar {
		// The sidebar already shows identity and guest memory for the selection.
		detailWidth, fullWidth = 76, 84
	}
	r.meters = width >= 90
	if r.meters {
		r.widths[colCPU] += 7
		r.widths[colGuestUsed] += 7
	}
	r.columns = append(r.columns[:0], colName)
	if width >= detailWidth {
		r.columns = append(r.columns, colID)
	}
	if width >= 19 {
		r.columns = append(r.columns, colState)
	}
	if width >= fullWidth {
		r.columns = append(r.columns, colVCPUs)
	}
	if width >= 31 {
		r.columns = append(r.columns, colCPU)
	}
	if width >= 43 {
		r.columns = append(r.columns, colAllocated)
	}
	if width >= detailWidth {
		r.columns = append(r.columns, colGuestUsed)
	}
	r.widths[colName] = width
	for _, col := range r.columns[1:] {
		r.widths[colName] -= r.widths[col] + 1
	}
}

func (r *domainRows) GetColumnCount() int { return len(r.columns) }
func (r *domainRows) GetRowCount() int    { return len(r.visible) + 1 }

// tview requests visible cells, so a repaint formats only rows that fit on screen.
func (r *domainRows) GetCell(row, index int) *tview.TableCell {
	if index < 0 || index >= len(r.columns) || row < 0 || row > len(r.visible) {
		return nil
	}
	col := r.columns[index]
	width := r.widths[col]
	align := tview.AlignRight
	if col == colName || col == colState {
		align = tview.AlignLeft
	}
	if row == 0 {
		title := columnTitles[col]
		if col == colName && r.order != config.OrderProvider && len(title)+3+len(orderNames[r.order]) <= width {
			title += " / " + orderNames[r.order]
		}
		padding := strings.Repeat(" ", max(0, width-len(title)))
		if align == tview.AlignLeft {
			title += padding
		} else {
			title = padding + title
		}
		return tview.NewTableCell(title).SetMaxWidth(width).SetSelectable(false).SetAttributes(tcell.AttrBold).SetTextColor(aqua)
	}
	d := r.Domains[r.visible[row-1]]
	color := ink
	var text string
	switch col {
	case colName:
		text = tview.Escape(terminal.SingleLine(d.Name))
	case colID:
		color = muted
		text = number(d.ID)
	case colState:
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
	case colVCPUs:
		text = number(d.VCPUs)
	case colCPU:
		pct, ok := r.CPUPercent(d)
		text = percent(pct, ok)
		color = aqua
		if r.meters && ok && d.VCPUs > 0 {
			text = meter(pct/(float64(d.VCPUs)*100)) + " " + text
		}
	case colAllocated:
		text = memoryLabel(float64(d.MemoryMiB))
	case colGuestUsed:
		text = guestMemory(d)
		color = blue
		if r.meters && d.GuestUsedKiB != nil && d.MemoryMiB > 0 {
			text = meter(float64(*d.GuestUsedKiB)/(float64(d.MemoryMiB)*1024)) + " " + text
		}
	}
	return tview.NewTableCell(text).SetMaxWidth(width).SetAlign(align).SetTextColor(color)
}

func meter(fraction float64) string {
	n := int(math.Round(min(1, max(0, fraction)) * 6))
	return strings.Repeat("▪", n) + strings.Repeat("▫", 6-n)
}
