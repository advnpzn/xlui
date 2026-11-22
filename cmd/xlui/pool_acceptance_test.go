package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/advnpzn/xlui/internal/config"
	"github.com/advnpzn/xlui/internal/session"
	"github.com/advnpzn/xlui/internal/xen"
)

var xeCapture = flag.String("xe-vifs", "", "xe vif-list capture from the selected pool VM")

func TestLivePoolNetwork(t *testing.T) {
	if *xeCapture == "" {
		t.Skip("supply -xe-vifs to compare with an authorized live pool")
	}
	file, err := os.Open(*xeCapture)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	vm, want, err := readXEInterfaces(file)
	if err != nil {
		t.Fatal(err)
	}
	p := config.Profile{Type: config.XAPI, Address: os.Getenv("XLUI_XAPI_ENDPOINT"), User: os.Getenv("XLUI_XAPI_USER"), CAFile: os.Getenv("XLUI_XAPI_CA")}
	password := os.Getenv("XLUI_XAPI_PASSWORD")
	if p.Address == "" || p.User == "" || password == "" {
		t.Fatal("set XLUI_XAPI_ENDPOINT, XLUI_XAPI_USER and XLUI_XAPI_PASSWORD")
	}
	source, err := openMonitor(p, password)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	if err := comparePoolInterfaces(ctx, source, vm, want); err != nil {
		t.Fatal(err)
	}
	t.Logf("matched %d interface(s) from the xe capture", len(want))
}

func comparePoolInterfaces(ctx context.Context, source session.Source, vm string, want map[string]xen.PoolVIF) error {
	domains, err := source.Read(ctx)
	if err != nil {
		return err
	}
	for _, domain := range domains {
		if !strings.EqualFold(domain.UUID, vm) {
			continue
		}
		rows, err := source.ReadPoolNetwork(ctx, domain.Identity())
		if err != nil {
			return err
		}
		if len(rows) != len(want) {
			return fmt.Errorf("interface count: xe=%d xlui=%d", len(want), len(rows))
		}
		seen := make(map[string]bool, len(rows))
		for _, row := range rows {
			id := strings.ToLower(row.UUID)
			vif, ok := want[id]
			if !ok || seen[id] {
				return errors.New("xlui returned an unexpected or duplicate VIF")
			}
			seen[id] = true
			if row.Device != vif.Device || !strings.EqualFold(row.MAC, vif.MAC) ||
				!strings.EqualFold(row.NetworkUUID, vif.NetworkUUID) || row.Attached != vif.Attached {
				return fmt.Errorf("device, MAC, network or attachment differs for VIF %s", vif.UUID)
			}
		}
		return nil
	}
	return errors.New("captured VM is missing from xlui inventory")
}

// Each xe record is a blank-line separated set of "field ( RO): value" lines.
func readXEInterfaces(r io.Reader) (string, map[string]xen.PoolVIF, error) {
	rows := make(map[string]xen.PoolVIF)
	var vm string
	fields := make(map[string]string)
	finish := func() error {
		if len(fields) == 0 {
			return nil
		}
		id := strings.ToLower(fields["uuid"])
		owner := strings.ToLower(fields["vm-uuid"])
		_, repeated := rows[id]
		if !xen.GuestUUID(id) || repeated || !xen.GuestUUID(owner) || (vm != "" && owner != vm) || !xen.GuestUUID(fields["network-uuid"]) {
			return errors.New("invalid, duplicate or mixed-VM identities in xe capture")
		}
		_, deviceErr := strconv.ParseUint(fields["device"], 10, 64)
		mac, macErr := net.ParseMAC(fields["MAC"])
		attached := fields["currently-attached"]
		if deviceErr != nil || macErr != nil || len(mac) != 6 || (attached != "true" && attached != "false") {
			return errors.New("invalid device, MAC or attachment in xe capture")
		}
		vm = owner
		rows[id] = xen.PoolVIF{UUID: id, NetworkUUID: fields["network-uuid"], Device: fields["device"], MAC: mac.String(), Attached: attached == "true"}
		clear(fields)
		return nil
	}
	s := bufio.NewScanner(r)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			if err := finish(); err != nil {
				return "", nil, err
			}
			continue
		}
		label, value, ok := strings.Cut(line, ":")
		key, _, _ := strings.Cut(label, " ")
		if _, duplicate := fields[key]; !ok || duplicate {
			return "", nil, errors.New("malformed or duplicate xe field")
		}
		switch key {
		case "uuid", "vm-uuid", "network-uuid", "device", "MAC", "currently-attached":
			fields[key] = strings.TrimSpace(value)
		default:
			return "", nil, errors.New("unexpected xe field; use the documented params list")
		}
	}
	if err := s.Err(); err != nil {
		return "", nil, err
	}
	if err := finish(); err != nil {
		return "", nil, err
	}
	if len(rows) == 0 {
		return "", nil, errors.New("select a VM with a configured VIF")
	}
	return vm, rows, nil
}
