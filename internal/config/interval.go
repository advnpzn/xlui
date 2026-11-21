package config

import (
	"errors"
	"os"
	"path/filepath"
	"time"
)

const MinInterval = 100 * time.Millisecond

type preferences struct {
	Version         int    `json:"version"`
	DefaultInterval string `json:"default_interval,omitempty"`
}

// Older clients strictly decode config.json. Keep operator preferences in a
// sibling file so both generations can continue to manage the same profiles.
func (s Store) preferencesPath() string {
	return filepath.Join(filepath.Dir(s.Path), "preferences.json")
}

// LoadInterval returns the saved default for new sessions, or zero if unset.
func (s Store) LoadInterval() (time.Duration, error) {
	var prefs preferences
	err := readJSON(s.preferencesPath(), &prefs)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if prefs.Version != 1 {
		return 0, errors.New("unsupported xlui preferences version")
	}
	if prefs.DefaultInterval == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(prefs.DefaultInterval)
	if err != nil || (d != 0 && d < MinInterval) {
		return 0, errors.New("default_interval must be zero or a duration of at least 100ms")
	}
	return d, nil
}

// SaveInterval changes the default if it still matches the value the operator
// opened. A zero duration clears the override.
func (s Store) SaveInterval(before, after time.Duration) error {
	if after != 0 && after < MinInterval {
		return errors.New("default interval must be zero or at least 100ms")
	}
	path := s.preferencesPath()
	lock, err := lockConfig(path)
	if err != nil {
		return err
	}
	defer lock.Close()
	current, err := s.LoadInterval()
	if err != nil {
		return err
	}
	if current != before {
		return errors.New("default interval changed on disk; reopen settings")
	}
	prefs := preferences{Version: 1}
	if after != 0 {
		prefs.DefaultInterval = after.String()
	}
	return writeJSON(path, prefs)
}
