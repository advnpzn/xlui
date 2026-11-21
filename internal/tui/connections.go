package tui

import (
	"fmt"
	"slices"

	"github.com/advnpzn/xlui/internal/config"
	"github.com/advnpzn/xlui/internal/terminal"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type connectionDialog struct {
	title, hint, message string
	body, focus          tview.Primitive
	detail               *tview.TextView
	back                 func()
	cancel               func()
	inspector            *inspector // only for reads tied to an observed domain
	down                 bool
	pressX, pressY       int
}

func (w *workspace) show(title, hint string, body tview.Primitive, back func()) *connectionDialog {
	w.dismissDialog()
	d := &connectionDialog{title: title, hint: hint, body: body, back: back}
	w.dialog = d
	d.setFocus(body)
	return d
}

func (w *workspace) dismissDialog() {
	if d := w.dialog; d != nil && d.cancel != nil {
		d.cancel()
	}
	w.dialog = nil
}

func (d *connectionDialog) setFocus(p tview.Primitive) {
	if d.focus != nil {
		d.focus.Blur()
	}
	d.focus = p
	p.Focus(d.setFocus)
}

func (d *connectionDialog) draw(s tcell.Screen) {
	w, h := s.Size()
	s.Clear()
	s.HideCursor()
	panel(s, 0, 0, w, h, d.title, aqua)
	paint(s, 2, 1, w-4, muted, d.hint)
	const bodyTop = 3
	bottom := h - 1
	width := max(0, w-4)
	if d.message != "" && width > 0 && h > 6 {
		message := terminal.SingleLine(d.message)
		maxRows := max(1, min(3, (h-6)/3))
		rows := min(len(tview.WordWrap(tview.Escape(message), width)), maxRows)
		bottom -= rows
		status := tview.NewTextView().SetWordWrap(true).SetScrollable(false).SetTextColor(tcell.ColorKhaki).SetText(message)
		status.SetBackgroundColor(surface)
		status.SetRect(2, bottom, width, rows)
		status.Draw(s)
	} else if d.detail != nil && h >= 9 {
		bottom--
		hint := "↑↓ Scroll · Tab Choose · Esc Back"
		if width >= 46 {
			hint = "↑↓ / wheel Scroll · Tab Choose · Esc Back"
		}
		paint(s, 2, bottom, width, muted, hint)
	}
	bodyHeight := max(0, bottom-bodyTop)
	d.body.SetRect(2, bodyTop, width, bodyHeight)
	if d.detail != nil {
		d.detail.SetSize(max(1, bodyHeight-2), 0)
	} else if form, ok := d.body.(*tview.Form); ok {
		padding := 1
		if bodyHeight < 12 {
			padding = 0
		}
		form.SetItemPadding(padding)
	}
	if bodyHeight > 0 && width > 0 {
		d.body.Draw(s)
	}
	s.Show()
}

func (d *connectionDialog) key(e *tcell.EventKey) {
	if e.Key() == tcell.KeyEsc {
		d.back()
		return
	}
	d.down = false
	if d.detail != nil {
		switch e.Key() {
		case tcell.KeyUp, tcell.KeyDown, tcell.KeyPgUp, tcell.KeyPgDn, tcell.KeyHome, tcell.KeyEnd:
			d.detail.InputHandler()(e, d.setFocus)
			return
		}
	}
	if h := d.focus.InputHandler(); h != nil {
		h(e, d.setFocus)
	}
}
func (d *connectionDialog) mouse(e *tcell.EventMouse) {
	h := d.body.MouseHandler()
	x, y := e.Position()
	switch e.Buttons() {
	case tcell.ButtonPrimary:
		if !d.down {
			d.pressX, d.pressY = x, y
			h(tview.MouseLeftDown, e, d.setFocus)
		}
		d.down = true
	case tcell.ButtonNone:
		if d.down {
			h(tview.MouseLeftUp, e, d.setFocus)
			if x == d.pressX && y == d.pressY {
				h(tview.MouseLeftClick, e, d.setFocus)
			}
		}
		d.down = false
	case tcell.WheelUp:
		h(tview.MouseScrollUp, e, d.setFocus)
	case tcell.WheelDown:
		h(tview.MouseScrollDown, e, d.setFocus)
	}
}

func connectionList() *tview.List {
	l := tview.NewList().SetSelectedBackgroundColor(edge).SetSelectedTextColor(tcell.ColorWhite).SetSecondaryTextColor(muted).SetShortcutColor(aqua)
	l.SetBackgroundColor(surface)
	return l
}

func (w *workspace) manager(message string) {
	profiles, err := w.store.Load()
	if err != nil {
		message = err.Error()
	}
	list := connectionList()
	back := func() {
		if w.active != nil {
			w.activate(w.active)
		} else {
			w.quit = true
		}
	}
	list.AddItem("Add connection", "Save a local, SSH or XenAPI connection", 'a', w.chooseType)
	entries := slices.Clone(profiles)
	if !slices.Contains(entries, w.initial) {
		entries = append([]config.Profile{w.initial}, entries...)
	}
	// An external editor may have removed a profile while its session is open.
	for _, p := range w.order {
		if !slices.Contains(entries, p) {
			entries = append(entries, p)
		}
	}
	current := 0
	for _, p := range entries {
		state := "Available"
		if h := w.live[p]; h != nil {
			state = "Open"
			if h.view.lastError != nil {
				state = "Unavailable"
				if !h.view.latest.IsZero() {
					state = "Stale"
				}
			}
			if h == w.active {
				state += " · current"
			}
		}
		if w.active != nil && p == w.active.profile {
			current = list.GetItemCount()
		}
		label := fmt.Sprintf("%s  ·  %s", state, connectionAddress(p))
		list.AddItem(tview.Escape(p.Name), tview.Escape(terminal.SingleLine(label)), 0, func() { w.actions(p, slices.Contains(profiles, p)) })
	}
	list.AddItem("Back", "Return to dashboard or exit if all sessions are closed", 'b', back)
	list.SetCurrentItem(current)
	d := w.show("SESSIONS", "Enter or click a connection   ·   a Add   ·   Esc Back", list, back)
	d.message = message
}

func connectionAddress(p config.Profile) string {
	switch p.Type {
	case config.SSH:
		return "SSH  " + p.Address
	case config.XAPI:
		return "XenAPI  " + p.User + " @ " + p.Address
	default:
		return "Local Dom0"
	}
}

func (w *workspace) actions(p config.Profile, saved bool) {
	list := connectionList()
	back := func() { w.manager("") }
	list.AddItem("Open dashboard", tview.Escape(terminal.SingleLine(connectionAddress(p))), 'o', func() { w.connect(p, "") })
	if saved {
		list.AddItem("Edit connection", "Change the saved connection settings", 'e', func() { w.editProfile(p, &p) })
	}
	if w.live[p] != nil {
		list.AddItem("Reconnect", "Start a fresh session with this connection", 'r', func() { w.disconnect(p); w.connect(p, "") })
		list.AddItem("Disconnect", "Stop sampling this session", 'd', func() { w.disconnect(p); w.manager("") })
	}
	list.AddItem("Back", "Return to all sessions", 'b', back)
	w.show(p.Name, "Select an action   ·   Esc Back", list, back)
}

func (w *workspace) chooseType() {
	list := connectionList()
	back := func() { w.manager("") }
	list.AddItem("Local Dom0", "Use xl on this machine", 'l', func() { w.editProfile(config.Profile{Type: config.Local}, nil) })
	list.AddItem("SSH", "Use xl on a remote Dom0 through OpenSSH", 's', func() { w.editProfile(config.Profile{Type: config.SSH}, nil) })
	list.AddItem("XenAPI", "Connect to a pool coordinator over HTTPS", 'x', func() { w.editProfile(config.Profile{Type: config.XAPI}, nil) })
	list.AddItem("Back", "Return to all sessions", 'b', back)
	w.show("ADD CONNECTION", "Select a connection type   ·   Esc Back", list, back)
}

func connectionForm() *tview.Form {
	f := tview.NewForm().SetLabelColor(aqua).SetFieldBackgroundColor(edge).SetFieldTextColor(ink).SetButtonBackgroundColor(edge).SetButtonTextColor(ink)
	f.SetButtonActivatedStyle(tcell.StyleDefault.Background(aqua).Foreground(surface).Bold(true))
	f.SetBorderPadding(0, 0, 1, 1)
	f.SetBackgroundColor(surface)
	return f
}

func (w *workspace) editProfile(p config.Profile, before *config.Profile) {
	f := connectionForm()
	f.AddInputField("Name", p.Name, 50, nil, func(v string) { p.Name = v })
	switch p.Type {
	case config.SSH:
		f.AddInputField("SSH destination", p.Address, 50, nil, func(v string) { p.Address = v })
	case config.XAPI:
		f.AddInputField("HTTPS endpoint", p.Address, 58, nil, func(v string) { p.Address = v })
		f.AddInputField("Username", p.User, 40, nil, func(v string) { p.User = v })
		f.AddInputField("CA file (optional)", p.CAFile, 58, nil, func(v string) { p.CAFile = v })
	}
	back := func() { w.manager("") }
	save := func(connect bool) {
		if _, err := w.store.Save(before, p); err != nil {
			w.dialog.message = err.Error()
			return
		}
		if before != nil && *before != p {
			w.disconnect(*before)
		}
		if connect {
			w.connect(p, "")
		} else {
			w.manager("Saved " + p.Name)
		}
	}
	f.AddButton("Save + open", func() { save(true) }).AddButton("Save", func() { save(false) }).AddButton("Cancel", back)
	w.show("CONNECTION SETTINGS", "Tab / Shift+Tab to move   ·   Enter to select   ·   Esc Cancel", f, back)
}

func (w *workspace) login(p config.Profile) {
	f := connectionForm()
	password := tview.NewInputField().SetLabel("Password").SetMaskCharacter('•').SetFieldWidth(40)
	f.AddFormItem(password)
	back := func() { password.SetText(""); w.manager("") }
	connect := func() {
		v := password.GetText()
		if v == "" {
			w.dialog.message = "Enter the XenAPI password"
			return
		}
		password.SetText("")
		w.connect(p, v)
	}
	f.AddButton("Connect", connect).AddButton("Cancel", back)
	w.show("XENAPI LOGIN", p.User+" @ "+p.Address+"   ·   Password stays in memory", f, back)
}
