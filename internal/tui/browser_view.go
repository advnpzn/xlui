package tui

import (
	"github.com/advnpzn/xlui/internal/terminal"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Keep freshness ahead of variable-length host names so a narrow terminal
// still distinguishes a live observation from a cached one.
func (b *browser) drawHeader(screen tcell.Screen, width int) {
	status, color := "Loading", tcell.ColorDefault
	if !b.latest.IsZero() {
		status, color = "Live "+b.latest.Format("15:04:05"), tcell.ColorGreen
	}
	if b.lastError != nil {
		status, color = "Unavailable", tcell.ColorYellow
		if !b.latest.IsZero() {
			status = "Stale " + b.latest.Format("15:04:05")
		}
	}
	title := status + " | " + b.host
	if b.lastError != nil && !b.latest.IsZero() {
		title += " | " + terminal.SingleLine(b.lastError.Error())
	}
	b.header.SetTextColor(color).SetText(title)
	b.header.SetRect(0, 0, width, 1)
	b.header.Draw(screen)
}

func (b *browser) emptyMessage() string {
	switch {
	case b.latest.IsZero() && b.lastError != nil:
		return "Inventory unavailable\n" + terminal.SingleLine(b.lastError.Error()) + "\nRetrying automatically."
	case b.latest.IsZero():
		return "Waiting for the first inventory sample."
	case len(b.rows.all) == 0:
		return "No domains in the last sample."
	case b.edit != nil:
		return "No matching domains.\nEdit the filter, Enter to apply or Esc to cancel."
	default:
		return "No matching domains.\nPress / to edit the filter or Esc to clear it."
	}
}

func (b *browser) draw(screen tcell.Screen) {
	width, height := screen.Size()
	if width < 1 || height < 1 {
		return
	}
	screen.HideCursor()
	b.drawHeader(screen, width)
	if height == 1 {
		screen.Show()
		return
	}
	content := tview.Primitive(b.table)
	if b.inspect != nil {
		content = b.detail
	} else if len(b.rows.visible) == 0 {
		content = b.message.SetText(b.emptyMessage())
	}
	if height > 2 {
		content.SetRect(0, 1, width, height-2)
		content.Draw(screen)
	}
	b.drawFooter(screen, width, height-1)
	screen.Show()
}
