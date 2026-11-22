package config

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIntervalPreservesConnectionFile(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "config.json")}
	const legacy = `{"version":1,"connections":[{"name":"board","type":"ssh","address":"arm-board"}]}`
	if err := os.WriteFile(s.Path, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	const interval = time.Second + 500*time.Microsecond
	for _, step := range [][2]time.Duration{{0, interval}, {interval, 0}} {
		if err := s.SavePreferences(Preferences{Interval: step[0]}, Preferences{Interval: step[1]}); err != nil {
			t.Fatal(err)
		}
		got, err := s.LoadPreferences()
		if err != nil || got != (Preferences{Interval: step[1]}) {
			t.Fatalf("saved interval = %v, %v", got, err)
		}
		data, err := os.ReadFile(s.Path)
		if err != nil || string(data) != legacy {
			t.Fatalf("connection file changed: %q, %v", data, err)
		}
	}
}

func TestConcurrentIntervalSavesHaveOneWinner(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "config.json")}
	type result struct {
		interval time.Duration
		err      error
	}
	start := make(chan struct{})
	done := make(chan result, 2)
	for _, interval := range []time.Duration{time.Second, time.Minute} {
		go func() {
			<-start
			done <- result{interval, s.SavePreferences(Preferences{}, Preferences{Interval: interval})}
		}()
	}
	close(start)
	first, second := <-done, <-done
	if (first.err == nil) == (second.err == nil) {
		t.Fatalf("expected one successful edit, got %v and %v", first, second)
	}
	winner := first.interval
	if first.err != nil {
		winner = second.interval
	}
	if err := s.SavePreferences(Preferences{}, Preferences{Interval: 2 * time.Second}); err == nil {
		t.Fatal("accepted stale edit")
	}
	got, err := s.LoadPreferences()
	if err != nil || got != (Preferences{Interval: winner}) {
		t.Fatalf("stored interval = %v, %v, winner = %v", got, err, winner)
	}
}

func TestInvalidIntervalPreservesConfiguration(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "config.json")}
	if got, err := s.LoadPreferences(); got != (Preferences{}) || err != nil {
		t.Fatalf("missing configuration = %v, %v", got, err)
	}
	for _, d := range []time.Duration{-time.Second, MinInterval - time.Nanosecond} {
		if err := s.SavePreferences(Preferences{}, Preferences{Interval: d}); err == nil {
			t.Fatalf("accepted interval %v", d)
		}
	}
	if err := s.SavePreferences(Preferences{}, Preferences{Interval: MinInterval}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(s.preferencesPath())
	if err != nil || !bytes.Contains(data, []byte(`"default_interval": "100ms"`)) {
		t.Fatalf("expected human-readable duration: %q, %v", data, err)
	}
	for _, value := range []string{`"99ms"`, `"-1s"`, `"9223372036854775808ns"`, `1`, `"never"`} {
		original := `{"version":1,"default_interval":` + value + `}`
		if err := os.WriteFile(s.preferencesPath(), []byte(original), 0600); err != nil {
			t.Fatal(err)
		}
		if err := s.SavePreferences(Preferences{}, Preferences{Interval: time.Second}); err == nil {
			t.Errorf("replaced config containing %s", value)
		}
		data, err := os.ReadFile(s.preferencesPath())
		if err != nil || string(data) != original {
			t.Fatalf("configuration lost: %s, %v", value, err)
		}
	}
}

func TestLayoutPreferenceMigrationAndConflict(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "config.json")}
	if err := os.WriteFile(s.preferencesPath(), []byte(`{"version":1,"default_interval":"750ms"}`), 0600); err != nil {
		t.Fatal(err)
	}
	old, err := s.LoadPreferences()
	if err != nil || old != (Preferences{Interval: 750 * time.Millisecond, Layout: Overview}) {
		t.Fatalf("legacy preferences = %+v, %v", old, err)
	}
	saved := old
	saved.Layout = Split
	if err := s.SavePreferences(old, saved); err != nil {
		t.Fatal(err)
	}
	if err := s.SavePreferences(old, Preferences{Interval: time.Second}); err == nil {
		t.Fatal("stale interval edit overwrote a concurrent layout change")
	}
	if got, err := s.LoadPreferences(); err != nil || got != saved {
		t.Fatalf("persisted preferences = %+v, %v; want %+v", got, err, saved)
	}
}

func TestDomainOrderPreferences(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "config.json")}
	legacy := `{"version":1,"default_layout":"split","default_interval":"2s"}`
	if err := os.WriteFile(s.preferencesPath(), []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	before := Preferences{Layout: Split, Interval: 2 * time.Second, Order: OrderProvider}
	if got, err := s.LoadPreferences(); err != nil || got != before {
		t.Fatalf("legacy preferences = %+v, %v", got, err)
	}
	for _, order := range []DomainOrder{OrderName, OrderCPU, OrderAllocated, OrderGuestUsed, OrderProvider} {
		after := before
		after.Order = order
		if err := s.SavePreferences(before, after); err != nil {
			t.Fatal(err)
		}
		if err := s.SavePreferences(before, before); err == nil {
			t.Fatal("stale save overwrote a new domain order")
		}
		if got, err := s.LoadPreferences(); err != nil || got != after {
			t.Fatalf("stored preferences = %+v, %v; want %+v", got, err, after)
		}
		before = after
	}
	invalid := before
	invalid.Order = DomainOrder(255)
	if err := s.SavePreferences(before, invalid); err == nil {
		t.Fatal("accepted an invalid domain order")
	}
	raw := `{"version":1,"default_order":"invalid"}`
	if err := os.WriteFile(s.preferencesPath(), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadPreferences(); err == nil {
		t.Fatal("accepted an unrecognized saved order")
	}
}
