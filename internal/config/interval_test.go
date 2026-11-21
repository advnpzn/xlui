package config

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestIntervalAndConnectionsMergeAcrossWriters(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "config.json")}
	const legacy = `{"version":1,"connections":[{"name":"board","type":"ssh","address":"arm-board"}]}`
	if err := os.WriteFile(s.Path, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := s.LoadInterval()
	if err != nil || before != 0 {
		t.Fatalf("legacy interval = %v, %v", before, err)
	}
	local := Profile{Name: "local", Type: Local}
	if _, err := s.Save(nil, local); err != nil {
		t.Fatal(err)
	}
	const interval = time.Second + 500*time.Microsecond
	if err := s.SaveInterval(before, interval); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveInterval(before, 2*time.Second); err == nil {
		t.Fatal("accepted stale interval")
	}
	updated := local
	updated.Name = "development"
	if _, err := s.Save(&local, updated); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadInterval()
	if err != nil || got != interval {
		t.Fatalf("interval after profile edit = %v, %v", got, err)
	}
	if err := s.SaveInterval(got, 0); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(s.Path)
	if err != nil || bytes.Contains(data, []byte("default_interval")) {
		t.Fatalf("clearing preference: %q, %v", data, err)
	}
	profiles, err := s.Load()
	want := []Profile{{Name: "board", Type: SSH, Address: "arm-board"}, updated}
	if err != nil || !slices.Equal(profiles, want) {
		t.Fatalf("profiles after interval edits = %v, %v", profiles, err)
	}
}

func TestInvalidIntervalPreservesConfiguration(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "config.json")}
	if got, err := s.LoadInterval(); got != 0 || err != nil {
		t.Fatalf("missing configuration = %v, %v", got, err)
	}
	for _, d := range []time.Duration{-time.Second, MinInterval - time.Nanosecond} {
		if err := s.SaveInterval(0, d); err == nil {
			t.Fatalf("accepted interval %v", d)
		}
	}
	if err := s.SaveInterval(0, MinInterval); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(s.Path)
	if err != nil || !bytes.Contains(data, []byte(`"default_interval": "100ms"`)) {
		t.Fatalf("expected human-readable duration: %q, %v", data, err)
	}
	for _, value := range []string{`"99ms"`, `"-1s"`, `"9223372036854775808ns"`, `1`, `"never"`} {
		original := `{"version":1,"default_interval":` + value + `}`
		if err := os.WriteFile(s.Path, []byte(original), 0600); err != nil {
			t.Fatal(err)
		}
		if err := s.SaveInterval(0, time.Second); err == nil {
			t.Errorf("replaced config containing %s", value)
		}
		data, err := os.ReadFile(s.Path)
		if err != nil || string(data) != original {
			t.Fatalf("configuration lost: %s, %v", value, err)
		}
	}
}
