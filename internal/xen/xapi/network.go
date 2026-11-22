package xapi

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/advnpzn/xlui/internal/xen"
)

// Bound a single detail request independently of the size of the pool.
const maxPoolVIFs = 64
const detailRecordLimit = 1 << 20

type vifRecord struct {
	UUID     string `json:"uuid"`
	VM       string `json:"VM"`
	Device   string `json:"device"`
	MAC      string `json:"MAC"`
	Network  string `json:"network"`
	Attached *bool  `json:"currently_attached"`
}

type poolNetwork struct {
	UUID string
	Name *string
}

// ReadNetwork walks only the selected VM's VIFs. XenAPI reads are individual
// observations, so recheck attachment, ownership and VM membership before returning.
func (m *Monitor) ReadNetwork(ctx context.Context, target xen.Identity) ([]xen.PoolVIF, error) {
	m.mu.Lock()
	session := m.session
	m.mu.Unlock()
	if !usableRef(session) {
		return nil, errors.New("pool session unavailable; refresh inventory")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	rpc := rpcClient{endpoint: m.rpc.endpoint, http: m.rpc.http}
	var vm string
	if err := rpc.call(ctx, "VM.get_by_uuid", []string{session, target.UUID}, &vm, sessionLimit); err != nil {
		return nil, err
	}
	if !usableRef(vm) {
		return nil, errors.New("selected VM is no longer available")
	}
	refs, err := rpc.vmVIFs(ctx, session, vm, target)
	if err != nil {
		return nil, err
	}
	records := make([]vifRecord, len(refs))
	rows := make([]xen.PoolVIF, 0, len(refs))
	networks := make(map[string]poolNetwork)
	seen := make(map[string]bool, len(refs))
	for i, ref := range refs {
		vif, err := rpc.readVIF(ctx, session, vm, ref)
		if err != nil {
			return nil, err
		}
		if seen[strings.ToLower(vif.UUID)] {
			return nil, errors.New("pool returned a duplicate VIF identity")
		}
		seen[strings.ToLower(vif.UUID)] = true
		records[i] = vif
		network, ok := networks[vif.Network]
		if !ok {
			// A full network record includes other VMs' VIF references.
			params := []string{session, vif.Network}
			if err := rpc.call(ctx, "network.get_uuid", params, &network.UUID, sessionLimit); err != nil {
				return nil, err
			}
			if err := rpc.call(ctx, "network.get_name_label", params, &network.Name, sessionLimit); err != nil {
				return nil, err
			}
			if !xen.GuestUUID(network.UUID) || network.Name == nil {
				return nil, errors.New("pool returned an incomplete network identity")
			}
			networks[vif.Network] = network
		}
		rows = append(rows, xen.PoolVIF{UUID: vif.UUID, Device: vif.Device, MAC: vif.MAC,
			NetworkUUID: network.UUID, NetworkName: *network.Name, Attached: *vif.Attached})
	}
	for i, ref := range refs {
		current, err := rpc.readVIF(ctx, session, vm, ref)
		if err != nil {
			return nil, err
		}
		before := records[i]
		if current.UUID != before.UUID || current.Device != before.Device || current.MAC != before.MAC ||
			current.Network != before.Network || *current.Attached != *before.Attached {
			return nil, errors.New("VM network interface changed; request a fresh sample")
		}
	}
	current, err := rpc.vmVIFs(ctx, session, vm, target)
	if err != nil {
		return nil, err
	}
	if !slices.Equal(refs, current) {
		return nil, errors.New("VM network configuration changed; request a fresh sample")
	}
	slices.SortFunc(rows, func(a, b xen.PoolVIF) int {
		ai, _ := strconv.ParseUint(a.Device, 10, 64)
		bi, _ := strconv.ParseUint(b.Device, 10, 64)
		return cmp.Compare(ai, bi)
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return rows, nil
}

func (rpc *rpcClient) vmVIFs(ctx context.Context, session, vm string, target xen.Identity) ([]string, error) {
	var record vmRecord
	if err := rpc.call(ctx, "VM.get_record", []string{session, vm}, &record, detailRecordLimit); err != nil {
		return nil, err
	}
	if record.UUID == "" || !strings.EqualFold(record.UUID, target.UUID) {
		return nil, errors.New("selected VM identity changed; refresh inventory")
	}
	switch record.PowerState {
	case "Running", "Paused":
		id, err := strconv.Atoi(record.DomainID.String())
		if err != nil || id < 0 || id != target.ID {
			return nil, errors.New("selected domain ID changed; refresh inventory")
		}
	case "Halted", "Suspended":
		if target.ID != -1 {
			return nil, errors.New("VM power state changed; refresh inventory")
		}
	default:
		return nil, errors.New("pool did not report a supported VM power state")
	}
	if record.VIFs == nil {
		return nil, errors.New("pool did not report the VM network configuration")
	}
	if len(record.VIFs) > maxPoolVIFs {
		return nil, fmt.Errorf("VM exceeds the inspector limit of %d interfaces", maxPoolVIFs)
	}
	slices.Sort(record.VIFs)
	for i, ref := range record.VIFs {
		if !usableRef(ref) || (i > 0 && ref == record.VIFs[i-1]) {
			return nil, errors.New("pool returned an invalid VIF reference set")
		}
	}
	return record.VIFs, nil
}

func (rpc *rpcClient) readVIF(ctx context.Context, session, vm, ref string) (vifRecord, error) {
	var vif vifRecord
	if err := rpc.call(ctx, "VIF.get_record", []string{session, ref}, &vif, detailRecordLimit); err != nil {
		return vifRecord{}, err
	}
	if vif.VM != vm {
		return vifRecord{}, errors.New("network interface no longer belongs to the selected VM")
	}
	_, deviceErr := strconv.ParseUint(vif.Device, 10, 64)
	mac, macErr := net.ParseMAC(vif.MAC)
	if deviceErr != nil || macErr != nil || len(mac) != 6 || !xen.GuestUUID(vif.UUID) || !usableRef(vif.Network) || vif.Attached == nil {
		return vifRecord{}, errors.New("pool returned an incomplete network interface")
	}
	return vif, nil
}
