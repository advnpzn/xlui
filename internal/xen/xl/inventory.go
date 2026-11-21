// Package xl implements operations through the plain Xen toolstack.
package xl

import (
	"context"
	"io"
)

// Runner executes a provider-selected program on the management host.
type Runner interface {
	Run(ctx context.Context, stdout, stderr io.Writer, program string, args ...string) error
}

// StreamInventory preserves the host's verbose listing for the headless list command.
func StreamInventory(ctx context.Context, runner Runner, stdout, stderr io.Writer) error {
	return runner.Run(ctx, stdout, stderr, "xl", "list", "-v")
}
