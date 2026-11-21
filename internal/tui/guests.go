package tui

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/advnpzn/xlui/internal/config"
	"github.com/advnpzn/xlui/internal/terminal"
	"github.com/advnpzn/xlui/internal/xen"
	"github.com/rivo/tview"
)

type guestResult struct {
	host   *liveSession
	dialog *connectionDialog
	show   func()
}

func (h *liveSession) guestTarget() (xen.Domain, bool) {
	if h.view.inspect != nil {
		i := h.view.inspect
		return i.domain, !i.gone
	}
	row, _ := h.view.table.GetSelection()
	if _, ok := h.view.selected(); !ok {
		return xen.Domain{}, false
	}
	return h.view.rows.Domains[h.view.rows.visible[row-1]], true
}

func (w *workspace) guestMenu(h *liveSession) {
	if w.active != h || h.prepare == nil {
		return
	}
	list := connectionList()
	back := func() { w.activate(h) }
	d := w.show("GUESTS", h.profile.Name+" · "+connectionAddress(h.profile), list, back)
	target, selected := h.guestTarget()
	if h.view.latest.IsZero() || h.view.lastError != nil {
		d.message = "Wait for a successful inventory refresh before changing guests."
	} else {
		pool := h.profile.Type == config.XAPI
		createHint := "Define and start a Linux guest from a Dom0 kernel and initramfs"
		if pool {
			createHint = "Clone a halted guest VM in this pool"
		}
		list.AddItem("Create guest", createHint, 'c', func() { w.guestForm(h, xen.CreateGuest, target) })
		editable := selected && !target.ControlDomain && target.ID != 0
		if pool {
			if editable {
				list.AddItem("Rename guest", tview.Escape(terminal.SingleLine(target.Name)), 'r', func() { w.guestForm(h, xen.RenameGuest, target) })
			}
			editable = editable && target.State == "Halted"
		}
		if editable {
			label := tview.Escape(terminal.SingleLine(target.Name))
			list.AddItem("Edit boot memory", label, 'm', func() { w.guestForm(h, xen.SetMemory, target) })
			list.AddItem("Edit startup vCPUs", label, 'v', func() { w.guestForm(h, xen.SetVCPUs, target) })
		}
		if !pool {
			list.AddItem("Start from saved definition", "Create a domain using the boot devices in an existing .cfg", 's', func() { w.guestForm(h, xen.BootDefinition, xen.Domain{}) })
			list.AddItem("Edit saved boot memory", "Open a Dom0 definition by guest name", 'b', func() { w.guestForm(h, xen.SetMemory, xen.Domain{}) })
			list.AddItem("Edit saved vCPUs", "Open a Dom0 definition by guest name", 'p', func() { w.guestForm(h, xen.SetVCPUs, xen.Domain{}) })
		}
	}
	list.AddItem("Back", "Return to dashboard", 0, back)
	// show() focused the empty list; select the first enabled operation now.
	list.SetCurrentItem(0)
}

