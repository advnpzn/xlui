package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"time"
)

const usage = "Usage: xlui [--host ssh://user@host | ssh-alias] list"

var destination = regexp.MustCompile(`^(ssh://)?([a-zA-Z0-9_][a-zA-Z0-9._-]*@)?([a-zA-Z0-9][a-zA-Z0-9._-]*|\[[a-fA-F0-9:]+\])$`)

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

	var cmd *exec.Cmd
	if *host == "" {
		cmd = exec.CommandContext(ctx, "xl", "list", "-v")
		cmd.Env = append(os.Environ(), "LC_ALL=C")
	} else {
		if !destination.MatchString(*host) {
			return errors.New("invalid SSH destination; use an SSH config alias for custom ports or jump hosts")
		}
		// The remote shell receives a fixed command. The SSH destination is a separate argument.
		cmd = exec.CommandContext(ctx, "ssh", "-T", "--", *host, "LC_ALL=C xl list -v")
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("domain inventory: %w", ctx.Err())
		}
		return fmt.Errorf("domain inventory: %w", err)
	}
	return nil
}
