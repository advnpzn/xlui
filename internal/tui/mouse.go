package tui

import (
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Bind a click to the displayed instance as well as its position. An inventory
// refresh can replace the domain under the pointer between press and release.
type mouseTarget struct {
	x, y   int
	row    int
	domain domainKey
	button shortcut
	field  bool
}

type pointerState struct {
	pressed, last mouseTarget
	lastAt        time.Time
	down          bool
}

func (b *browser) mouseTarget(x, y, height int) mouseTarget {
	target := mouseTarget{x: x, y: y}
	if y == 0 || y == height-1 {
		if b.edit != nil && b.filter.InRect(x, y) {
			target.field = true
			return target
		}
		buttons := b.buttons
		if y == 0 {
			buttons = b.refreshButtons
		}
		for _, button := range buttons {
			if x >= button.x && x < button.x+len(button.label) {
				target.button = button.shortcut
				if b.edit == nil && button.key == tcell.KeyEnter {
					target.domain, _ = b.selected()
				}
				return target
			}
		}
	} else if b.inspect == nil && b.table.InRect(x, y) {
		row, _ := b.table.CellAt(x, y)
		if row > 0 && row <= len(b.rows.visible) {
			target.row = row
			target.domain = keyOf(b.rows.Domains[b.rows.visible[row-1]])
			return target
		}
	}
	return mouseTarget{}
}

func (b *browser) mouse(ev *tcell.EventMouse, width, height int) bool {
	x, y := ev.Position()
	p := &b.pointer
	if x < 0 || x >= width || y < 0 || y >= height {
		*p = pointerState{}
		return false
	}

	switch ev.Buttons() {
	case tcell.WheelUp, tcell.WheelDown:
		*p = pointerState{}
		if y == 0 || y == height-1 {
			return false
		}
		direction := tview.MouseScrollUp
		if ev.Buttons() == tcell.WheelDown {
			direction = tview.MouseScrollDown
		}
		if b.inspect != nil {
			b.inspect.view.MouseHandler()(direction, ev, nil)
		} else if len(b.rows.visible) > 0 {
			b.table.MouseHandler()(direction, ev, nil)
		}
	case tcell.ButtonPrimary:
		target := b.mouseTarget(x, y, height)
		if !p.down {
			p.pressed = target
		} else if p.pressed != target {
			p.pressed = mouseTarget{}
		}
		p.down = true
	case tcell.ButtonNone:
		target := p.pressed
		p.down = false
		p.pressed = mouseTarget{}
		if target == (mouseTarget{}) || target != b.mouseTarget(x, y, height) {
			p.last = mouseTarget{}
			return false
		}
		double := target == p.last && ev.When().Sub(p.lastAt) <= tview.DoubleClickInterval
		p.last = mouseTarget{}
		switch {
		case target.field:
			// The editor already has focus. Let tview place its Unicode cursor.
			handler := b.filter.MouseHandler()
			handler(tview.MouseLeftDown, ev, func(tview.Primitive) {})
			handler(tview.MouseLeftUp, ev, nil)
		case target.row > 0:
			if b.edit != nil {
				b.key(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
			}
			b.table.SetSelectable(true, false).Select(target.row, 0)
			if double {
				b.key(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
			} else {
				p.last, p.lastAt = target, ev.When()
			}
		default:
			return b.key(tcell.NewEventKey(target.button.key, target.button.char, tcell.ModNone))
		}
	default:
		*p = pointerState{}
	}
	return false
}
