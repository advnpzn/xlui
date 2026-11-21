package xl

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/advnpzn/xlui/internal/xen"
)

func TestGuestMemoryBoundToIdentityAndCompleteReadings(t *testing.T) {
	d := []xen.Domain{{ID: 1, UUID: "A"}, {ID: 2, UUID: "b"}, {ID: 3, UUID: "c"}, {ID: 4, UUID: "d"}, {ID: 5, UUID: "e"}}
	applyGuestMemory(d, "1 /vm/a 4096 1024\n2 /vm/previous 8000 0\n3 /vm/c 7 8\n4 /vm/d 2 -1\n5 /vm/e 4 1")
	if v := d[0].GuestUsedKiB; v == nil || *v != 3072 {
		t.Fatal("missing verified KiB value")
	}
	for _, dom := range d[1:] {
		if dom.GuestUsedKiB != nil {
			t.Fatalf("accepted invalid reading for %s", dom.UUID)
		}
	}
}

func TestMemoryProbeReadsIndependentGuests(t *testing.T) {
	dir := t.TempDir()
	// Model the three-value XenStore CLI contract and reject any other argument list.
	script := "#!/bin/sh\n" + `p=${1%/vm}
[ "$#" -eq 3 ] && [ "$2" = "$p/data/meminfo_total" ] && [ "$3" = "$p/data/meminfo_free" ] || exit 1
printf '%s\n' "/vm/guest-${p##*/}" 4096 1024
`
	if err := os.WriteFile(filepath.Join(dir, "xenstore-read"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", "-c", guestMemoryProbe, "xlui-memory", "1", "2")
	cmd.Env = []string{"PATH=" + dir}
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	d := []xen.Domain{{ID: 1, UUID: "guest-1"}, {ID: 2, UUID: "guest-2"}}
	applyGuestMemory(d, string(out))
	for _, guest := range d {
		if guest.GuestUsedKiB == nil || *guest.GuestUsedKiB != 3072 {
			t.Fatalf("missing independent reading: %q", out)
		}
	}
}
