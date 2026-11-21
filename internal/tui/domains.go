package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/advnpzn/xlui/internal/session"
	"github.com/advnpzn/xlui/internal/xen"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type domainKey struct {
	uuid string
	id   int
}

func keyOf(d xen.Domain) domainKey { return domainKey{d.UUID, d.ID} }

// literal removes control characters from host data before putting it on a single line.
func literal(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return '�'
		}
		return r
	}, text)
}

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
		return tview.NewTableCell(tview.Escape(literal(d.Name))).SetMaxWidth(28).SetExpansion(1)
	case 1:
		text = strconv.Itoa(d.ID)
	case 2:
		text = tview.Escape(literal(d.State))
	case 3:
		text = strconv.FormatInt(d.MemoryMiB, 10)
	case 4:
		text = strconv.Itoa(d.VCPUs)
	case 5:
		text = strconv.FormatFloat(d.CPUSeconds, 'f', 1, 64)
	}
	return tview.NewTableCell(text)
}

type browser struct {
	host      string
	rows      domainRows
	table     *tview.Table
	filter    *tview.InputField
	header    *tview.TextView
	footer    *tview.TextView
	detail    *tview.TextView
	inspect   *domainKey
	editing   bool
	latest    time.Time
	lastError error
}

func newBrowser(host string) *browser {
	b := &browser{
		host:   literal(host),
		table:  tview.NewTable().SetFixed(1, 0),
		filter: tview.NewInputField().SetLabel("Filter  "),
		header: tview.NewTextView().SetWrap(false),
		footer: tview.NewTextView().SetWrap(false),
		detail: tview.NewTextView(),
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
				literal(d.Name), literal(d.UUID), d.ID, literal(d.State), d.MemoryMiB, d.VCPUs, d.CPUSeconds))
			return
		}
	}
}

func (b *browser) key(ev *tcell.EventKey) bool {
	if ev.Key() == tcell.KeyCtrlC {
		return true
	}
	if b.editing {
		switch ev.Key() {
		case tcell.KeyEsc, tcell.KeyEnter:
			b.editing = false
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
		}
	case tcell.KeyRune:
		if ev.Rune() == '/' {
			b.editing = true
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

func (b *browser) draw(screen tcell.Screen) {
	w, h := screen.Size()
	if w < 1 || h < 1 {
		return
	}
	screen.HideCursor()
	status := "loading"
	if !b.latest.IsZero() {
		status = b.latest.Format("15:04:05")
	}
	if b.lastError != nil {
		if b.latest.IsZero() {
			status = "unavailable"
		} else {
			status = "stale " + status
		}
		status += " | " + literal(b.lastError.Error())
	}
	b.header.SetText(fmt.Sprintf("xlui | %s | %s", b.host, status))
	b.header.SetRect(0, 0, w, 1)
	b.header.Draw(screen)

	content := tview.Primitive(b.table)
	help := fmt.Sprintf("%d/%d domains   / filter   Enter inspect   q quit", len(b.rows.visible), len(b.rows.all))
	if b.inspect != nil {
		content = b.detail
		help = "Domain details   Esc back   q quit"
	}
	if h > 2 {
		content.SetRect(0, 1, w, h-2)
		content.Draw(screen)
	}
	if h > 1 {
		if b.editing {
			b.filter.SetRect(0, h-1, w, 1)
			b.filter.Draw(screen)
		} else {
			if text := b.filter.GetText(); text != "" && b.inspect == nil {
				help = "Filter " + literal(text) + " | " + help
			}
			b.footer.SetText(help)
			b.footer.SetRect(0, h-1, w, 1)
			b.footer.Draw(screen)
		}
	}
	screen.Show()
}
