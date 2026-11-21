package config

import (
	"errors"
	"time"
)

const MinInterval = 100 * time.Millisecond

// savedInterval keeps durations readable in config.json. Zero uses the
// application's default and is omitted on save.
type savedInterval time.Duration

func (i savedInterval) MarshalText() ([]byte, error) {
	return []byte(time.Duration(i).String()), nil
}

func (i *savedInterval) UnmarshalText(text []byte) error {
	if len(text) == 0 {
		*i = 0
		return nil
	}
	d, err := time.ParseDuration(string(text))
	if err != nil || (d != 0 && d < MinInterval) {
		return errors.New("default_interval must be zero or a duration of at least 100ms")
	}
	*i = savedInterval(d)
	return nil
}

// LoadInterval returns the saved default for new sessions, or zero if unset.
func (s Store) LoadInterval() (time.Duration, error) {
	cfg, err := s.load()
	if err != nil {
		return 0, err
	}
	return time.Duration(cfg.DefaultInterval), nil
}

// SaveInterval changes the default if it still matches the value the operator
// opened. Connection edits from other xlui processes are retained.
func (s Store) SaveInterval(before, after time.Duration) error {
	if after != 0 && after < MinInterval {
		return errors.New("default interval must be zero or at least 100ms")
	}
	_, err := s.update(func(cfg *settings) error {
		if time.Duration(cfg.DefaultInterval) != before {
			return errors.New("default interval changed on disk; reopen settings")
		}
		cfg.DefaultInterval = savedInterval(after)
		return nil
	})
	return err
}
