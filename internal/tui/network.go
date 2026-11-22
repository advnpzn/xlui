package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/advnpzn/xlui/internal/terminal"
	"github.com/advnpzn/xlui/internal/xen"
)

func formatNetwork(rows []xen.VIF, observed time.Time) string {
	var out strings.Builder
	fmt.Fprintf(&out, "Sampled %s\n", observed.UTC().Format(time.RFC3339))
	if len(rows) == 0 {
		out.WriteString("\nNo interfaces reported by xl.")
	}
	for _, row := range rows {
		fmt.Fprintf(&out, "\nVIF %d  ·  MAC %s\n  Backend domain %d  ·  Xenbus state %s  ·  Event channel %s\n  TX ring %s  ·  RX ring %s\n  %s\n",
			row.ID, row.MAC, row.BackendID, xenbusState(row.State), number(row.EventChannel), number(row.TxRing), number(row.RxRing), terminal.SingleLine(row.BackendPath))
	}
	return out.String()
}

// State numbers are part of the Xenbus frontend/backend protocol.
func xenbusState(state int) string {
	names := [...]string{"Unknown", "Initialising", "InitWait", "Initialised", "Connected", "Closing", "Closed", "Reconfiguring", "Reconfigured"}
	if state >= 0 && state < len(names) {
		return fmt.Sprintf("%s (%d)", names[state], state)
	}
	return number(state)
}
