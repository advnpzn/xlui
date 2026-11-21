package tui

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/advnpzn/xlui/internal/session"
	"github.com/advnpzn/xlui/internal/xen"
	"github.com/gdamore/tcell/v2"
)

func assertVisibleIDs(t *testing.T, b *browser, want ...int) {
	t.Helper()
	var got []int
	for _, i := range b.rows.visible {
		got = append(got, b.rows.Domains[i].ID)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("visible domain IDs %v, want %v", got, want)
	}
}

func TestOrderCyclesOverCachedSnapshot(t *testing.T) {
	hot, idle := 130.0, 0.0
	used, empty := int64(1200), int64(0)
	domains := []xen.Domain{
		{ID: 4, UUID: "z", Name: "zulu", CPUPercent: &hot, MemoryMiB: 32, GuestUsedKiB: &used},
		{ID: 8, UUID: "c", Name: "beta", CPUSeconds: -1, MemoryMiB: -1},
		{ID: 3, UUID: "b", Name: "alpha", CPUPercent: &idle, MemoryMiB: 64, GuestUsedKiB: &empty},
		{ID: 2, UUID: "a", Name: "alpha", CPUPercent: &idle, MemoryMiB: 0},
	}
	original := slices.Clone(domains)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	stream, err := session.WatchInventory(ctx, time.Hour, func(context.Context) ([]xen.Domain, error) {
		return domains, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	var snap session.Snapshot
	select {
	case snap = <-stream.Updates():
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	b, other := newBrowser("bench"), newBrowser("other")
	b.update(snap)
	other.update(snap)
	b.table.Select(3, 0) // Select guest b, which shares a name and CPU reading with guest a.
	for _, want := range [][]int{
		{2, 3, 8, 4}, // name; UUID breaks the tie between the two alpha guests
		{4, 2, 3, 8}, // CPU; measured idle precedes unavailable
		{3, 4, 2, 8}, // allocated; measured zero precedes unavailable
		{4, 3, 2, 8}, // guest used; measured zero precedes unavailable
		{4, 8, 3, 2}, // provider order restored
	} {
		b.key(tcell.NewEventKey(tcell.KeyRune, 'o', 0))
		assertVisibleIDs(t, b, want...)
		if selected, ok := b.selected(); !ok || selected != keyOf(original[2]) {
			t.Fatalf("ordering changed selection to %v", selected)
		}
		assertVisibleIDs(t, other, 4, 8, 3, 2)
		if !slices.Equal(domains, original) {
			t.Fatal("ordering mutated the provider's snapshot")
		}
	}
}

func TestOrderWithFilterRefreshAndMouse(t *testing.T) {
	v := newMouseView(t)
	v.screen.SetSize(130, 24)
	names := []xen.Domain{
		{ID: 7, UUID: "b", Name: "test-z"},
		{ID: 5, UUID: "a", Name: "test-a"},
		{ID: 9, UUID: "c", Name: "other"},
	}
	v.update(session.Snapshot{Domains: names, ObservedAt: v.latest.Add(time.Second)})
	v.table.Select(1, 0)
	v.draw(v.screen)
	_, height := v.screen.Size()
	clicked := false
	for _, btn := range v.buttons {
		if btn.char == 'o' {
			v.click(btn.x+1, height-1)
			clicked = true
			break
		}
	}
	if !clicked {
		t.Fatal("order shortcut missing from wide footer")
	}
	assertVisibleIDs(t, v.browser, 9, 5, 7)
	v.key(tcell.NewEventKey(tcell.KeyRune, '/', 0))
	v.key(tcell.NewEventKey(tcell.KeyRune, 'o', 0))
	if v.filter.GetText() != "o" {
		t.Fatal("order shortcut intercepted filter input")
	}
	v.key(tcell.NewEventKey(tcell.KeyEsc, 0, 0))
	v.filter.SetText("test-")
	assertVisibleIDs(t, v.browser, 5, 7)
	v.update(session.Snapshot{
		Domains:    []xen.Domain{names[1], {ID: 10, UUID: "d", Name: "test-0"}, names[0]},
		ObservedAt: v.latest.Add(time.Second),
	})
	assertVisibleIDs(t, v.browser, 10, 5, 7)
	v.key(tcell.NewEventKey(tcell.KeyEnter, 0, 0))
	if v.inspect == nil || v.inspect.key != keyOf(names[0]) {
		t.Fatal("inspection after refresh lost the selected instance")
	}
}
