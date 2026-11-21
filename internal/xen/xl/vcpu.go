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

const vcpuHeader = "Name ID VCPU CPU State Time(s) Affinity (Hard / Soft)"

// Match the typed columns after the free-form name, including offline vCPUs.
var vcpuRow = regexp.MustCompile(`^.+?\s+([0-9]+)\s+([0-9]+)\s+(-|[0-9]+)\s+([r-][b-]-|--p)\s+([0-9]+\.[0-9]+)\s+(.+?)\s+/\s+(.+)$`)

// ReadVCPUs samples a single guest on demand. xl addresses vcpus by domid
// and does not include domain UUIDs in its result, so check for ID reuse
// on both sides of the sample before exposing it.
func ReadVCPUs(ctx context.Context, runner Runner, target xen.Identity) ([]xen.VCPU, error) {
	check := func() error {
		domains, err := ReadDomains(ctx, runner)
		if err != nil {
			return err
		}
		for _, d := range domains {
			if d.Identity() == target {
				return nil
			}
		}
		return errors.New("selected domain is no longer in the inventory")
	}
	if err := check(); err != nil {
		return nil, err
	}
	stdout, stderr := cappedOutput{limit: maxInventoryBytes}, cappedOutput{limit: 4096}
	if err := runner.Run(ctx, &stdout, &stderr, "xl", "vcpu-list", strconv.Itoa(target.ID)); err != nil {
		if detail := bytes.TrimSpace(stderr.buf.Bytes()); len(detail) != 0 {
			return nil, fmt.Errorf("xl vcpu-list: %w (%q)", err, detail)
		}
		return nil, fmt.Errorf("xl vcpu-list: %w", err)
	}
	if stdout.truncated {
		return nil, fmt.Errorf("xl vcpu-list exceeded %d bytes", stdout.limit)
	}
	rows, err := parseVCPUs(&stdout.buf, target.ID)
	if err != nil {
		return nil, err
	}
	if err := check(); err != nil {
		return nil, err
	}
	return rows, nil
}

func parseVCPUs(out *bytes.Buffer, domid int) ([]xen.VCPU, error) {
	lines := bufio.NewScanner(out)
	if !lines.Scan() || strings.Join(strings.Fields(lines.Text()), " ") != vcpuHeader {
		return nil, errors.New("unsupported xl vcpu-list header (expected hard and soft affinity)")
	}
	var rows []xen.VCPU
	seen := make(map[int]bool)
	for line := 2; lines.Scan(); line++ {
		row := strings.TrimSpace(lines.Text())
		if row == "" {
			continue
		}
		fields := vcpuRow.FindStringSubmatch(row)
		if fields == nil {
			return nil, fmt.Errorf("invalid xl vcpu-list row at line %d", line)
		}
		domain, e1 := strconv.Atoi(fields[1])
		id, e2 := strconv.Atoi(fields[2])
		seconds, e3 := strconv.ParseFloat(fields[5], 64)
		if e1 != nil || domain != domid || e2 != nil || seen[id] || e3 != nil {
			return nil, fmt.Errorf("invalid domain, vCPU ID or time at line %d", line)
		}
		cpu := -1
		if fields[3] != "-" {
			var err error
			cpu, err = strconv.Atoi(fields[3])
			if err != nil {
				return nil, fmt.Errorf("invalid physical CPU at line %d", line)
			}
		}
		if (cpu < 0) != (fields[4] == "--p") || !validAffinity(fields[6]) || !validAffinity(fields[7]) {
			return nil, fmt.Errorf("invalid vCPU state or affinity at line %d", line)
		}
		seen[id] = true
		rows = append(rows, xen.VCPU{ID: id, CPU: cpu, State: fields[4], Seconds: seconds, Hard: fields[6], Soft: fields[7]})
	}
	if err := lines.Err(); err != nil {
		return nil, fmt.Errorf("reading xl vcpu-list: %w", err)
	}
	if len(rows) == 0 {
		return nil, errors.New("xl vcpu-list returned no vCPUs for the selected domain")
	}
	return rows, nil
}

func validAffinity(mask string) bool {
	switch mask {
	case "all", "none", "any cpu":
		return true
	}
	for span := range strings.SplitSeq(mask, ",") {
		lo, hi, hasEnd := strings.Cut(span, "-")
		first, err := strconv.ParseUint(lo, 10, 32)
		if err != nil {
			return false
		}
		if hasEnd {
			last, err := strconv.ParseUint(hi, 10, 32)
			if err != nil || last < first {
				return false
			}
		}
	}
	return true
}
