package xen

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync/atomic"
)

type Action int

const (
	CreateGuest Action = iota + 1
	BootDefinition
	SetMemory
	SetVCPUs
	RenameGuest
)

type Change struct {
	Action Action
	Name   string
	// Target is the selected instance for an edit. SourceUUID identifies a clone source.
	Target                  Identity
	SourceUUID              string
	NewName                 string
	MemoryMiB, VCPUs        int64
	Kernel, Initrd, Cmdline string
}

var safeName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_.-]{0,62}$`)
var uuidText = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func GuestUUID(s string) bool {
	return uuidText.MatchString(s) && s != "00000000-0000-0000-0000-000000000000"
}
func CheckName(name string) error {
	if !safeName.MatchString(name) || strings.EqualFold(name, "Domain-0") || strings.EqualFold(name, "dom0") {
		return errors.New("use a guest name starting with a letter, followed by up to 62 letters, digits, dots, underscores or hyphens")
	}
	return nil
}

type Plan struct {
	Description string
	// Apply rechecks the preview and verifies the outcome. It is invoked only
	// after confirmation, on the provider session that prepared this plan.
	Apply func(context.Context) (string, error)
}
type Prepare func(context.Context, Change) (Plan, error)

// NewPlan binds a single confirmation to one dispatch, including when the
// response is lost. A further attempt needs a fresh provider preview.
func NewPlan(description string, apply func(context.Context) (string, error)) Plan {
	var used atomic.Bool
	return Plan{Description: description, Apply: func(ctx context.Context) (string, error) {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if !used.CompareAndSwap(false, true) {
			return "", errors.New("this preview has already been applied")
		}
		return apply(ctx)
	}}
}
