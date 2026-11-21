package tui

import (
	"math"
	"strings"
	"time"

	"github.com/advnpzn/xlui/internal/session"
	"github.com/advnpzn/xlui/internal/terminal"
	"github.com/advnpzn/xlui/internal/xen"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const (
	DefaultInterval = 2 * time.Second
	MinInterval     = 100 * time.Millisecond
	intervalStep    = 100 * time.Millisecond
)

type domainKey = xen.Identity

func keyOf(d xen.Domain) domainKey { return d.Identity() }

type browser struct {
	stats                 dashboard
	inventory             *session.Inventory
	openCount             int
	sessions, nextSession func()
	guests                func()
	host                  string
	rows                  domainRows
	table                 *tview.Table
	filter                *tview.InputField
	header                *tview.TextView
	footer                *tview.TextView
	buttons               []shortcutButton
	refreshButtons        []shortcutButton
	pointer               pointerState
	message               *tview.TextView
	inspect               *inspector
	edit                  *filterEdit
	latest                time.Time
	lastError             error
}

// Escape restores the filter and selection from the start of this edit.
type filterEdit struct {
	query     string
	selection domainKey
}

func newBrowser(host string) *browser {
	b := &browser{
		host: terminal.SingleLine(host), openCount: 1,
		table:   tview.NewTable().SetFixed(1, 0),
		filter:  tview.NewInputField().SetLabel("Filter  "),
		header:  tview.NewTextView().SetWrap(false),
		footer:  tview.NewTextView().SetWrap(false),
		message: tview.NewTextView().SetWordWrap(true).ScrollToBeginning(),
	}
	b.table.SetContent(&b.rows).SetSelectedStyle(tcell.StyleDefault.Background(tcell.NewHexColor(0x1e5361)).Foreground(tcell.ColorWhite).Bold(true))
	b.table.SetBackgroundColor(surface)
	b.footer.SetBackgroundColor(surface)
	b.footer.SetTextColor(aqua)
	b.filter.SetChangedFunc(func(string) {
		key, _ := b.selected()
		b.project(key, false)
	})
	return b
}

func (b *browser) selected() (domainKey, bool) {
	row, _ := b.table.GetSelection()
	if row < 1 || row > len(b.rows.visible) {
		return domainKey{}, false
	}
	return keyOf(b.rows.Domains[b.rows.visible[row-1]]), true
}

func (b *browser) update(s session.Snapshot) {
	if last := b.rows.Sequence; last > 0 && s.Sequence > last && s.Sequence-last > 1 {
		b.stats.skip(s.Sequence - last - 1)
	}
	if s.Err != nil {
		b.stats.skip(1)
	}
	lostCPU := b.rows.order == orderCPU && b.lastError == nil && s.Err != nil
	b.lastError = s.Err
	selected, _ := b.selected()
	b.rows.Snapshot = s
	if b.inspect != nil {
		b.inspect.observe(s)
	}
	if s.ObservedAt.Equal(b.latest) {
		if lostCPU {
			b.project(selected, false)
		}
		return
	}
	initial := b.latest.IsZero()
	b.stats.sample(s)
	b.latest = s.ObservedAt
	b.project(selected, initial)
}

func (b *browser) project(selected domainKey, initial bool) {
	query := strings.ToLower(b.filter.GetText())
	visible := b.rows.visible[:0]
	for i, d := range b.rows.Domains {
		if query != "" && !strings.Contains(strings.ToLower(d.Name), query) &&
			!strings.Contains(strings.ToLower(d.State), query) && !strings.Contains(strings.ToLower(stateLabel(d.State)), query) &&
			!strings.Contains(strings.ToLower(d.UUID), query) && !strings.Contains(number(d.ID), query) {
			continue
		}
		visible = append(visible, i)
	}
	b.rows.visible = visible
	b.rows.sortVisible()
	row := 0
	if initial && len(visible) > 0 {
		row = 1
	} else {
		for j, i := range visible {
			if keyOf(b.rows.Domains[i]) == selected {
				row = j + 1
				break
			}
		}
	}
	b.table.SetSelectable(row > 0, false)
	if current, _ := b.table.GetSelection(); current != row {
		b.table.Select(row, 0)
	}
}

func (b *browser) key(ev *tcell.EventKey) bool {
	if ev.Key() == tcell.KeyCtrlC {
		return true
	}
	if b.edit != nil {
		switch ev.Key() {
		case tcell.KeyEsc:
			b.filter.SetText(b.edit.query)
			b.project(b.edit.selection, false)
			b.edit = nil
			b.filter.Blur()
		case tcell.KeyEnter:
			b.edit = nil
			b.filter.Blur()
		default:
			b.filter.InputHandler()(ev, nil)
		}
		return false
	}
	if ev.Key() == tcell.KeyTab && b.nextSession != nil {
		b.nextSession()
		return false
	}
	if ev.Key() == tcell.KeyRune && b.inventory != nil {
		switch ev.Rune() {
		case '-':
			b.inventory.SetInterval(max(MinInterval, b.inventory.Interval()-intervalStep))
			return false
		case '+':
			b.inventory.SetInterval(min(time.Duration(math.MaxInt64)-intervalStep, b.inventory.Interval()) + intervalStep)
			return false
		}
	}
	if ev.Key() == tcell.KeyRune && ev.Rune() == 'g' && b.guests != nil {
		b.guests()
		return false
	}
	if ev.Key() == tcell.KeyRune && ev.Rune() == 's' && b.sessions != nil {
		b.sessions()
		return false
	}
	if ev.Key() == tcell.KeyRune && ev.Rune() == 'q' {
		return true
	}
	if b.inspect != nil {
		if ev.Key() == tcell.KeyEsc {
			b.inspect = nil
		} else {
			b.inspect.view.InputHandler()(ev, nil)
		}
		return false
	}
	switch ev.Key() {
	case tcell.KeyEsc:
		b.filter.SetText("")
	case tcell.KeyEnter:
		if key, ok := b.selected(); ok {
			b.inspect = newInspector(key, b.rows.Snapshot)
		}
	case tcell.KeyRune:
		if ev.Rune() == 'o' {
			selected, _ := b.selected()
			b.rows.order = (b.rows.order + 1) % domainOrder(len(orderNames))
			b.project(selected, false)
			return false
		}
		if ev.Rune() == '/' {
			selected, _ := b.selected()
			b.edit = &filterEdit{query: b.filter.GetText(), selection: selected}
			b.filter.Focus(nil)
			return false
		}
		fallthrough
	default:
		if _, ok := b.selected(); !ok && len(b.rows.visible) > 0 {
			if ev.Key() == tcell.KeyDown || ev.Key() == tcell.KeyUp || ev.Rune() == 'j' || ev.Rune() == 'k' {
				b.table.SetSelectable(true, false).Select(1, 0)
			}
			return false
		}
		if len(b.rows.visible) > 0 {
			b.table.InputHandler()(ev, nil)
		}
	}
	return false
}
