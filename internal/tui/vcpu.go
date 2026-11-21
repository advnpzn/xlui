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

func (b *browser) canReadVCPUs() bool {
	return b.vcpus != nil && b.inspect != nil && !b.inspect.gone && b.lastError == nil
}

func (w *workspace) vcpuPlacement(h *liveSession, read session.VCPUReader) {
	if w.active != h || !h.view.canReadVCPUs() {
		return
	}
	i := h.view.inspect
	target := i.key
	body := tview.NewTextView().SetWordWrap(true).SetTextColor(ink).SetText("Reading vCPU placement…")
	body.SetBackgroundColor(surface)
	hint := "Esc Back · ↑↓ Scroll · " + h.view.host + " · " + terminal.SingleLine(i.domain.Name)
	d := w.show("VCPU PLACEMENT", hint, body, func() { w.activate(h) })
	ctx, cancel := context.WithTimeout(h.ctx, 30*time.Second)
	d.inspector, d.cancel = i, cancel
	w.workers.Add(1)
	go func() {
		defer w.workers.Done()
		defer cancel()
		rows, err := read(ctx, target)
		var result string
		if err != nil {
			result = "Could not read vCPU placement: " + terminal.SingleLine(err.Error())
		} else {
			result = formatVCPUs(rows, time.Now())
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
