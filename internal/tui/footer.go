package tui

import (
	"fmt"
	"strings"

	"github.com/advnpzn/xlui/internal/terminal"
	"github.com/gdamore/tcell/v2"
)

type shortcut struct {
	label string
	key   tcell.Key
	char  rune
}

// Only fully visible buttons are clickable. Labels use single-cell ASCII.
type footerButton struct {
	shortcut
	x int
}

func (b *browser) drawFooter(screen tcell.Screen, width, y int) {
	var line strings.Builder
	start := 0
	b.buttons = b.buttons[:0]
	add := func(label string, key tcell.Key, char rune) {
		x := start + line.Len()
		if x+len(label) > width {
			return
		}
		b.buttons = append(b.buttons, footerButton{shortcut{label, key, char}, x})
		line.WriteString(label)
		line.WriteString("  ")
	}

	switch {
	case b.edit != nil:
		const apply, cancel = "[Enter Apply]", "[Esc Cancel]"
		const controlWidth = len(apply) + 2 + len(cancel)
		// Leave room to type on narrow screens. Keyboard shortcuts still work.
		start = width
		if width >= controlWidth+16 {
			start = width - controlWidth
		}
		b.filter.SetRect(0, y, start, 1)
		b.filter.Draw(screen)
		add(apply, tcell.KeyEnter, 0)
		add(cancel, tcell.KeyEsc, 0)
	case b.inspect != nil:
		add("[Esc Back]", tcell.KeyEsc, 0)
		add("[q Quit]", tcell.KeyRune, 'q')
		line.WriteString("↑↓ scroll")
	default:
		add("[/ Filter]", tcell.KeyRune, '/')
		query := b.filter.GetText()
		if query != "" {
			add("[Esc Clear]", tcell.KeyEsc, 0)
		}
		if _, ok := b.selected(); ok {
			add("[Enter Inspect]", tcell.KeyEnter, 0)
		}
		add("[q Quit]", tcell.KeyRune, 'q')
		fmt.Fprintf(&line, "%d/%d domains", len(b.rows.visible), len(b.rows.Domains))
		if query != "" {
			line.WriteString("   Filter: " + terminal.SingleLine(query))
		}
	}
	if start < width {
		b.footer.SetRect(start, y, width-start, 1)
		b.footer.SetText(line.String()).Draw(screen)
	}
}
