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
		if err := s.SaveInterval(step[0], step[1]); err != nil {
			t.Fatal(err)
		}
		got, err := s.LoadInterval()
		if err != nil || got != step[1] {
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
			done <- result{interval, s.SaveInterval(0, interval)}
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
	if err := s.SaveInterval(0, 2*time.Second); err == nil {
		t.Fatal("accepted stale edit")
	}
	got, err := s.LoadInterval()
	if err != nil || got != winner {
		t.Fatalf("stored interval = %v, %v, winner = %v", got, err, winner)
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
	data, err := os.ReadFile(s.preferencesPath())
	if err != nil || !bytes.Contains(data, []byte(`"default_interval": "100ms"`)) {
		t.Fatalf("expected human-readable duration: %q, %v", data, err)
	}
	for _, value := range []string{`"99ms"`, `"-1s"`, `"9223372036854775808ns"`, `1`, `"never"`} {
		original := `{"version":1,"default_interval":` + value + `}`
		if err := os.WriteFile(s.preferencesPath(), []byte(original), 0600); err != nil {
			t.Fatal(err)
		}
		if err := s.SaveInterval(0, time.Second); err == nil {
			t.Errorf("replaced config containing %s", value)
		}
		data, err := os.ReadFile(s.preferencesPath())
		if err != nil || string(data) != original {
			t.Fatalf("configuration lost: %s, %v", value, err)
		}
	}
}
