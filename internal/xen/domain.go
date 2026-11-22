// Package xen defines the host observations shared by inventory consumers.
package xen

import "time"

// Domain is a single live toolstack observation. CPUSeconds is cumulative
// processor time across vCPUs. ID is local to a host and can be reused.
type Domain struct {
	ControlDomain bool
	Name          string
	UUID          string
	State         string
	StartedAt     time.Time // current boot, zero when unavailable
	ID            int
	MemoryMiB     int64
	VCPUs         int
	CPUSeconds    float64  // -1 when no cumulative counter is available
	CPUPercent    *float64 // 100% is one fully occupied logical CPU
	GuestUsedKiB  *int64   // guest-reported usage when available

	// ShutdownInstance binds a supported graceful shutdown to this observation.
	ShutdownInstance string
	// StartTarget binds a supported start to the observed halted VM.
	StartTarget string
}

// Identity binds a numeric ID to this guest inside a single session.
type Identity struct {
	UUID string
	ID   int
}

func (d Domain) Identity() Identity { return Identity{d.UUID, d.ID} }

// VCPU is a toolstack scheduling observation for one virtual CPU.
type VCPU struct {
	ID, CPU int // CPU is the observed physical CPU, or -1 when offline.
	State   string
	Seconds float64
	Hard    string // allowed physical CPUs
	Soft    string // preferred physical CPUs
}

// ResidentHost identifies the pool member currently hosting a VM.
type ResidentHost struct {
	Name string
	UUID string
}

// VIF is one network frontend reported by xl network-list.
type VIF struct {
	ID, BackendID int
	MAC           string
	// Xen reports -1 when a XenStore field is unavailable.
	State, EventChannel, TxRing, RxRing int
	BackendPath                         string
}

// PoolVIF describes a VM's configured XenAPI interface and its attachment state.
type PoolVIF struct {
	UUID, Device, MAC        string
	NetworkUUID, NetworkName string
	Attached                 bool
}
