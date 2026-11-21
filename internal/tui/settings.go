package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/rivo/tview"
)

type intervalSettings struct {
	saved time.Duration
	err   error
}

func (p intervalSettings) effective() time.Duration {
	if p.saved != 0 {
		return p.saved
	}
	return DefaultInterval
}

func (w *workspace) settings(h *liveSession) {
	back := func() { w.activate(h) }
	form := connectionForm()
	field := tview.NewInputField().SetLabel("Refresh interval").SetText(h.view.inventory.Interval().String()).SetFieldWidth(20)
	form.AddFormItem(field)
	hint := func() string {
		text := fmt.Sprintf("%s · Default %s", h.profile.Name, w.preferences.effective())
		if w.interval > 0 {
			text += fmt.Sprintf(" · New sessions use --interval %s", w.interval)
		}
		return text
	}
	var dialog *connectionDialog
	apply := func(save bool) {
		interval, err := time.ParseDuration(strings.TrimSpace(field.GetText()))
		if err != nil || interval < MinInterval {
			dialog.message = "Enter a duration of at least 100ms, such as 500ms or 2s."
			return
		}
		if !save {
			h.view.inventory.SetInterval(interval)
			back()
			return
		}
		before := w.preferences.saved
		busy := connectionList()
		busy.AddItem("Saving refresh interval", "", 0, nil)
		saving := w.show("SETTINGS", h.profile.Name, busy, func() {})
		w.workers.Add(1)
		go func() {
			defer w.workers.Done()
			err := w.store.SaveInterval(before, interval)
			current := intervalSettings{saved: interval}
			if err != nil {
				current.saved, current.err = w.store.LoadInterval()
				if current.err != nil {
					current.saved = before
				}
			}
			select {
			case w.settingsDone <- func() {
				w.preferences = current
				if w.active != h || w.dialog != saving {
					return
				}
				if err != nil {
					w.dialog = dialog
					dialog.hint = hint()
					dialog.message = err.Error()
					return
				}
				h.view.inventory.SetInterval(interval)
				back()
			}:
			case <-w.ctx.Done():
			}
		}()
	}
	form.AddButton("Apply", func() { apply(false) }).AddButton("Apply + save default", func() { apply(true) }).AddButton("Cancel", back)
	dialog = w.show("SETTINGS", hint(), form, back)
	if w.preferences.err != nil {
		dialog.message = w.preferences.err.Error()
	}
}
