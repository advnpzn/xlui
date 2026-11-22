package config

import (
	"fmt"
	"slices"
)

// DomainOrder selects how a connection presents its domain inventory.
type DomainOrder uint8

const (
	OrderProvider DomainOrder = iota
	OrderName
	OrderCPU
	OrderAllocated
	OrderGuestUsed
)

var orders = [...]string{"provider", "name", "cpu", "allocated", "guest-used"}

func parseOrder(name string) (DomainOrder, error) {
	index := slices.Index(orders[:], name)
	if index < 0 {
		return OrderProvider, fmt.Errorf("invalid default_order %q", name)
	}
	return DomainOrder(index), nil
}
