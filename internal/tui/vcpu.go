package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/advnpzn/xlui/internal/session"
	"github.com/advnpzn/xlui/internal/terminal"
	"github.com/advnpzn/xlui/internal/xen"
	"github.com/rivo/tview"
)

func (b *browser) canReadDetail() bool {
	return b.inspect != nil && !b.inspect.gone && b.lastError == nil
}

func (b *browser) canReadResidentHost() bool {
	if b.residentHost == nil || !b.canReadDetail() {
		return false
	}
	d := b.inspect.domain
	return d.ID >= 0 && (d.State == "Running" || d.State == "Paused")
}

func (w *workspace) bindInspectorReads(h *liveSession, source session.Source) {
	if source.ReadVCPUs != nil {
		h.view.vcpus = func() {
			w.readDetail(h, "vCPU placement", func(ctx context.Context, target xen.Identity) (string, error) {
				rows, err := source.ReadVCPUs(ctx, target)
				return formatVCPUs(rows, time.Now()), err
			})
		}
	}
	if source.ReadResidentHost != nil {
		h.view.residentHost = func() {
			w.readDetail(h, "resident host", func(ctx context.Context, target xen.Identity) (string, error) {
				host, err := source.ReadResidentHost(ctx, target)
				return fmt.Sprintf("Sampled %s\n\nHost  %s\nUUID  %s", time.Now().UTC().Format(time.RFC3339), terminal.SingleLine(host.Name), terminal.SingleLine(host.UUID)), err
			})
		}
	}
}

func (w *workspace) readDetail(h *liveSession, label string, read func(context.Context, xen.Identity) (string, error)) {
	if w.active != h || !h.view.canReadDetail() {
		return
	}
	i := h.view.inspect
	target := i.key
	body := tview.NewTextView().SetWordWrap(true).SetTextColor(ink).SetText("Reading " + label + "…")
	body.SetBackgroundColor(surface)
	hint := "Esc Back · ↑↓ Scroll · " + h.view.host + " · " + terminal.SingleLine(i.domain.Name)
	d := w.show(strings.ToUpper(label), hint, body, func() { w.activate(h) })
	ctx, cancel := context.WithTimeout(h.ctx, 30*time.Second)
	d.inspector, d.cancel = i, cancel
	w.workers.Add(1)
	go func() {
		defer w.workers.Done()
		defer cancel()
		result, err := read(ctx, target)
		if err != nil {
			result = "Could not read " + label + ": " + terminal.SingleLine(err.Error())
		}
		select {
		case w.dialogResults <- dialogResult{h, d, func() { body.SetText(result).ScrollToBeginning() }}:
		case <-h.ctx.Done():
		}
	}()
}

func formatVCPUs(rows []xen.VCPU, observed time.Time) string {
	var out strings.Builder
	fmt.Fprintf(&out, "Sampled %s\n", observed.UTC().Format(time.RFC3339))
	for _, row := range rows {
		cpu := "offline"
		if row.CPU >= 0 {
			cpu = strconv.Itoa(row.CPU)
		}
		fmt.Fprintf(&out, "\nvCPU %d  ·  pCPU %s  ·  %s  ·  %.1f s\n  Hard: %s  ·  Soft: %s\n",
			row.ID, cpu, row.State, row.Seconds, row.Hard, row.Soft)
	}
	return out.String()
}
