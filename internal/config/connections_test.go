package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestMergeConnectionChangesAndRejectStaleEdits(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "xlui", "config.json")}
	east := Profile{Name: "east", Type: SSH, Address: "operator@east"}
	west := Profile{Name: "west", Type: XAPI, Address: "https://west", User: "viewer"}
	for _, p := range []Profile{east, west} {
		if _, err := s.Save(nil, p); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Save(nil, west); err == nil {
		t.Fatal("accepted duplicate")
	}
	renamed := east
	renamed.Name = "East rack"
	if _, err := s.Save(&east, renamed); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(&east, east); err == nil {
		t.Fatal("accepted stale edit")
	}
	got, err := s.Load()
	if err != nil || !slices.Equal(got, []Profile{renamed, west}) {
		t.Fatalf("loaded %v, %v", got, err)
	}
	info, err := os.Stat(s.Path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("expected private config file", err)
	}
}

func TestRejectsInvalidConnectionsWithoutReplacingConfig(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "config.json")}
	const invalid = `{"version":1,"connections":[{"name":"pool","type":"xapi","password":"test"}]}`
	if err := os.WriteFile(s.Path, []byte(invalid), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(nil, Profile{Name: "Local", Type: Local}); err == nil {
		t.Fatal("overwrote invalid data")
	}
	b, err := os.ReadFile(s.Path)
	if err != nil || string(b) != invalid {
		t.Fatalf("original configuration lost")
	}
	for _, p := range []Profile{
		{Name: "pool", Type: XAPI, Address: "https://user:password@host", User: "viewer"},
		{Name: "pool", Type: XAPI, Address: "http://host", User: "viewer"},
		{Name: "ssh", Type: SSH, Address: "-oProxyCommand=test"},
		{Name: "rack\n2", Type: Local},
	} {
		if p.Validate() == nil {
			t.Errorf("unexpected valid profile %q", p.Name)
		}
	}
}
