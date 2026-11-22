package config

import (
	"errors"
	"os"
	"path/filepath"
	"time"
)

const MinInterval = 100 * time.Millisecond

type Preferences struct {
	Interval time.Duration
	Layout   Layout
}

type preferencesFile struct {
	DefaultLayout   string `json:"default_layout,omitempty"`
	Version         int    `json:"version"`
	DefaultInterval string `json:"default_interval,omitempty"`
}

// Older clients strictly decode config.json. Keep operator preferences in a
// sibling file so both generations can continue to manage the same profiles.
func (s Store) preferencesPath() string {
	return filepath.Join(filepath.Dir(s.Path), "preferences.json")
}

// LoadPreferences also accepts the original interval-only preferences file.
func (s Store) LoadPreferences() (Preferences, error) {
	var file preferencesFile
	err := readJSON(s.preferencesPath(), &file)
	if errors.Is(err, os.ErrNotExist) {
		return Preferences{}, nil
	}
	if err != nil {
		return Preferences{}, err
	}
	if file.Version != 1 {
		return Preferences{}, errors.New("unsupported xlui preferences version")
	}
	var prefs Preferences
	if file.DefaultInterval != "" {
		prefs.Interval, err = time.ParseDuration(file.DefaultInterval)
		if err != nil || (prefs.Interval != 0 && prefs.Interval < MinInterval) {
			return Preferences{}, errors.New("default_interval must be zero or a duration of at least 100ms")
		}
	}
	if file.DefaultLayout != "" {
		prefs.Layout, err = ParseLayout(file.DefaultLayout)
		if err != nil {
			return Preferences{}, err
		}
	}
	return prefs, nil
}

// SavePreferences compares and replaces the settings together under one lock.
func (s Store) SavePreferences(before, after Preferences) error {
	if after.Interval != 0 && after.Interval < MinInterval {
		return errors.New("default interval must be zero or at least 100ms")
	}
	if int(after.Layout) >= len(layouts) {
		return errors.New("unsupported default layout")
	}
	path := s.preferencesPath()
	lock, err := lockConfig(path)
	if err != nil {
		return err
	}
	defer lock.Close()
	current, err := s.LoadPreferences()
	if err != nil {
		return err
	}
	if current != before {
		return errors.New("default settings changed on disk; reopen settings")
	}
	file := preferencesFile{Version: 1}
	if after.Interval != 0 {
		file.DefaultInterval = after.Interval.String()
	}
	if after.Layout != Overview {
		file.DefaultLayout = layouts[after.Layout]
	}
	return writeJSON(path, file)
}
