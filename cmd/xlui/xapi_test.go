package main

import (
	"bytes"
	"context"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/advnpzn/xlui/internal/xen/xapi"
)

func TestXAPIFlags(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("XLUI_XAPI_PASSWORD", "")
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--xapi", "", "list"}, "requires an HTTPS endpoint"},
		{[]string{"--xapi", "https://pool", "list"}, "--xapi-user"},
		{[]string{"--host", "bench", "--xapi", "https://pool", "list"}, "mutually exclusive"},
		{[]string{"--xapi-user", "viewer", "list"}, "require --xapi"},
		{[]string{"--xapi-ca", "root.pem", "list"}, "require --xapi"},
		{[]string{"--xapi", "https://pool", "--xapi-user", "viewer", "--xapi-ca=", "list"}, "requires a PEM file"},
		{[]string{"--xapi", "https://pool", "--xapi-user", "viewer", "list"}, "XLUI_XAPI_PASSWORD"},
	} {
		err := run(context.Background(), tc.args, io.Discard, io.Discard)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("args %q: got %v, want %q", tc.args, err, tc.want)
		}
	}
}

func TestXAPIUsesConfiguredCA(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	// No local or SSH command is available as a fallback.
	t.Setenv("PATH", t.TempDir())
	t.Setenv("XLUI_XAPI_PASSWORD", "test-only-password")
	args := []string{"--xapi", server.URL, "--xapi-user", "viewer"}
	checkFailure := func(flags []string) {
		t.Helper()
		var stdout bytes.Buffer
		err := run(context.Background(), append(flags, "list"), &stdout, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "XAPI inventory") ||
			strings.Contains(err.Error(), "test-only-password") || stdout.Len() != 0 {
			t.Fatalf("stdout %q, err %v", stdout.String(), err)
		}
	}
	checkFailure(args)
	if requests.Load() != 0 {
		t.Fatal("client accepted the untrusted test certificate")
	}
	caFile := filepath.Join(t.TempDir(), "root.pem")
	cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(caFile, cert, 0600); err != nil {
		t.Fatal(err)
	}
	checkFailure(append(args, "--xapi-ca", caFile))
	if requests.Load() != 1 {
		t.Fatalf("server received %d requests, want one verified TLS request", requests.Load())
	}
}

func TestXAPIInventoryPresentation(t *testing.T) {
	vms := []xapi.VM{
		{UUID: "control", Name: "z\n\x1b[1m", PowerState: "Running", ControlDomain: true},
		{UUID: "b", Name: "bench", PowerState: "Halted"},
		{UUID: "a", Name: "bench", PowerState: "Running"},
	}
	var buf bytes.Buffer
	if err := writeXAPIInventory(&buf, vms); err != nil {
		t.Fatal(err)
	}
	rows := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(rows) != 4 ||
		strings.Join(strings.Fields(rows[1]), " ") != "a Running false bench" ||
		strings.Join(strings.Fields(rows[2]), " ") != "b Halted false bench" ||
		strings.Join(strings.Fields(rows[3]), " ") != `control Running true z\n\x1b[1m` {
		t.Fatalf("unexpected table: %q", buf.String())
	}
}
