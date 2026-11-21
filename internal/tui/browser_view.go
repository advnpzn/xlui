package tui

import (
	"fmt"
	"time"

	"github.com/advnpzn/xlui/internal/terminal"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Keep freshness ahead of variable-length host names so a narrow terminal
// still distinguishes a live observation from a cached one.
func (b *browser) drawHeader(screen tcell.Screen, width int) {
	status, color := "Loading", muted
	if !b.latest.IsZero() {
		status, color = "Live "+b.latest.Format("15:04:05"), aqua
	}
	if b.lastError != nil {
		status, color = "Unavailable", tcell.ColorKhaki
		if !b.latest.IsZero() {
			status = "Stale " + b.latest.Format("15:04:05")
		}
	}
	b.header.SetRect(0, 0, width, 1)
	b.header.Draw(screen)
	b.refreshButtons = b.refreshButtons[:0]
	left, right := 0, width
	if width >= 68 {
		paint(screen, 1, 0, 4, ink, "XLUI")
		left = 7
	}
	if b.inventory != nil && b.edit == nil {
		interval := b.inventory.Interval()
		value := fmt.Sprintf("%d ms", interval.Milliseconds())
		if interval%time.Millisecond != 0 {
			value = interval.String()
		}
		label := " [-] " + value + " [+] "
		x := width - len(label)
		if x >= left+len(status)+10 {
			paint(screen, x, 0, len(label), aqua, label)
			b.refreshButtons = append(b.refreshButtons,
				shortcutButton{shortcut{"[-]", tcell.KeyRune, '-'}, x + 1, 0},
				shortcutButton{shortcut{"[+]", tcell.KeyRune, '+'}, width - 4, 0},
			)
			right = x
		}
	}
	paint(screen, left, 0, right-left, color, status)
	left += len(status)
	paint(screen, left, 0, right-left, ink, "  │  "+b.host)
}

func (b *browser) emptyMessage() string {
	switch {
	case b.latest.IsZero() && b.lastError != nil:
		hint := "Retrying automatically."
		if b.sessions != nil {
			hint = "Press s to manage connections."
		}
		return "Inventory unavailable\n" + terminal.SingleLine(b.lastError.Error()) + "\n" + hint
	case b.latest.IsZero():
		return "Waiting for the first inventory sample."
	case len(b.rows.Domains) == 0:
		return "No domains in the last sample."
	case b.edit != nil:
		return "No matching domains.\nEdit the filter, Enter to apply or Esc to cancel."
	default:
		return "No matching domains.\nPress / to edit the filter or Esc to clear it."
	}
}

func (b *browser) inventoryHeading() string {
	title := fmt.Sprintf("DOMAINS  %d/%d", len(b.rows.visible), len(b.rows.Domains))
	if query := b.filter.GetText(); b.edit == nil && query != "" {
		title += "  ·  Filter: " + query
	}
	return title
}

func (b *browser) draw(screen tcell.Screen) {
	width, height := screen.Size()
	if width < 1 || height < 1 {
		return
	}
	screen.Clear()
	screen.HideCursor()
	b.drawHeader(screen, width)
	if height == 1 {
		screen.Show()
		return
	}
	footerHeight := 1
	if width >= 40 && height >= 10 {
		footerHeight = 2
	}
	footerTop := height - footerHeight
	top, bottom := 1, footerTop
	if b.lastError != nil && !b.latest.IsZero() && width >= 3 && height >= 8 {
		message := "Refresh failed: " + terminal.SingleLine(b.lastError.Error())
		lines := 1
		if height >= 14 && tview.TaggedStringWidth(tview.Escape(message)) > width-2 {
			lines = 2
		}
		b.notice.SetRect(1, top, width-2, lines)
		b.notice.SetText(message).Draw(screen)
		top += lines
	}
	if b.inspect == nil && width >= 68 && height >= 20 {
		b.drawSummary(screen, width, top)
		top += 5
		if bottom-top >= 13 {
			bottom -= 4
			b.drawSelected(screen, width, bottom)
		}
	}
	left, innerWidth := 0, width
	if width >= 54 && bottom-top >= 8 {
		title := "DOMAIN INSPECTOR"
		if b.inspect == nil {
			title = b.inventoryHeading()
		}
		panel(screen, 0, top, width, bottom-top, title, aqua)
		left, innerWidth = 2, width-4
		top, bottom = top+1, bottom-1
	} else if b.inspect == nil && width >= 32 && bottom-top >= 7 {
		paint(screen, 0, top, width, muted, b.inventoryHeading())
		top++
	}
	if bottom > top {
		b.rows.layout(innerWidth)
		content := tview.Primitive(b.table)
		if b.inspect != nil {
			content = b.inspect.render(innerWidth)
		} else if len(b.rows.visible) == 0 {
			content = b.message.SetText(b.emptyMessage())
		}
		content.SetRect(left, top, innerWidth, bottom-top)
		content.Draw(screen)
	}
	b.drawFooter(screen, width, footerTop, footerHeight)
	screen.Show()
}
