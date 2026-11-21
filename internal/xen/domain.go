// Package xen defines the host observations shared by inventory consumers.
package xen

// Domain is a single live toolstack observation. CPUSeconds is cumulative
// processor time across vCPUs. ID is local to a host and can be reused.
type Domain struct {
	Name       string
	UUID       string
	State      string
	ID         int
	MemoryMiB  int64
	VCPUs      int
	CPUSeconds float64
}
