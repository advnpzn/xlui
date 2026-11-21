package xl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/advnpzn/xlui/internal/xen"
)

type listRunner struct {
	stdout string
	stderr string
	err    error
	calls  [][]string
}

func (r *listRunner) Run(_ context.Context, stdout, stderr io.Writer, program string, args ...string) error {
	r.calls = append(r.calls, append([]string{program}, args...))
	io.WriteString(stdout, r.stdout)
	io.WriteString(stderr, r.stderr)
	return r.err
}

func TestReadDomains(t *testing.T) {
	// Synthetic data formatted as list_domains in Xen RELEASE-4.19.0,
	// tools/xl/xl_info.c. Includes the valid null UUID of a control domain.
	raw, err := os.ReadFile("testdata/list-v-xen-4.19.txt")
	if err != nil {
		t.Fatal(err)
	}
	runner := &listRunner{stdout: string(raw)}
	got, err := ReadDomains(context.Background(), runner)
	if err != nil {
		t.Fatal(err)
	}
	want := []xen.Domain{
		{ControlDomain: true, Name: "Domain-0", ID: 0, MemoryMiB: 670, VCPUs: 4, State: "r-----", CPUSeconds: 9684.2, UUID: "00000000-0000-0000-0000-000000000000"},
		{Name: "worker  01", ID: 9, MemoryMiB: 2048, VCPUs: 2, State: "-b----", CPUSeconds: 123, UUID: "b586dd4f-32ba-478e-a5ca-9c2640c7ef54"},
		{Name: "a guest with a longer than forty character visible name", ID: 42, MemoryMiB: 512, VCPUs: 1, State: "---sS-", UUID: "02f547a0-a926-452c-a5c1-6f36b01cca9e"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	if !reflect.DeepEqual(runner.calls, [][]string{{"xl", "list", "-v"}}) {
		t.Fatalf("commands: %q", runner.calls)
	}
}

func TestReadDomainsEmpty(t *testing.T) {
	domains, err := ReadDomains(context.Background(), &listRunner{stdout: verboseHeader + "\n"})
	if err != nil || len(domains) != 0 {
		t.Fatalf("got %v, %v", domains, err)
	}
}

func TestNameCanContainInventoryFields(t *testing.T) {
	const name = "  guest 9 10 1 r----- 2.5 abcdefab-0000-4000-8000-000000000009 - -"
	const row = name + " 8 128 1 --p--- 4.5 abcdefab-0000-4000-8000-000000000008 - -"
	rows, err := ReadDomains(context.Background(), &listRunner{stdout: verboseHeader + "\n" + row})
	if err != nil || len(rows) != 1 || rows[0].Name != name || rows[0].ID != 8 {
		t.Fatalf("got %v, %v", rows, err)
	}
}

func TestReadDomainsRejectsPartialInventories(t *testing.T) {
	const one = "guest 7 128 1 --p--- 4.5 abcdefab-0000-4000-8000-000000000000 - -\n"
	for name, output := range map[string]string{
		"missing header":  "",
		"plain listing":   "Name ID Mem VCPUs State Time(s)\n",
		"truncated row":   verboseHeader + "\n" + one + "another 8 128\n",
		"duplicate ID":    verboseHeader + "\n" + one + one,
		"invalid UUID":    verboseHeader + "\n" + strings.Replace(one, "abcdefab", "unknown!", 1),
		"invalid counter": verboseHeader + "\n" + strings.Replace(one, "4.5", "4..5", 1),
		"overflow":        verboseHeader + "\n" + strings.Replace(one, "128", "99999999999999999999", 1),
		"output limit":    verboseHeader + "\n" + strings.Repeat("x", maxInventoryBytes),
	} {
		t.Run(name, func(t *testing.T) {
			got, err := ReadDomains(context.Background(), &listRunner{stdout: output})
			if len(got) != 0 || err == nil {
				t.Fatalf("got %v, %v", got, err)
			}
		})
	}
}

func TestReadDomainsRunnerError(t *testing.T) {
	failure := errors.New("command failed")
	runner := &listRunner{stdout: verboseHeader + "\n", stderr: "denied\n", err: failure}
	rows, err := ReadDomains(context.Background(), runner)
	if len(rows) != 0 || !errors.Is(err, failure) || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("got %v, %v", rows, err)
	}
}

// Benchmark the complete in-memory read, including capture and parsing but
// excluding process startup, SSH and the cost of obtaining data from Xen.
func BenchmarkReadDomains256(b *testing.B) {
	var input strings.Builder
	fmt.Fprintln(&input, verboseHeader)
	for id := 0; id < 256; id++ {
		fmt.Fprintf(&input, "guest %03d %d 2048 2 -b---- 123.4 00000000-0000-4000-8000-%012x - -\n", id, id, id)
	}
	runner := &listRunner{stdout: input.String()}
	b.ReportAllocs()
	b.SetBytes(int64(input.Len()))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runner.calls = runner.calls[:0]
		domains, err := ReadDomains(context.Background(), runner)
		if err != nil || len(domains) != 256 {
			b.Fatalf("got %d domains, %v", len(domains), err)
		}
	}
}
