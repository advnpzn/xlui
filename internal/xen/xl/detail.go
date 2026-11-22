package xl

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/advnpzn/xlui/internal/xen"
)

// xl detail commands address a recyclable domid and omit the domain UUID.
func readDomainDetail[T any](ctx context.Context, runner Runner, target xen.Identity, command string, parse func(*bytes.Buffer, int) ([]T, error)) ([]T, error) {
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
	if err := runner.Run(ctx, &stdout, &stderr, "xl", command, strconv.Itoa(target.ID)); err != nil {
		if detail := bytes.TrimSpace(stderr.buf.Bytes()); len(detail) != 0 {
			return nil, fmt.Errorf("xl %s: %w (%q)", command, err, detail)
		}
		return nil, fmt.Errorf("xl %s: %w", command, err)
	}
	// xl network-list may log a failed device lookup and still exit zero.
	diagnostic := stderr.buf.Bytes()
	if command == "network-list" && (bytes.Contains(diagnostic, []byte("libxl: error:")) || bytes.Contains(diagnostic, []byte("libxl: critical:"))) {
		return nil, fmt.Errorf("xl network-list diagnostic: %q", bytes.TrimSpace(diagnostic))
	}
	if stdout.truncated {
		return nil, fmt.Errorf("xl %s exceeded %d bytes", command, stdout.limit)
	}
	rows, err := parse(&stdout.buf, target.ID)
	if err != nil {
		return nil, err
	}
	if err := check(); err != nil {
		return nil, err
	}
	return rows, nil
}
