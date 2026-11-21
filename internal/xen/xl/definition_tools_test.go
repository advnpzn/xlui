package xl

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/advnpzn/xlui/internal/transport"
	"github.com/advnpzn/xlui/internal/xen"
)

func TestPreviewAndSaveWithExternalCopyTool(t *testing.T) {
	bin := t.TempDir()
	for _, name := range []string{"cat", "flock", "head", "mktemp", "mv", "rm", "sha256sum", "stat", "touch"} {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(path, filepath.Join(bin, name)); err != nil {
			t.Fatal(err)
		}
	}
	writeTool := func(name, script string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	// BusyBox ash can resolve a bare cp to its own applet even when an
	// external executable with --attributes-only support is in PATH.
	writeTool("sh", `[ "$1" = -c ] || exit 1
script=$2
shift 2
exec /bin/sh -c 'cp() { return 2; }
'"$script" "$@"
`)
	writeTool("xl", `case "$1" in
list) echo 'Name ID Mem VCPUs State Time(s) UUID Reason-Code Security Label' ;;
create) [ "$2" = -n ] && [ "$3" = -q ] && [ -s "$4" ] ;;
*) exit 1 ;;
esac
`)
	cp, err := exec.LookPath("cp")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_EXTERNAL_CP", cp)
	writeTool("cp", `[ "$TEST_CAN_COPY_ATTRIBUTES" = yes ] || exit 1
exec "$TEST_EXTERNAL_CP" "$@"
`)
	t.Setenv("PATH", bin)
	dir := t.TempDir()
	original := "name='worker'\nuuid='12345678-1234-1234-1234-123456789abc'\nvcpus=1 # keep\n"
	path := filepath.Join(dir, "worker.cfg")
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	const mode = 0440
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	checkFile := func(want string) {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil || string(data) != want {
			t.Fatalf("definition = %q, %v", data, err)
		}
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("definition metadata = %v, %v", info, err)
		}
		if want == original && (!os.SameFile(info, before) || !info.ModTime().Equal(before.ModTime())) {
			t.Fatal("preview or failed save replaced or touched the original")
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 1 {
			t.Fatalf("definition directory = %v, %v", entries, err)
		}
	}
	m := definitions{transport.Runner{}, dir}
	req := xen.Change{Action: xen.SetVCPUs, Name: "worker", VCPUs: 2}
	t.Setenv("TEST_CAN_COPY_ATTRIBUTES", "no")
	plan, err := m.prepare(t.Context(), req)
	if err == nil || !strings.Contains(err.Error(), "cp") || !strings.Contains(err.Error(), "metadata") || plan.Apply != nil {
		t.Fatalf("preview without metadata support: plan %q, error %v", plan.Description, err)
	}
	checkFile(original)
	t.Setenv("TEST_CAN_COPY_ATTRIBUTES", "yes")
	plan, err = m.prepare(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	checkFile(original)
	t.Setenv("TEST_CAN_COPY_ATTRIBUTES", "no")
	if _, err := plan.Apply(t.Context()); err == nil {
		t.Fatal("save succeeded after the tool lost metadata support")
	}
	checkFile(original)
	t.Setenv("TEST_CAN_COPY_ATTRIBUTES", "yes")
	plan, err = m.prepare(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plan.Apply(t.Context()); err != nil {
		t.Fatal(err)
	}
	checkFile(strings.Replace(original, "vcpus=1", "vcpus=2", 1))
}
