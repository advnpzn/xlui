// Package xen defines the host observations shared by inventory consumers.
package xen

// Domain is a single live toolstack observation. CPUSeconds is cumulative
// processor time across vCPUs. ID is local to a host and can be reused.
type Domain struct {
	ControlDomain bool
	Name          string
	UUID          string
	State         string
	ID            int
	MemoryMiB     int64
	VCPUs         int
	CPUSeconds    float64  // -1 when no cumulative counter is available
	CPUPercent    *float64 // 100% is one fully occupied logical CPU
	GuestUsedKiB  *int64   // guest-reported usage when available
}

// Identity binds a numeric ID to this guest inside a single session.
type Identity struct {
	UUID string
	ID   int
}

func (d Domain) Identity() Identity { return Identity{d.UUID, d.ID} }
