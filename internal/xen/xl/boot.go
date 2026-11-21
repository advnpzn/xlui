package xl

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/advnpzn/xlui/internal/xen"
)

// Use xl's own parser for saved definitions, including their storage and
// network settings. Keep the exact reviewed bytes separate from shell code.
func (m definitions) prepareBoot(ctx context.Context, name string) (xen.Plan, error) {
	raw, err := m.file(ctx, "read", name, "", "")
	if err != nil {
		return xen.Plan{}, err
	}
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(raw)))
	inspect := func(ctx context.Context) (string, error) {
		out, err := m.file(ctx, "inspect-boot", name, digest, raw)
		if err != nil {
			return "", err
		}
		var cfg struct {
			Info struct {
				Name string `json:"name"`
				UUID string `json:"uuid"`
			} `json:"c_info"`
		}
		if json.Unmarshal([]byte(out), &cfg) != nil || cfg.Info.Name != name || !xen.GuestUUID(cfg.Info.UUID) {
			return "", errors.New("xl dry-run must return JSON with a matching guest name and UUID")
		}
		return strings.ToLower(cfg.Info.UUID), nil
	}
	uuid, err := inspect(ctx)
	if err != nil {
		return xen.Plan{}, err
	}
	if err = m.checkTarget(ctx, name, uuid, xen.Identity{}, true); err != nil {
		return xen.Plan{}, err
	}
	summary := fmt.Sprintf("Start %s\nUUID %s\nDefinition %s\nThe guest will use the disks, network and devices configured in this file.", name, uuid, path.Join(m.dir, name+".cfg"))
	return xen.NewPlan(summary, func(ctx context.Context) (string, error) {
		fresh, err := inspect(ctx)
		if err != nil {
			return "", err
		}
		if fresh != uuid {
			return "", errors.New("guest UUID changed. Set an explicit UUID in the boot definition and preview again")
		}
		if err = m.checkTarget(ctx, name, uuid, xen.Identity{}, true); err != nil {
			return "", err
		}
		if _, err = m.file(ctx, "boot", name, digest, raw); err != nil {
			return "", fmt.Errorf("start could not be verified; inspect inventory before retrying: %w", err)
		}
		return m.verifyStarted(ctx, name, uuid)
	}), nil
}

func (m definitions) verifyStarted(ctx context.Context, name, uuid string) (string, error) {
	rows, err := ReadDomains(ctx, m.runner)
	if err == nil {
		for _, d := range rows {
			if d.ID > 0 && d.Name == name && strings.EqualFold(d.UUID, uuid) {
				return fmt.Sprintf("Guest started. Domain ID %d, UUID %s", d.ID, uuid), nil
			}
		}
	}
	return "", errors.New("running guest could not be verified. Check the definition and inventory before retrying")
}
