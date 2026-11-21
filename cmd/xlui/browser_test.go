package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/advnpzn/xlui/internal/config"
	"github.com/advnpzn/xlui/internal/tui"
)

func TestBrowserRouting(t *testing.T) {
	const listing = "Name ID Mem VCPUs State Time(s) UUID Reason-Code Security Label\n" +
		"worker 4 256 1 -b---- 2.0 12345678-1234-1234-1234-123456789abc - -\n"
	for _, tc := range []struct {
		executable string
		args       []string
		host       string
		interval   time.Duration
	}{
		{"xl", nil, "local Dom0", 5 * time.Second},
		{"ssh", []string{"--host", "bench", "--interval", "2s"}, "bench", 2 * time.Second},
	} {
		t.Run(tc.executable, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("PATH", dir)
			script := "#!/bin/sh\n"
			if tc.executable == "ssh" {
				script += `[ "$*" = "-T -a -x -o ClearAllForwardings=yes -o BatchMode=yes -o StrictHostKeyChecking=yes -- bench LC_ALL=C xl list -v" ] || exit 1` + "\n"
			}
			script += "printf '%s' '" + listing + "'\n"
			if err := os.WriteFile(filepath.Join(dir, tc.executable), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			opened := false
			browser := func(ctx context.Context, p config.Profile, interval time.Duration, pass string, open tui.OpenSource) error {
				opened = true
				if _, deadline := ctx.Deadline(); deadline {
					t.Fatal("CLI set a deadline on the interactive session")
				}
				host := p.Name
				if host != tc.host || interval != tc.interval {
					t.Fatalf("host %q, interval %v", host, interval)
				}
				src, err := open(p, pass)
				if err != nil {
					t.Fatal(err)
				}
				domains, err := src.Read(ctx)
				if err != nil || len(domains) != 1 || domains[0].Name != "worker" {
					t.Fatalf("domains %v, err %v", domains, err)
				}
				return nil
			}
			if err := runWithBrowser(context.Background(), tc.args, io.Discard, io.Discard, browser); err != nil || !opened {
				t.Fatalf("opened %t, err %v", opened, err)
			}
		})
	}
}

func TestBrowserFlags(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--interval", "500ms"}, "at least 1s"},
		{[]string{"--interval", "0s"}, "at least 1s"},
		{[]string{"--interval", "5s", "list"}, "only to the interactive browser"},
		{[]string{"--xapi", "https://pool"}, "--xapi-user"},
		{[]string{"helpme"}, "Usage:"},
	} {
		err := run(context.Background(), tc.args, io.Discard, io.Discard)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("args %q: got %v, want %q", tc.args, err, tc.want)
		}
	}
}
