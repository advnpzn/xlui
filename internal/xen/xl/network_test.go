package xl

import (
	"context"
	"io"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/advnpzn/xlui/internal/xen"
)

// Synthetic xl network-list output following RELEASE-4.20.0 tools/xl/xl_nic.c.
// libxl_device_nic_getinfo reports -1 when frontend xenstore fields are absent.
const networkHeading = "Idx BE Mac Addr.         handle state evt-ch   tx-/rx-ring-ref BE-path                       \n"
const connectedVIF = "2   0  00:16:3e:ab:cd:02      2     4      9    14/15          /local/domain/0/backend/vif/9/2 \n"
const pendingVIF = "3   1  00:16:3e:ab:cd:03      3    -1     -1    -1/-1          /local/domain/1/backend/vif/9/3 \n"

type networkDiagnosticRunner struct {
	placementRunner
	diagnostic string
}

func (r *networkDiagnosticRunner) Run(ctx context.Context, stdout, stderr io.Writer, program string, args ...string) error {
	if len(args) > 0 && args[0] == "network-list" {
		io.WriteString(stderr, r.diagnostic)
	}
	return r.placementRunner.Run(ctx, stdout, stderr, program, args...)
}

func TestReadNetworkInterfaces(t *testing.T) {
	data, err := os.ReadFile("testdata/list-v-xen-4.19.txt")
	if err != nil {
		t.Fatal(err)
	}
	inventory := string(data)
	target := xen.Identity{ID: 9, UUID: "b586dd4f-32ba-478e-a5ca-9c2640c7ef54"}
	changed := strings.Replace(inventory, target.UUID, "03a4cb3b-9239-46b0-8760-fec25d333bb9", 1)
	for _, tc := range []struct {
		name      string
		responses []string
		want      []xen.VIF
		reject    bool
	}{
		{"interfaces", []string{inventory, networkHeading + connectedVIF + pendingVIF, inventory}, []xen.VIF{
			{ID: 2, BackendID: 0, MAC: "00:16:3e:ab:cd:02", State: 4, EventChannel: 9, TxRing: 14, RxRing: 15, BackendPath: "/local/domain/0/backend/vif/9/2"},
			{ID: 3, BackendID: 1, MAC: "00:16:3e:ab:cd:03", State: -1, EventChannel: -1, TxRing: -1, RxRing: -1, BackendPath: "/local/domain/1/backend/vif/9/3"},
		}, false},
		{"no reported interfaces", []string{inventory, networkHeading, inventory}, nil, false},
		{"unrecognized output", []string{inventory, "unsupported network listing\n"}, nil, true},
		{"malformed second interface", []string{inventory, networkHeading + connectedVIF + "3 1 incomplete\n"}, nil, true},
		{"reused before lookup", []string{changed}, nil, true},
		{"reused while reading", []string{inventory, networkHeading + connectedVIF, changed}, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &placementRunner{responses: tc.responses}
			interfaces, err := ReadNetwork(t.Context(), r, target)
			if (err != nil) != tc.reject || !slices.Equal(interfaces, tc.want) {
				t.Fatalf("interfaces = %+v, error = %v", interfaces, err)
			}
			wantCalls := []string{"xl list -v", "xl network-list 9", "xl list -v"}[:len(tc.responses)]
			if !slices.Equal(r.calls, wantCalls) {
				t.Fatalf("calls = %v, want %v", r.calls, wantCalls)
			}
		})
	}

	for _, tc := range []struct {
		name       string
		diagnostic string
		reject     bool
	}{
		{"ssh warning", "Warning: Permanently added 'bench' to the list of known hosts.\n", false},
		{"libxl query error", "ssh banner\nlibxl: error: libxl_device.c: cannot read device list\n", true},
		{"libxl critical error", "libxl: critical: cannot access xenstore\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &networkDiagnosticRunner{
				placementRunner: placementRunner{responses: []string{inventory, networkHeading + connectedVIF, inventory}},
				diagnostic:      tc.diagnostic,
			}
			got, err := ReadNetwork(t.Context(), r, target)
			if tc.reject {
				if err == nil || len(got) != 0 {
					t.Fatalf("partial inventory after error: %+v, %v", got, err)
				}
			} else if err != nil || len(got) != 1 {
				t.Fatalf("SSH diagnostic hid a valid interface: %+v, %v", got, err)
			}
		})
	}
}
