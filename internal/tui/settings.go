package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/advnpzn/xlui/internal/config"
	"github.com/rivo/tview"
)

type operatorSettings struct {
	saved config.Preferences
	err   error
}

func (p operatorSettings) effective() time.Duration {
	if p.saved.Interval != 0 {
		return p.saved.Interval
	}
	return DefaultInterval
}

func (w *workspace) settings(h *liveSession) {
	back := func() { w.activate(h) }
	form := connectionForm()
	field := tview.NewInputField().SetLabel("Refresh interval").SetText(h.view.inventory.Interval().String()).SetFieldWidth(20)
	form.AddFormItem(field)
	layoutField := tview.NewDropDown().SetLabel("Dashboard layout").SetOptions(layoutNames[:], nil).SetCurrentOption(int(w.layout))
	form.AddFormItem(layoutField)
	orderField := tview.NewDropDown().SetLabel("Domain order").SetOptions(orderNames[:], nil).SetCurrentOption(int(h.view.rows.order))
	form.AddFormItem(orderField)
	before := w.preferences.saved
	hint := func() string {
		text := fmt.Sprintf("%s · Defaults %s · %s · %s", h.profile.Name, w.preferences.effective(), layoutNames[w.preferences.saved.Layout], orderNames[w.preferences.saved.Order])
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
		option, _ := layoutField.GetCurrentOption()
		order, _ := orderField.GetCurrentOption()
		after := config.Preferences{Interval: interval, Layout: Layout(option), Order: config.DomainOrder(order)}
		applyToView := func() {
			w.layout = after.Layout
			h.view.setOrder(after.Order)
			h.view.inventory.SetInterval(after.Interval)
			back()
		}
		if !save {
			applyToView()
			return
		}
		busy := connectionList()
		busy.AddItem("Saving settings", "", 0, nil)
		saving := w.show("SETTINGS", h.profile.Name, busy, func() {})
		w.workers.Add(1)
		go func() {
			defer w.workers.Done()
			err := w.store.SavePreferences(before, after)
			current := operatorSettings{saved: after}
			if err != nil {
				current.saved, current.err = w.store.LoadPreferences()
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
				applyToView()
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
