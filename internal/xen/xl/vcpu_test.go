package xl

import (
	"context"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/advnpzn/xlui/internal/xen"
)

// Synthetic output in the format of RELEASE-4.19.0 tools/xl/xl_vcpu.c
// (print_vcpuinfo) and xl_utils.c (print_bitmap). Names can contain spaces.
const vcpuHeaderFixture = "Name                                ID  VCPU   CPU State   Time(s) Affinity (Hard / Soft)\n"
const vcpuRowsFixture = "worker  01                           9     0    4   r--       5.4  0-2,4 / 4\n" +
	"worker  01                           9     1    2   -b-       2.1  all / 0-2\n" +
	"worker  01                           9     2    -   --p       0.0  all / none\n"

type placementRunner struct {
	responses []string
	calls     []string
}

func (r *placementRunner) Run(_ context.Context, stdout, _ io.Writer, program string, args ...string) error {
	r.calls = append(r.calls, strings.Join(append([]string{program}, args...), " "))
	if len(r.calls) > len(r.responses) {
		return fmt.Errorf("unexpected command: %s", r.calls[len(r.calls)-1])
	}
	_, err := io.WriteString(stdout, r.responses[len(r.calls)-1])
	return err
}

func TestReadVCPUPlacement(t *testing.T) {
	boot, err := os.ReadFile("testdata/list-v-xen-4.19.txt")
	if err != nil {
		t.Fatal(err)
	}
	inventory := string(boot)
	target := xen.Identity{ID: 9, UUID: "b586dd4f-32ba-478e-a5ca-9c2640c7ef54"}
	runner := &placementRunner{responses: []string{inventory, vcpuHeaderFixture + vcpuRowsFixture, inventory}}
	got, err := ReadVCPUs(t.Context(), runner, target)
	want := []xen.VCPU{
		{ID: 0, CPU: 4, State: "r--", Seconds: 5.4, Hard: "0-2,4", Soft: "4"},
		{ID: 1, CPU: 2, State: "-b-", Seconds: 2.1, Hard: "all", Soft: "0-2"},
		{ID: 2, CPU: -1, State: "--p", Hard: "all", Soft: "none"},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("placement = %+v, %v", got, err)
	}
	if !reflect.DeepEqual(runner.calls, []string{"xl list -v", "xl vcpu-list 9", "xl list -v"}) {
		t.Fatalf("unexpected host commands %v", runner.calls)
	}

	other := strings.ReplaceAll(inventory, target.UUID, "abababab-1111-4444-aaaa-012345678912")
	for name, observations := range map[string][]string{
		"reused before read":       {other},
		"reused during read":       {inventory, vcpuHeaderFixture + vcpuRowsFixture, other},
		"silently empty xl result": {inventory, vcpuHeaderFixture, inventory},
		"unexpected domain":        {inventory, vcpuHeaderFixture + strings.Replace(vcpuRowsFixture, "9     1", "6     1", 1), inventory},
		"duplicate vcpu":           {inventory, vcpuHeaderFixture + vcpuRowsFixture + vcpuRowsFixture, inventory},
		"malformed trailing row":   {inventory, vcpuHeaderFixture + vcpuRowsFixture + "worker  01 9 3", inventory},
		"invalid affinity":         {inventory, vcpuHeaderFixture + strings.Replace(vcpuRowsFixture, "0-2,4", "2-0,4", 1), inventory},
	} {
		t.Run(name, func(t *testing.T) {
			r := &placementRunner{responses: observations}
			vcpus, err := ReadVCPUs(t.Context(), r, target)
			if err == nil || len(vcpus) != 0 || len(r.calls) > len(observations) {
				t.Fatalf("got %+v, %v after %v", vcpus, err, r.calls)
			}
		})
	}

	t.Run("control domain", func(t *testing.T) {
		r := &placementRunner{responses: []string{inventory, vcpuHeaderFixture + "Domain-0  0  0  1  ---  0.0  all / all\n", inventory}}
		got, err := ReadVCPUs(t.Context(), r, xen.Identity{ID: 0, UUID: "00000000-0000-0000-0000-000000000000"})
		if err != nil || len(got) != 1 || got[0].CPU != 1 {
			t.Fatalf("control domain placement = %v, %v", got, err)
		}
	})
}
