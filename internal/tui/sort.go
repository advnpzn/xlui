package tui

import (
	"cmp"
	"slices"

	"github.com/advnpzn/xlui/internal/xen"
)

type domainOrder uint8

const (
	orderProvider domainOrder = iota
	orderName
	orderCPU
	orderAllocated
	orderGuestUsed
)

var orderNames = [...]string{"Provider", "Name asc", "CPU desc", "Allocated desc", "Guest used desc"}

// Sort the projection only. Provider data may also be in use by other views.
func (r *domainRows) sortVisible() {
	if r.order == orderProvider {
		return
	}
	slices.SortFunc(r.visible, func(i, j int) int {
		a, b := r.Domains[i], r.Domains[j]
		var byMetric int
		switch r.order {
		case orderCPU:
			byMetric = cmp.Compare(r.cpuSortValue(b), r.cpuSortValue(a))
		case orderAllocated:
			byMetric = cmp.Compare(b.MemoryMiB, a.MemoryMiB)
		case orderGuestUsed:
			byMetric = cmp.Compare(guestUsedSortValue(b), guestUsedSortValue(a))
		}
		if byMetric != 0 {
			return byMetric
		}
		return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.UUID, b.UUID), cmp.Compare(a.ID, b.ID))
	})
}

// Resource values are nonnegative. Missing readings sort below a measured zero.
func (r *domainRows) cpuSortValue(d xen.Domain) float64 {
	if pct, ok := r.CPUPercent(d); ok {
		return pct
	}
	return -1
}

func guestUsedSortValue(d xen.Domain) int64 {
	if d.GuestUsedKiB != nil {
		return *d.GuestUsedKiB
	}
	return -1
}
