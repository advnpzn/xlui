package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/advnpzn/xlui/internal/session"
	"github.com/advnpzn/xlui/internal/terminal"
	"github.com/advnpzn/xlui/internal/xen"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type domainKey struct {
	uuid string
	id   int
}

func keyOf(d xen.Domain) domainKey { return domainKey{d.UUID, d.ID} }

type browser struct {
	host      string
	rows      domainRows
	table     *tview.Table
	filter    *tview.InputField
	header    *tview.TextView
	footer    *tview.TextView
	detail    *tview.TextView
	message   *tview.TextView
	inspect   *domainKey
	edit      *filterEdit
	latest    time.Time
	lastError error
}

// Escape restores the filter and selection from the start of this edit.
type filterEdit struct {
	query     string
	selection domainKey
}

func newBrowser(host string) *browser {
	b := &browser{
		host:    terminal.SingleLine(host),
		table:   tview.NewTable().SetFixed(1, 0),
		filter:  tview.NewInputField().SetLabel("Filter  "),
		header:  tview.NewTextView().SetWrap(false),
		footer:  tview.NewTextView().SetWrap(false),
		detail:  tview.NewTextView().ScrollToBeginning(),
		message: tview.NewTextView().SetWordWrap(true).ScrollToBeginning(),
	}
	b.table.SetContent(&b.rows)
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
	return keyOf(b.rows.all[b.rows.visible[row-1]]), true
}

func (b *browser) update(s session.Snapshot) {
	b.lastError = s.Err
	if s.ObservedAt.Equal(b.latest) {
		return
	}
	selected, _ := b.selected()
	initial := b.latest.IsZero()
	b.rows.all = s.Domains
	b.latest = s.ObservedAt
	b.project(selected, initial)
	if b.inspect != nil {
		b.updateDetail()
	}
}

func (b *browser) project(selected domainKey, initial bool) {
	query := strings.ToLower(b.filter.GetText())
	visible := b.rows.visible[:0]
	row := 0
	for i, d := range b.rows.all {
		if query != "" && !strings.Contains(strings.ToLower(d.Name), query) &&
			!strings.Contains(strings.ToLower(d.State), query) &&
			!strings.Contains(strings.ToLower(d.UUID), query) && !strings.Contains(strconv.Itoa(d.ID), query) {
			continue
		}
		visible = append(visible, i)
		if keyOf(d) == selected {
			row = len(visible)
		}
	}
	b.rows.visible = visible
	if initial && len(visible) > 0 {
		row = 1
	}
	b.table.SetSelectable(row > 0, false).Select(row, 0)
}

func (b *browser) updateDetail() {
	b.detail.SetText("This domain instance is no longer in the inventory.")
	for _, d := range b.rows.all {
		if keyOf(d) == *b.inspect {
			b.detail.SetText(fmt.Sprintf("%s\n\nUUID     %s\nID       %d\nState    %s\nMemory   %d MiB\nvCPUs    %d\nCPU time %.1f s",
				terminal.SingleLine(d.Name), terminal.SingleLine(d.UUID), d.ID, terminal.SingleLine(d.State), d.MemoryMiB, d.VCPUs, d.CPUSeconds))
			return
		}
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
	if ev.Key() == tcell.KeyRune && ev.Rune() == 'q' {
		return true
	}
	if b.inspect != nil {
		if ev.Key() == tcell.KeyEsc {
			b.inspect = nil
		} else {
			b.detail.InputHandler()(ev, nil)
		}
		return false
	}
	switch ev.Key() {
	case tcell.KeyEsc:
		b.filter.SetText("")
	case tcell.KeyEnter:
		if key, ok := b.selected(); ok {
			b.inspect = &key
			b.updateDetail()
			b.detail.ScrollToBeginning()
		}
	case tcell.KeyRune:
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
