package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/advnpzn/xlui/internal/session"
	"github.com/advnpzn/xlui/internal/xen"
	"github.com/gdamore/tcell/v2"
)

type mouseView struct {
	*browser
	screen tcell.SimulationScreen
}

func newMouseView(t *testing.T) mouseView {
	t.Helper()
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(screen.Fini)
	screen.SetSize(80, 7)
	v := mouseView{newBrowser("test"), screen}
	domains := make([]xen.Domain, 12)
	for i := range domains {
		domains[i] = xen.Domain{ID: i, UUID: fmt.Sprint(i), Name: fmt.Sprintf("guest-%02d", i)}
	}
	v.update(session.Snapshot{Domains: domains, ObservedAt: time.Unix(1, 0)})
	v.draw(screen)
	return v
}

func (v mouseView) send(x, y int, buttons tcell.ButtonMask) bool {
	w, h := v.screen.Size()
	quit := v.mouse(tcell.NewEventMouse(x, y, buttons, tcell.ModNone), w, h)
	v.draw(v.screen)
	return quit
}

func (v mouseView) click(x, y int) bool {
	v.send(x, y, tcell.ButtonPrimary)
	return v.send(x, y, tcell.ButtonNone)
}

func (v mouseView) button(t *testing.T, label string) bool {
	t.Helper()
	for _, button := range v.buttons {
		if button.label == label {
			return v.click(button.x+1, button.y)
		}
	}
	t.Fatalf("button %q missing", label)
	return false
}

func TestTwoRowFooterControls(t *testing.T) {
	v := newMouseView(t)
	v.screen.SetSize(46, 12)
	opened := false
	v.settings = func() { opened = true }
	v.draw(v.screen)
	v.button(t, "[, Settings]")
	if !opened {
		t.Fatal("Settings did not open from upper footer row")
	}
	v.button(t, "[Enter Inspect]")
	if v.inspect == nil {
		t.Fatal("Inspect did not open from lower footer row")
	}
	v.button(t, "[Esc Back]")
	if v.inspect != nil {
		t.Fatal("Back did not leave inspector")
	}
}

func TestSwitchLayoutsKeepsSelectionAndSharesTheInventory(t *testing.T) {
	v := newMouseView(t)
	v.screen.SetSize(80, 24)
	v.table.Select(4, 0)
	want, _ := v.selected()
	v.draw(v.screen)
	v.button(t, "[L Overview]")
	if _, _, width, _ := v.table.GetRect(); v.layout != Inventory || width != 80 {
		t.Fatalf("inventory layout %v has table width %d", v.layout, width)
	}
	v.key(tcell.NewEventKey(tcell.KeyRune, 'L', 0))
	v.draw(v.screen)
	x, y, width, _ := v.table.GetRect()
	if v.layout != Split || width >= 80 || x+width >= 80 {
		t.Fatalf("split layout %v has table at %d with width %d", v.layout, x, width)
	}
	if key, ok := v.selected(); !ok || key != want {
		t.Fatalf("layout change selected %v, want %v", key, want)
	}
	v.click(79, y+2)
	if key, ok := v.selected(); !ok || key != want {
		t.Fatalf("sidebar click selected %v, want %v", key, want)
	}
	v.click(x+1, y+2)
	if key, ok := v.selected(); !ok || key != keyOf(v.rows.Domains[1]) {
		t.Fatalf("split table click selected %v, want second domain", key)
	}
	v.screen.SetSize(46, 12)
	v.draw(v.screen)
	if _, _, width, _ := v.table.GetRect(); v.layout != Split || width != 46 {
		t.Fatalf("split should fall back on a narrow screen, got layout %v and width %d", v.layout, width)
	}
}

func TestMouseScrolledRowAndInspector(t *testing.T) {
	v := newMouseView(t)
	v.send(2, 3, tcell.WheelDown)
	offset, _ := v.table.GetOffset()
	if offset != 1 {
		t.Fatalf("wheel scroll offset %d, want 1", offset)
	}
	v.update(session.Snapshot{Domains: v.rows.Domains, ObservedAt: v.latest.Add(time.Second)})
	v.draw(v.screen)
	if got, _ := v.table.GetOffset(); got != offset {
		t.Fatalf("refresh reset scroll offset to %d", got)
	}
	v.click(2, 1) // Fixed column headings.
	v.click(2, 2) // First displayed domain after scrolling.
	want := keyOf(v.rows.Domains[1])
	if selected, ok := v.selected(); !ok || selected != want || v.inspect != nil {
		t.Fatalf("single click: selection %v, inspector %v", selected, v.inspect)
	}
	v.click(2, 2)
	if v.inspect == nil || v.inspect.key != want {
		t.Fatalf("double click opened %v, want %v", v.inspect, want)
	}
	v.send(2, 2, tcell.WheelDown)
	if offset, _ := v.inspect.view.GetScrollOffset(); offset == 0 {
		t.Fatal("inspector did not scroll")
	}
	v.button(t, "[Esc Back]")
	v.button(t, "[Enter Inspect]")
	if v.inspect == nil || v.inspect.key != want {
		t.Fatal("Inspect button lost selection")
	}
	v.button(t, "[Esc Back]")
	if !v.button(t, "[q Quit]") {
		t.Fatal("Quit button did not exit")
	}
}