func (w *workspace) guestForm(h *liveSession, kind xen.Action, target xen.Domain) {
	f := connectionForm()
	c := xen.Change{Action: kind, Target: target.Identity(), Name: target.Name}
	create := kind == xen.CreateGuest
	if create {
		c.Name = ""
	}
	if create || target.UUID == "" {
		f.AddInputField("Guest name", c.Name, 40, nil, func(s string) { c.Name = s })
	}
	mem, cpus := "512", "1"
	if kind == xen.RenameGuest {
		c.NewName = target.Name
		f.AddInputField("New pool display name", c.NewName, 40, nil, func(s string) { c.NewName = s })
	} else if h.profile.Type == config.XAPI && create {
		if !target.ControlDomain && target.State == "Halted" {
			c.SourceUUID = target.UUID
		}
		f.AddInputField("Halted source UUID", c.SourceUUID, 40, nil, func(s string) { c.SourceUUID = s })
	} else if kind != xen.BootDefinition {
		if create {
			f.AddInputField("Kernel on Dom0", "", 50, nil, func(s string) { c.Kernel = s })
			f.AddInputField("Initramfs on Dom0", "", 50, nil, func(s string) { c.Initrd = s })
			c.Cmdline = "console=hvc0"
			f.AddInputField("Kernel arguments", c.Cmdline, 50, nil, func(s string) { c.Cmdline = s })
		}
		if !create {
			mem = ""
			cpus = ""
		}
		if create || kind == xen.SetMemory {
			f.AddInputField("Memory (MiB)", mem, 18, nil, func(s string) { mem = s })
		}
		if create || kind == xen.SetVCPUs {
			f.AddInputField("Startup vCPUs", cpus, 18, nil, func(s string) { cpus = s })
		}
	}
	back := func() { w.guestMenu(h) }
	title := "EDIT GUEST"
	if kind == xen.RenameGuest {
		title = "RENAME GUEST"
	}
	if create {
		title = "CREATE GUEST"
	}
	if kind == xen.BootDefinition {
		title = "START FROM DEFINITION"
	}
	hint := h.profile.Name
	if !create && target.UUID != "" {
		hint += " · " + target.Name + " · " + target.UUID
	}
	f.AddButton("Preview", func() {
		if h.view.lastError != nil {
			w.dialog.message = "Inventory is stale. Reopen Guests after a successful refresh."
			return
		}
		if !create || h.profile.Type != config.XAPI {
			if create || kind == xen.SetMemory {
				v, err := strconv.ParseInt(mem, 10, 64)
				if err != nil || v <= 0 {
					w.dialog.message = "Enter a positive memory size in MiB"
					return
				}
				c.MemoryMiB = v
			}
			if create || kind == xen.SetVCPUs {
				v, err := strconv.ParseInt(cpus, 10, 64)
				if err != nil || v <= 0 {
					w.dialog.message = "Enter a positive vCPU count"
					return
				}
				c.VCPUs = v
			}
		}
		form := w.dialog
		request := c
		w.guestWork(h, "Preparing preview", true, func(ctx context.Context) func() {
			plan, err := h.prepare(ctx, request)
			return func() {
				if err != nil {
					w.dialog = form
					form.message = err.Error()
					return
				}
				w.confirmGuest(h, plan, func() { w.dialog = form })
			}
		})
	}).AddButton("Cancel", back)
	w.show(title, hint, f, back)
}

// Workers return presentation callbacks to the terminal loop. A discarded
// dialog or disconnected host cannot receive a stale completion.
func (w *workspace) guestWork(h *liveSession, title string, preview bool, work func(context.Context) func()) {
	ctx, cancel := context.WithTimeout(h.ctx, 2*time.Minute)
	list := connectionList()
	back := func() {}
	if preview {
		back = func() { cancel(); w.guestMenu(h) }
		list.AddItem("Cancel preview", "Return to guest actions", 'b', back)
	} else {
		list.AddItem("Applying confirmed change", "Waiting for the provider to verify the outcome", 0, nil)
	}
	d := w.show(title, h.profile.Name+" · "+connectionAddress(h.profile), list, back)
	w.workers.Add(1)
	go func() {
		defer w.workers.Done()
		defer cancel()
		show := work(ctx)
		select {
		case w.guestResults <- guestResult{h, d, show}:
		case <-w.ctx.Done():
		}
	}()
}

func (w *workspace) confirmGuest(h *liveSession, plan xen.Plan, back func()) {
	f := connectionForm()
	description := fmt.Sprintf("Connection %s\n%s\n\n%s", terminal.SingleLine(h.profile.Name), terminal.SingleLine(connectionAddress(h.profile)), plan.Description)
	f.AddTextView("", description, 0, 8, false, true)
	f.AddButton("Back", back).AddButton("Apply", func() {
		if h.view.lastError != nil {
			w.dialog.message = "Inventory is stale. Prepare a new preview."
			return
		}
		w.guestWork(h, "APPLYING GUEST CHANGE", false, func(ctx context.Context) func() {
			message, err := plan.Apply(ctx)
			return func() {
				h.view.inventory.Refresh()
				title := "GUEST UPDATED"
				if err != nil {
					title = "GUEST CHANGE RESULT"
					message = err.Error()
				}
				form := connectionForm()
				done := func() { w.activate(h) }
				form.AddTextView("", message, 0, 6, false, true).AddButton("Done", done)
				w.show(title, h.profile.Name, form, done)
			}
		})
	})
	f.SetFocus(1) // Back is the initial button. Applying requires a deliberate selection.
	w.show("REVIEW GUEST CHANGE", "Review the host and values. Select Apply to send the change.", f, back)
}
