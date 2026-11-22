package main

import (
	"context"
	"errors"
	"maps"
	"strings"
	"testing"

	"github.com/advnpzn/xlui/internal/session"
	"github.com/advnpzn/xlui/internal/xen"
)

func TestReadXEInterfaces(t *testing.T) {
	const vm = "11111111-1111-4111-8111-111111111111"
	const first = "22222222-2222-4222-8222-222222222222"
	const second = "33333333-3333-4333-8333-333333333333"
	const network = "44444444-4444-4444-8444-444444444444"
	const entry = "uuid ( RO)            : " + first + "\n" +
		"             vm-uuid ( RO): " + vm + "\n" +
		"              device ( RO): 0\n" +
		"                 MAC ( RO): 00:16:3e:00:00:01\n" +
		"  currently-attached ( RO): false\n" +
		"        network-uuid ( RO): " + network
	next := strings.NewReplacer(first, second, "device ( RO): 0", "device ( RO): 1", ":01\n", ":02\n", "false", "true").Replace(entry)
	gotVM, got, err := readXEInterfaces(strings.NewReader(entry + "\n\n\n" + next + "\n"))
	want := map[string]xen.PoolVIF{
		first:  {UUID: first, Device: "0", MAC: "00:16:3e:00:00:01", NetworkUUID: network},
		second: {UUID: second, Device: "1", MAC: "00:16:3e:00:00:02", NetworkUUID: network, Attached: true},
	}
	if err != nil || gotVM != vm || !maps.Equal(got, want) {
		t.Fatalf("readXEInterfaces returned %s, %+v, %v", gotVM, got, err)
	}

	for _, test := range []struct{ name, input string }{
		{"empty capture", "  \n"},
		{"wrong fields", strings.Replace(entry, "network-uuid", "network-name-label", 1)},
		{"missing attachment", strings.Replace(entry, "  currently-attached ( RO): false\n", "", 1)},
		{"invalid attachment", strings.Replace(entry, "false", "unknown", 1)},
		{"invalid identity", strings.Replace(entry, first, "no UUID", 1)},
		{"multiple owners", entry + "\n\n" + strings.Replace(next, vm, network, 1)},
		{"duplicate interface", entry + "\n\n" + entry},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := readXEInterfaces(strings.NewReader(test.input)); err == nil {
				t.Fatal("accepted unusable xe output")
			}
		})
	}
}

func TestComparePoolInterfaces(t *testing.T) {
	const vm = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	domain := xen.Domain{UUID: vm, ID: 7, State: "Running"}
	vif := xen.PoolVIF{UUID: "15870868-e244-473f-a493-640108d66f52", Device: "2",
		NetworkUUID: "c8b83f24-cfe5-4d5c-a05f-f698dfdc59a7", MAC: "00:16:3e:ab:cd:02", Attached: true}
	want := map[string]xen.PoolVIF{vif.UUID: vif}
	changed := vif
	changed.Attached = false
	for _, test := range []struct {
		name    string
		seen    []xen.PoolVIF
		readErr error
		ok      bool
	}{
		{"match", []xen.PoolVIF{vif}, nil, true},
		{"missing VIF", nil, nil, false},
		{"changed attachment", []xen.PoolVIF{changed}, nil, false},
		{"provider error", nil, errors.New("permission denied"), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := session.Source{
				Read: func(context.Context) ([]xen.Domain, error) { return []xen.Domain{domain}, nil },
				ReadPoolNetwork: func(_ context.Context, target xen.Identity) ([]xen.PoolVIF, error) {
					if target != domain.Identity() {
						t.Fatalf("read targeted %+v, want %+v", target, domain.Identity())
					}
					return test.seen, test.readErr
				},
			}
			if err := comparePoolInterfaces(t.Context(), source, vm, want); (err == nil) != test.ok {
				t.Fatalf("comparison error = %v", err)
			}
		})
	}
}
