package tui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
)

type shortcut struct {
	label string
	key   tcell.Key
	char  rune
}

// Only fully visible buttons are clickable. Labels use single-cell ASCII.
type shortcutButton struct {
	shortcut
	x, y int
}

func (b *browser) drawFooter(screen tcell.Screen, width, top, height int) {
	b.buttons = b.buttons[:0]
	b.footer.SetRect(0, top, width, height)
	b.footer.Draw(screen)
	bottom := top + height - 1
	margin, spacing := 1, 2
	if width < 68 {
		margin, spacing = 0, 1
	}
	x, y := margin, bottom
	add := func(label string, key tcell.Key, char rune) {
		if x+len(label) > width {
			return
		}
		b.buttons = append(b.buttons, shortcutButton{shortcut{label, key, char}, x, y})
		paint(screen, x, y, len(label), ink, label)
		if end := strings.IndexByte(label, ' '); end > 1 {
			paint(screen, x+1, y, end-1, aqua, label[1:end])
		}
		x += len(label) + spacing
	}

	if b.edit != nil {
		const apply, cancel = "[Enter Apply]", "[Esc Cancel]"
		controlWidth := len(apply) + spacing + len(cancel)
		x = width
		if width >= controlWidth+16 {
			x = width - controlWidth
		}
		b.filter.SetRect(0, y, x, 1)
		b.filter.Draw(screen)
		add(apply, tcell.KeyEnter, 0)
		add(cancel, tcell.KeyEsc, 0)
		if height == 2 {
			paint(screen, 1, top, width-2, muted, fmt.Sprintf("%d of %d domains match", len(b.rows.visible), len(b.rows.Domains)))
		}
		return
	}

	if b.inspect != nil {
		add("[Esc Back]", tcell.KeyEsc, 0)
	} else {
		if _, ok := b.selected(); ok {
			inspect := "[Enter Inspect]"
			if width < 32 {
				inspect = "[Enter View]"
			}
			add(inspect, tcell.KeyEnter, 0)
		}
		add("[/ Filter]", tcell.KeyRune, '/')
		if b.filter.GetText() != "" {
			add("[Esc Clear]", tcell.KeyEsc, 0)
		}
		add("[o Sort]", tcell.KeyRune, 'o')
	}
	if b.guests != nil {
		add("[g Guests]", tcell.KeyRune, 'g')
	}
	if b.inspect != nil && height == 2 {
		paint(screen, x, y, width-x, muted, "↑↓ or wheel to scroll")
	}
	if height == 2 {
		x, y = margin, top
	}
	if b.sessions != nil {
		add("[s Sessions]", tcell.KeyRune, 's')
		if b.openCount > 1 {
			add("[Tab Next]", tcell.KeyTab, 0)
		}
	}
	if b.settings != nil {
		add("[, Settings]", tcell.KeyRune, ',')
	}
	if b.inventory != nil {
		add("[r Refresh]", tcell.KeyRune, 'r')
	}
	if b.inspect == nil {
		label := "[L " + layoutNames[b.layout] + "]"
		if width < 68 && x+len(label) > width {
			label = "[L View]"
		}
		add(label, tcell.KeyRune, 'L')
	}
	add("[q Quit]", tcell.KeyRune, 'q')
}
