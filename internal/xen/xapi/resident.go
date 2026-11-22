package xapi

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/advnpzn/xlui/internal/xen"
)

// ReadResidentHost looks up placement through the configured pool coordinator.
// A VM can migrate without changing its domid, so check resident_on again
// after resolving the host reference. A sample is valid only for this read.
func (m *Monitor) ReadResidentHost(ctx context.Context, target xen.Identity) (xen.ResidentHost, error) {
	m.mu.Lock()
	session := m.session
	m.mu.Unlock()
	if !usableRef(session) {
		return xen.ResidentHost{}, errors.New("pool session unavailable; refresh inventory")
	}
	rpc := rpcClient{endpoint: m.rpc.endpoint, http: m.rpc.http}
	var vm string
	if err := rpc.call(ctx, "VM.get_by_uuid", []string{session, target.UUID}, &vm, sessionLimit); err != nil {
		return xen.ResidentHost{}, err
	}
	if !usableRef(vm) {
		return xen.ResidentHost{}, errors.New("selected VM is no longer available")
	}
	resident := func() (string, error) {
		var record vmRecord
		if err := rpc.call(ctx, "VM.get_record", []string{session, vm}, &record, inventoryLimit); err != nil {
			return "", err
		}
		if !strings.EqualFold(record.UUID, target.UUID) || record.UUID == "" {
			return "", errors.New("selected VM identity changed; refresh inventory")
		}
		if record.PowerState != "Running" && record.PowerState != "Paused" {
			return "", errors.New("VM has no running or paused domain on a resident host")
		}
		id, err := strconv.Atoi(record.DomainID.String())
		if err != nil || id < 0 || id != target.ID {
			return "", errors.New("selected domain ID changed; refresh inventory")
		}
		if !usableRef(record.Resident) {
			return "", errors.New("pool did not report a resident host for this VM")
		}
		return record.Resident, nil
	}
	ref, err := resident()
	if err != nil {
		return xen.ResidentHost{}, err
	}
	var host struct {
		Name *string `json:"name_label"`
		UUID string  `json:"uuid"`
	}
	if err := rpc.call(ctx, "host.get_record", []string{session, ref}, &host, inventoryLimit); err != nil {
		return xen.ResidentHost{}, err
	}
	if host.Name == nil || !xen.GuestUUID(host.UUID) {
		return xen.ResidentHost{}, errors.New("pool returned an incomplete host identity")
	}
	current, err := resident()
	if err != nil {
		return xen.ResidentHost{}, err
	}
	if current != ref {
		return xen.ResidentHost{}, errors.New("VM moved between hosts; request a fresh sample")
	}
	return xen.ResidentHost{Name: *host.Name, UUID: host.UUID}, nil
}
