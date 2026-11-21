package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Stub the OS executables to check the actual argument boundary, environment and exit status.
func TestInventory(t *testing.T) {
	dir := t.TempDir()
	const prelude = "#!/bin/sh\nif [ \"${XLUI_XAPI_PASSWORD+x}\" = x ]; then exit 17; fi\n"
	for name, script := range map[string]string{
		"xl":  "printf '%s\\n' \"$LC_ALL\" \"$@\"\n",
		"ssh": "printf '%s\\n' \"$@\"\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(prelude+script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	t.Setenv("XLUI_XAPI_PASSWORD", "unused-by-this-provider")
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"local", []string{"list"}, "C\nlist\n-v\n"},
		{"remote", []string{"--host", "ssh://operator@bench", "list"}, "-T\n--\nssh://operator@bench\nLC_ALL=C xl list -v\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := run(context.Background(), tc.args, &out, io.Discard); err != nil {
				t.Fatal(err)
			}
			if out.String() != tc.want {
				t.Fatalf("got %q, want %q", out.String(), tc.want)
			}
		})
	}
}

func TestInvalidDestination(t *testing.T) {
	for _, host := range []string{"-oProxyCommand=date", "bench;date", "ssh://user@host/path"} {
		err := run(context.Background(), []string{"--host", host, "list"}, io.Discard, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "invalid SSH destination") {
			t.Fatalf("host %q: %v", host, err)
		}
	}
}

func TestBackendFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "xl"), []byte("#!/bin/sh\necho 'permission denied' >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	var stderr bytes.Buffer
	if err := run(context.Background(), []string{"list"}, io.Discard, &stderr); err == nil || !strings.Contains(stderr.String(), "permission denied") {
		t.Fatalf("err=%v, stderr=%q", err, stderr.String())
	}
}
