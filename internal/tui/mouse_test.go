package tui

import (
	"fmt"
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
	_, height := v.screen.Size()
	for _, button := range v.buttons {
		if button.label == label {
			return v.click(button.x+1, height-1)
		}
	}
	t.Fatalf("button %q missing", label)
	return false
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