func TestMouseRefreshAndDrag(t *testing.T) {
	v := newMouseView(t)
	v.send(4, 3, tcell.ButtonPrimary)
	changed := append([]xen.Domain(nil), v.rows.Domains...)
	changed[1].UUID = "replacement"
	v.update(session.Snapshot{Domains: changed, ObservedAt: v.latest.Add(time.Second)})
	v.draw(v.screen)
	v.send(4, 3, tcell.ButtonNone)
	v.send(4, 3, tcell.ButtonPrimary)
	v.send(4, 4, tcell.ButtonPrimary)
	v.send(4, 4, tcell.ButtonNone)
	if sel, ok := v.selected(); !ok || sel != keyOf(changed[0]) {
		t.Fatalf("interrupted click selected %v", sel)
	}
	v.click(4, 3)
	changed = append([]xen.Domain(nil), changed...)
	changed[1].UUID = "another"
	v.update(session.Snapshot{Domains: changed, ObservedAt: v.latest.Add(time.Second)})
	v.draw(v.screen)
	v.click(4, 3)
	if sel, ok := v.selected(); !ok || sel != keyOf(changed[1]) || v.inspect != nil {
		t.Fatal("two clicks on different instances counted as a double-click")
	}
}

func TestMouseFilterAndResize(t *testing.T) {
	v := newMouseView(t)
	v.button(t, "[/ Filter]")
	v.filter.SetText("guest-09")
	v.draw(v.screen)
	v.click(12, 6) // Position the cursor inside the active field.
	v.button(t, "[Enter Apply]")
	if v.edit != nil || len(v.rows.visible) != 1 {
		t.Fatal("filter did not apply")
	}
	v.click(3, 2)
	v.click(3, 4) // Blank space beneath the only match.
	if sel, ok := v.selected(); !ok || sel != keyOf(v.rows.Domains[9]) || v.inspect != nil {
		t.Fatal("blank row changed selection or filtered row resolved incorrectly")
	}
	v.button(t, "[/ Filter]")
	v.filter.SetText("none")
	v.draw(v.screen)
	v.button(t, "[Esc Cancel]")
	if v.edit != nil || v.filter.GetText() != "guest-09" {
		t.Fatal("Cancel lost the applied filter")
	}
	v.button(t, "[Esc Clear]")
	if len(v.rows.visible) != len(v.rows.Domains) {
		t.Fatal("Clear did not restore inventory")
	}
	v.screen.SetSize(8, 1)
	v.draw(v.screen)
	if v.click(1, 0) || v.edit != nil || v.inspect != nil {
		t.Fatal("hidden footer responded to a click")
	}
}

func TestHeaderIntervalControlsAndFilterInput(t *testing.T) {
	v := newMouseView(t)
	inv, err := session.WatchInventory(t.Context(), DefaultInterval, func(ctx context.Context) ([]xen.Domain, error) { <-ctx.Done(); return nil, ctx.Err() })
	if err != nil {
		t.Fatal(err)
	}
	defer inv.Close()
	v.inventory = inv
	check := func(want string) {
		t.Helper()
		v.draw(v.screen)
		var line strings.Builder
		width, _ := v.screen.Size()
		for x := range width {
			r, _, _, _ := v.screen.GetContent(x, 0)
			line.WriteRune(r)
		}
		if !strings.Contains(line.String(), want) {
			t.Fatalf("missing %q in header %q", want, line.String())
		}
	}
	key := func(r rune) { v.key(tcell.NewEventKey(tcell.KeyRune, r, 0)) }
	check("[-] 2000 ms [+]")
	key('-')
	check("[-] 1900 ms [+]")
	v.key(tcell.NewEventKey(tcell.KeyEnter, 0, 0))
	v.click(v.refreshButtons[1].x+1, 0)
	check("[-] 2000 ms [+]")
	v.key(tcell.NewEventKey(tcell.KeyEsc, 0, 0))
	key('/')
	key('+')
	key('-')
	if v.filter.GetText() != "+-" || inv.Interval() != DefaultInterval {
		t.Fatal("interval controls intercepted filter text")
	}
	v.key(tcell.NewEventKey(tcell.KeyEsc, 0, 0))
	inv.SetInterval(125 * time.Millisecond)
	key('-')
	key('-')
	check("[-] 100 ms [+]")
}
