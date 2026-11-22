package xl

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/advnpzn/xlui/internal/xen"
)

func ReadNetwork(ctx context.Context, runner Runner, target xen.Identity) ([]xen.VIF, error) {
	return readDomainDetail(ctx, runner, target, "network-list", parseNetwork)
}

// xl prints one row per successfully queried interface, including separate
// grant references for the TX and RX rings in a single slash-delimited column.
func parseNetwork(out *bytes.Buffer, _ int) ([]xen.VIF, error) {
	lines := bufio.NewScanner(out)
	if !lines.Scan() || strings.Join(strings.Fields(lines.Text()), " ") != "Idx BE Mac Addr. handle state evt-ch tx-/rx-ring-ref BE-path" {
		return nil, errors.New("unsupported xl network-list header")
	}
	var rows []xen.VIF
	seen := make(map[int]bool)
	for line := 2; lines.Scan(); line++ {
		f := strings.Fields(lines.Text())
		if len(f) == 0 {
			continue
		}
		if len(f) != 8 {
			return nil, fmt.Errorf("invalid xl network-list row at line %d", line)
		}
		tx, rx, ok := strings.Cut(f[6], "/")
		mac, err := net.ParseMAC(f[2])
		if !ok || err != nil || len(mac) != 6 || !strings.HasPrefix(f[7], "/") {
			return nil, fmt.Errorf("invalid interface address, ring references or backend path at line %d", line)
		}
		row := xen.VIF{MAC: mac.String(), BackendPath: f[7]}
		var handle int
		for _, field := range []struct {
			raw string
			dst *int
			min int
		}{{f[0], &row.ID, 0}, {f[1], &row.BackendID, 0}, {f[3], &handle, 0}, {f[4], &row.State, -1},
			{f[5], &row.EventChannel, -1}, {tx, &row.TxRing, -1}, {rx, &row.RxRing, -1}} {
			n, err := strconv.Atoi(field.raw)
			if err != nil || n < field.min {
				return nil, fmt.Errorf("invalid xl network-list number at line %d", line)
			}
			*field.dst = n
		}
		if row.ID != handle || seen[row.ID] {
			return nil, fmt.Errorf("invalid or duplicate VIF index at line %d", line)
		}
		seen[row.ID] = true
		rows = append(rows, row)
	}
	if err := lines.Err(); err != nil {
		return nil, fmt.Errorf("reading xl network-list: %w", err)
	}
	return rows, nil
}
