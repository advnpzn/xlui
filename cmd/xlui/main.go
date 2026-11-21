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

	"github.com/advnpzn/xlui/internal/session"
	"github.com/advnpzn/xlui/internal/transport"
	"github.com/advnpzn/xlui/internal/tui"
	"github.com/advnpzn/xlui/internal/xen"
	"github.com/advnpzn/xlui/internal/xen/xl"
)

const usage = "Usage: xlui [--host SSH_DESTINATION] [--interval DURATION]\n" +
	"       xlui [--host SSH_DESTINATION | --xapi HTTPS_URL --xapi-user USER [--xapi-ca PEM_FILE]] list"

type browserFunc func(context.Context, string, time.Duration, session.Reader) error

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	return runWithBrowser(ctx, args, stdout, stderr, tui.Run)
}

func runWithBrowser(ctx context.Context, args []string, stdout, stderr io.Writer, openBrowser browserFunc) error {
	flags := flag.NewFlagSet("xlui", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintln(stderr, usage)
		flags.PrintDefaults()
	}
	host := flags.String("host", "", "OpenSSH destination (omit for local Dom0)")
	interval := flags.Duration("interval", 5*time.Second, "interactive inventory refresh interval (minimum 1s)")
	endpoint := flags.String("xapi", "", "HTTPS URL of the XAPI pool coordinator")
	username := flags.String("xapi-user", "", "XAPI user (read the password from XLUI_XAPI_PASSWORD)")
	caFile := flags.String("xapi-ca", "", "PEM file containing additional trusted XAPI CA certificates")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	interactive := flags.NArg() == 0
	if !interactive && (flags.NArg() != 1 || flags.Arg(0) != "list") {
		return errors.New(usage)
	}

	supplied := make(map[string]bool)
	flags.Visit(func(f *flag.Flag) { supplied[f.Name] = true })
	if !interactive {
		if supplied["interval"] {
			return errors.New("--interval applies only to the interactive browser")
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}
	if supplied["xapi"] {
		if supplied["host"] {
			return errors.New("--host and --xapi are mutually exclusive")
		}
		if interactive {
			return errors.New("interactive XAPI inventory is unavailable; use xlui --xapi HTTPS_URL --xapi-user USER list")
		}
		if *endpoint == "" || *username == "" {
			return errors.New("--xapi requires an HTTPS endpoint and --xapi-user")
		}
		if supplied["xapi-ca"] && *caFile == "" {
			return errors.New("--xapi-ca requires a PEM file")
		}
		return listXAPI(ctx, *endpoint, *username, *caFile, stdout)
	}
	if supplied["xapi-user"] || supplied["xapi-ca"] {
		return errors.New("--xapi-user and --xapi-ca require --xapi")
	}
	runner, err := transport.NewRunner(*host)
	if err != nil {
		return err
	}
	if interactive {
		if *interval < time.Second {
			return errors.New("--interval must be at least 1s")
		}
		label := *host
		if label == "" {
			label = "local Dom0"
		}
		return openBrowser(ctx, label, *interval, func(ctx context.Context) ([]xen.Domain, error) {
			return xl.ReadDomains(ctx, runner)
		})
	}
	if err := xl.StreamInventory(ctx, runner, stdout, stderr); err != nil {
		return fmt.Errorf("domain inventory: %w", err)
	}
	return nil
}
