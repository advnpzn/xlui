package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/advnpzn/xlui/internal/config"
	"github.com/advnpzn/xlui/internal/session"
	"github.com/advnpzn/xlui/internal/terminal"
	"github.com/advnpzn/xlui/internal/transport"
	"github.com/advnpzn/xlui/internal/tui"
	"github.com/advnpzn/xlui/internal/xen"
	"github.com/advnpzn/xlui/internal/xen/xapi"
	"github.com/advnpzn/xlui/internal/xen/xl"
)

const usage = "Usage: xlui [--host SSH_DESTINATION | --xapi HTTPS_URL --xapi-user USER [--xapi-ca PEM_FILE]] [--interval DURATION] [--layout NAME] [--read-only] [list]"

type browserFunc func(context.Context, config.Profile, time.Duration, tui.Layout, string, tui.OpenSource) error

func main() {
	os.Exit(command())
}

func command() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	out, finishOut := terminal.Output(os.Stdout)
	diagnostics, finishDiagnostics := terminal.Output(os.Stderr)
	err := run(ctx, os.Args[1:], out, diagnostics)
	err = errors.Join(err, finishOut())
	if err != nil {
		fmt.Fprintln(diagnostics, err)
	}
	if flushErr := finishDiagnostics(); err != nil || flushErr != nil {
		return 1
	}
	return 0
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
	readOnly := flags.Bool("read-only", false, "disable guest changes in all sessions")
	host := flags.String("host", "", "OpenSSH destination (omit for local Dom0)")
	interval := flags.Duration("interval", 0, "override the saved monitoring interval for new sessions (built-in 2s, minimum 100ms)")
	layoutName := flags.String("layout", "overview", "dashboard layout: overview, inventory or split")
	endpoint := flags.String("xapi", "", "HTTPS URL of the XAPI pool coordinator")
	username := flags.String("xapi-user", "", "XAPI user (read the password from XLUI_XAPI_PASSWORD)")
	caFile := flags.String("xapi-ca", "", "PEM file containing additional trusted XAPI CA certificates")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	openSource := tui.OpenSource(openMonitor)
	if *readOnly {
		openSource = func(p config.Profile, pass string) (session.Source, error) {
			source, err := openMonitor(p, pass)
			source.Prepare = nil
			return source, err
		}
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
		if supplied["layout"] {
			return errors.New("--layout applies only to the interactive browser")
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}
	if interactive && supplied["interval"] && *interval < tui.MinInterval {
		return fmt.Errorf("--interval must be at least %s", tui.MinInterval)
	}
	layout, err := tui.ParseLayout(*layoutName)
	if err != nil {
		return err
	}
	if supplied["xapi"] {
		if supplied["host"] {
			return errors.New("--host and --xapi are mutually exclusive")
		}

		if *endpoint == "" || *username == "" {
			return errors.New("--xapi requires an HTTPS endpoint and --xapi-user")
		}
		if supplied["xapi-ca"] && *caFile == "" {
			return errors.New("--xapi-ca requires a PEM file")
		}
		if interactive {
			p := config.Profile{Name: "XenAPI pool", Type: config.XAPI, Address: *endpoint, User: *username, CAFile: *caFile}
			if err := p.Validate(); err != nil {
				return err
			}
			return openBrowser(ctx, p, *interval, layout, os.Getenv("XLUI_XAPI_PASSWORD"), openSource)
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
		p := config.Profile{Name: "local Dom0", Type: config.Local}
		if *host != "" {
			p = config.Profile{Name: *host, Type: config.SSH, Address: *host}
		}
		return openBrowser(ctx, p, *interval, layout, "", openSource)
	}
	if err := xl.StreamInventory(ctx, runner, stdout, stderr); err != nil {
		return fmt.Errorf("domain inventory: %w", err)
	}
	return nil
}

func openMonitor(p config.Profile, password string) (session.Source, error) {
	if p.Type == config.XAPI {
		client, err := xapiClient(p.CAFile)
		if err != nil {
			return session.Source{}, err
		}
		m, err := xapi.NewMonitor(p.Address, p.User, password, client)
		if err != nil {
			return session.Source{}, err
		}
		return session.Source{Read: m.Read, Close: m.Close, Prepare: m.Prepare}, nil
	}
	runner, err := transport.NewRunner(p.Address)
	if err != nil {
		return session.Source{}, err
	}
	runner.BatchMode = true
	return session.Source{
		Prepare: xl.GuestChanges(runner),
		Read:    func(ctx context.Context) ([]xen.Domain, error) { return xl.ReadMonitor(ctx, runner) },
		ReadVCPUs: func(ctx context.Context, target xen.Identity) ([]xen.VCPU, error) {
			return xl.ReadVCPUs(ctx, runner, target)
		},
	}, nil
}
