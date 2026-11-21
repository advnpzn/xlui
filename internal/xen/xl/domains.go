package xl

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/advnpzn/xlui/internal/xen"
)

const maxInventoryBytes = 4 << 20

const verboseHeader = "Name ID Mem VCPUs State Time(s) UUID Reason-Code Security Label"

// xl pads names to a minimum width and prints them verbatim. A name can
// contain spaces, so locate its end using the typed columns that follow it.
var verboseRow = regexp.MustCompile(`^(.+?)\s+` +
	`([0-9]+)\s+([0-9]+)\s+([0-9]+)\s+` + // domid, MiB, online vCPUs
	`([r-][b-][p-][s-][rscwS?-][d-])\s+([0-9.]+)\s+` + // state, CPU seconds
	`([[:xdigit:]]{8}-[[:xdigit:]]{4}-[[:xdigit:]]{4}-[[:xdigit:]]{4}-[[:xdigit:]]{12})` +
	`\s+([-]|[[:xdigit:]]+)\s+\S+$`) // shutdown reason, one security label

// ReadDomains obtains one complete domain inventory with a single xl call.
// A failed command or malformed listing never publishes a partial snapshot.
func ReadDomains(ctx context.Context, runner Runner) ([]xen.Domain, error) {
	stdout := cappedOutput{limit: maxInventoryBytes}
	stderr := cappedOutput{limit: 4096}
	if err := StreamInventory(ctx, runner, &stdout, &stderr); err != nil {
		if detail := bytes.TrimSpace(stderr.buf.Bytes()); len(detail) > 0 {
			return nil, fmt.Errorf("xl list -v: %w (%q)", err, detail)
		}
		return nil, fmt.Errorf("xl list -v: %w", err)
	}
	if stdout.truncated {
		return nil, fmt.Errorf("xl list -v exceeded %d bytes", stdout.limit)
	}

	lines := bufio.NewScanner(&stdout.buf)
	if !lines.Scan() || strings.Join(strings.Fields(lines.Text()), " ") != verboseHeader {
		return nil, errors.New("unrecognized xl list -v header")
	}
	var domains []xen.Domain
	seen := make(map[int]struct{})
	for line := 2; lines.Scan(); line++ {
		row := strings.TrimRight(lines.Text(), " \t")
		if row == "" {
			continue
		}
		fields := verboseRow.FindStringSubmatch(row)
		if fields == nil {
			return nil, fmt.Errorf("invalid xl list -v row at line %d", line)
		}
		id, idErr := strconv.Atoi(fields[2])
		mem, memErr := strconv.ParseInt(fields[3], 10, 64)
		cpus, cpusErr := strconv.Atoi(fields[4])
		seconds, secondsErr := strconv.ParseFloat(fields[6], 64)
		if idErr != nil || memErr != nil || cpusErr != nil || secondsErr != nil {
			return nil, fmt.Errorf("invalid xl list -v number at line %d", line)
		}
		if _, exists := seen[id]; exists {
			return nil, fmt.Errorf("duplicate xl domain ID %d at line %d", id, line)
		}
		seen[id] = struct{}{}
		domains = append(domains, xen.Domain{
			Name: fields[1], ID: id, MemoryMiB: mem, VCPUs: cpus,
			State: fields[5], CPUSeconds: seconds, UUID: strings.ToLower(fields[7]),
		})
	}
	if err := lines.Err(); err != nil {
		return nil, fmt.Errorf("reading xl list -v: %w", err)
	}
	return domains, nil
}

// cappedOutput retains a bounded prefix while allowing the process to drain.
type cappedOutput struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (b *cappedOutput) Write(p []byte) (int, error) {
	n := len(p)
	if room := b.limit - b.buf.Len(); n > room {
		b.truncated = true
		p = p[:room]
	}
	b.buf.Write(p)
	return n, nil
}
