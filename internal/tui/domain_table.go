package tui

import (
	"strconv"

	"github.com/advnpzn/xlui/internal/terminal"
	"github.com/advnpzn/xlui/internal/xen"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type domainRows struct {
	tview.TableContentReadOnly
	all     []xen.Domain
	visible []int
}

var columns = [...]string{"NAME", "ID", "STATE", "MiB", "vCPUs", "CPU sec"}

func (r *domainRows) GetColumnCount() int { return len(columns) }
func (r *domainRows) GetRowCount() int    { return len(r.visible) + 1 }

// tview requests visible cells, so a repaint formats only rows that fit on screen.
func (r *domainRows) GetCell(row, col int) *tview.TableCell {
	if col < 0 || col >= len(columns) || row < 0 || row > len(r.visible) {
		return nil
	}
	if row == 0 {
		return tview.NewTableCell(columns[col]).SetSelectable(false).SetAttributes(tcell.AttrBold)
	}
	d := r.all[r.visible[row-1]]
	var text string
	switch col {
	case 0:
		return tview.NewTableCell(tview.Escape(terminal.SingleLine(d.Name))).SetMaxWidth(28).SetExpansion(1)
	case 1:
		text = strconv.Itoa(d.ID)
	case 2:
		text = tview.Escape(terminal.SingleLine(d.State))
	case 3:
		text = strconv.FormatInt(d.MemoryMiB, 10)
	case 4:
		text = strconv.Itoa(d.VCPUs)
	case 5:
		text = strconv.FormatFloat(d.CPUSeconds, 'f', 1, 64)
	}
	return tview.NewTableCell(text)
}
