package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/advnpzn/xlui/internal/config"
	"github.com/advnpzn/xlui/internal/session"
	"github.com/advnpzn/xlui/internal/xen"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestSessionSettings(t *testing.T) {
	store := config.Store{Path: filepath.Join(t.TempDir(), "config.json")}
	initial := config.Profile{Name: "local", Type: config.Local}
	second := config.Profile{Name: "bench", Type: config.SSH, Address: "bench"}
	open := func(config.Profile, string) (session.Source, error) {
		return session.Source{Read: func(context.Context) ([]xen.Domain, error) {
			return []xen.Domain{{ID: 1, UUID: "guest", Name: "guest"}}, nil
		}}, nil
	}
	w := newWorkspace(t.Context(), initial, 0, open, store)
	defer w.close()
	w.connect(initial, "")
	sample := <-w.updates
	w.active.view.update(sample.snapshot)
	first := w.active
	if first.view.inventory.Interval() != DefaultInterval {
		t.Fatal("missing preference did not use the built-in default")
	}
	key := func(k tcell.Key, r rune) {
		e := tcell.NewEventKey(k, r, 0)
		if w.dialog != nil {
			w.dialog.key(e)
		} else {
			w.active.view.key(e)
		}
	}
	edit := func(value string, save bool) {
		t.Helper()
		key(tcell.KeyRune, ',')
		if w.dialog == nil || w.dialog.title != "SETTINGS" {
			t.Fatal("settings did not open")
		}
		field := w.dialog.body.(*tview.Form).GetFormItem(0).(*tview.InputField)
		field.SetText(value)
		key(tcell.KeyTab, 0)
		// Select Split by keyboard. The menu starts on the active layout.
		key(tcell.KeyEnter, 0)
		key(tcell.KeyEnd, 0)
		key(tcell.KeyEnter, 0)
		key(tcell.KeyTab, 0)
		if save {
			key(tcell.KeyTab, 0)
		}
		key(tcell.KeyEnter, 0)
	}
	awaitSave := func() {
		t.Helper()
		select {
		case show := <-w.settingsDone:
			show()
		case <-time.After(time.Second):
			t.Fatal("save did not finish")
		}
	}
	edit("1ms", false)
	if w.dialog == nil || !strings.Contains(w.dialog.message, "100ms") || first.view.inventory.Interval() != DefaultInterval {
		t.Fatal("invalid interval was accepted")
	}
	key(tcell.KeyEsc, 0)
	edit("750ms", false)
	if w.dialog != nil || first.view.inventory.Interval() != 750*time.Millisecond || first.view.layout != Split {
		t.Fatal("Apply did not change the active session")
	}
	if got, err := store.LoadPreferences(); got != (config.Preferences{}) || err != nil {
		t.Fatalf("Apply persisted %+v: %v", got, err)
	}

	key(tcell.KeyEnter, 0)
	inspector := first.view.inspect
	if inspector == nil {
		t.Fatal("inspector did not open")
	}
	edit("5s", true)
	awaitSave()
	if w.active != first || first.view.inspect != inspector || first.view.inventory.Interval() != 5*time.Second {
		t.Fatal("saving the interval replaced the session or inspector")
	}
	w.connect(second, "")
	if w.active.view.inventory.Interval() != 5*time.Second || w.active.view.layout != Split {
		t.Fatal("new connection did not inherit saved interval")
	}
	if err := store.SavePreferences(config.Preferences{Interval: 5 * time.Second, Layout: config.Split}, config.Preferences{Interval: time.Minute, Layout: config.Inventory}); err != nil {
		t.Fatal(err)
	}
	edit("3s", true)
	awaitSave()
	if w.dialog == nil || w.dialog.message == "" || !strings.Contains(w.dialog.hint, "1m0s") || w.active.view.inventory.Interval() != 5*time.Second {
		t.Fatal("stale save changed the sampler or concealed the new default")
	}

	// A second save from the same stale form must still require a reopen.
	key(tcell.KeyEnter, 0)
	awaitSave()
	want := config.Preferences{Interval: time.Minute, Layout: Inventory}
	if got, err := store.LoadPreferences(); err != nil || got != want || w.dialog == nil {
		t.Fatalf("retry from stale form: stored %+v, error %v, want %+v and an open form", got, err, want)
	}

	for _, tc := range []struct{ override, want time.Duration }{{0, time.Minute}, {DefaultInterval, DefaultInterval}} {
		fresh := newWorkspace(t.Context(), initial, tc.override, open, store)
		fresh.connect(initial, "")
		got := fresh.active.view.inventory.Interval()
		fresh.close()
		if got != tc.want {
			t.Fatalf("on restart with override %s: got %s, want %s", tc.override, got, tc.want)
		}
	}

	// Losing access to the preferences directory makes both save and reload fail.
	// Keep using the last observed default for sessions opened in this process.
	blocked := filepath.Join(t.TempDir(), "unavailable")
	if err := os.WriteFile(blocked, nil, 0600); err != nil {
		t.Fatal(err)
	}
	w.store.Path = filepath.Join(blocked, "config.json")
	key(tcell.KeyEsc, 0)
	edit("3s", true)
	awaitSave()
	if w.dialog == nil || w.dialog.message == "" || w.active.view.inventory.Interval() != 5*time.Second {
		t.Fatal("unreadable preferences did not report a failed save")
	}
	key(tcell.KeyEsc, 0)
	w.connect(config.Profile{Name: "another", Type: config.Local}, "")
	if got := w.active.view.inventory.Interval(); got != time.Minute {
		t.Fatalf("failed save and reload discarded the last observed default: %s", got)
	}
}

func TestStartupLayoutPreferenceAndOverride(t *testing.T) {
	store := config.Store{Path: filepath.Join(t.TempDir(), "config.json")}
	if err := store.SavePreferences(config.Preferences{}, config.Preferences{Layout: Split}); err != nil {
		t.Fatal(err)
	}
	profile := config.Profile{Name: "local", Type: config.Local}
	open := func(config.Profile, string) (session.Source, error) {
		return session.Source{Read: func(context.Context) ([]xen.Domain, error) { return nil, nil }}, nil
	}
	for _, tc := range []struct {
		layout Layout
		button string
	}{{SavedLayout, "[L Split]"}, {Overview, "[L Overview]"}} {
		t.Run(tc.button, func(t *testing.T) {
			screen := tcell.NewSimulationScreen("UTF-8")
			if err := screen.Init(); err != nil {
				t.Fatal(err)
			}
			defer screen.Fini()
			screen.SetSize(120, 36)
			// The event loop draws the first dashboard before reading the quit key.
			if err := screen.PostEvent(tcell.NewEventKey(tcell.KeyRune, 'q', 0)); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			if err := runScreen(ctx, screen, profile, time.Minute, tc.layout, "", open, store); err != nil {
				t.Fatal(err)
			}
			cells, _, _ := screen.GetContents()
			var display strings.Builder
			for _, cell := range cells {
				display.WriteString(string(cell.Runes))
			}
			if !strings.Contains(display.String(), tc.button) {
				t.Fatalf("dashboard did not show expected layout %q", tc.button)
			}
		})
	}
}
