package tui

import (
	"cmp"
	"slices"

	"github.com/advnpzn/xlui/internal/config"
	"github.com/advnpzn/xlui/internal/xen"
)

var orderNames = [...]string{"Provider", "Name asc", "CPU desc", "Allocated desc", "Guest used desc"}

func (b *browser) setOrder(order config.DomainOrder) {
	if b.rows.order == order {
		return
	}
	selected, _ := b.selected()
	b.rows.order = order
	b.project(selected, false)
}

// Sort the projection only. Provider data may also be in use by other views.
func (r *domainRows) sortVisible() {
	if r.order == config.OrderProvider {
		return
	}
	slices.SortFunc(r.visible, func(i, j int) int {
		a, b := r.Domains[i], r.Domains[j]
		var byMetric int
		switch r.order {
		case config.OrderCPU:
			byMetric = cmp.Compare(r.cpuSortValue(b), r.cpuSortValue(a))
		case config.OrderAllocated:
			byMetric = cmp.Compare(b.MemoryMiB, a.MemoryMiB)
		case config.OrderGuestUsed:
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
