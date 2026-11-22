package config

import (
	"fmt"
	"slices"
)

// Layout is the preferred arrangement of the domain dashboard.
type Layout uint8

const (
	Overview Layout = iota
	Inventory
	Split
)

var layouts = [...]string{"overview", "inventory", "split"}

func ParseLayout(name string) (Layout, error) {
	index := slices.Index(layouts[:], name)
	if index < 0 {
		return Overview, fmt.Errorf("invalid layout %q; choose overview, inventory or split", name)
	}
	return Layout(index), nil
}
