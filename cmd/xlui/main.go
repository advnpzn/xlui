package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/advnpzn/xlui/internal/transport"
	"github.com/advnpzn/xlui/internal/xen/xl"
)

const usage = "Usage: xlui [--host ssh://user@host | ssh-alias] list"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("xlui", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { fmt.Fprintln(stderr, usage) }
	host := flags.String("host", "", "OpenSSH destination (omit for local Dom0)")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 1 || flags.Arg(0) != "list" {
		return errors.New(usage)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	runner, err := transport.NewRunner(*host)
	if err != nil {
		return err
	}
	if err := xl.StreamInventory(ctx, runner, stdout, stderr); err != nil {
		return fmt.Errorf("domain inventory: %w", err)
	}
	return nil
}
