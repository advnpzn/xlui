// Package config stores the operator's named connections and preferences. Credentials stay with
// OpenSSH or in the running process.
package config

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/advnpzn/xlui/internal/transport"
	"github.com/advnpzn/xlui/internal/xen/xapi"
)

const (
	Local = "local"
	SSH   = "ssh"
	XAPI  = "xapi"
)

type Profile struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Address string `json:"address,omitempty"`
	User    string `json:"user,omitempty"`
	CAFile  string `json:"ca_file,omitempty"`
}

func (p Profile) Validate() error {
	if strings.TrimSpace(p.Name) == "" || len(p.Name) > 80 || strings.TrimSpace(p.Name) != p.Name {
		return errors.New("enter a name of 1–80 bytes without leading or trailing spaces")
	}
	for _, s := range []string{p.Name, p.Address, p.User, p.CAFile} {
		if len(s) > 4096 || strings.IndexFunc(s, func(r rune) bool { return !unicode.IsPrint(r) }) >= 0 {
			return errors.New("connection fields must contain printable text")
		}
	}
	switch p.Type {
	case Local:
		if p.Address != "" || p.User != "" || p.CAFile != "" {
			return errors.New("local connections use only a name")
		}
	case SSH:
		if p.Address == "" || p.User != "" || p.CAFile != "" {
			return errors.New("enter an SSH alias or user@host as the destination")
		}
		_, err := transport.NewRunner(p.Address)
		return err
	case XAPI:
		if strings.TrimSpace(p.User) == "" {
			return errors.New("enter a XenAPI username")
		}
		_, err := xapi.ParseEndpoint(p.Address)
		return err
	default:
		return errors.New("unknown connection type")
	}
	return nil
}

type Store struct{ Path string }
type settings struct {
	Version     int       `json:"version"`
	Connections []Profile `json:"connections"`
}

func UserStore() (Store, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return Store{}, err
	}
	return Store{filepath.Join(dir, "xlui", "config.json")}, nil
}

func (s Store) Load() ([]Profile, error) {
	var cfg settings
	err := readJSON(s.Path, &cfg)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if cfg.Version != 1 {
		return nil, errors.New("invalid or unsupported xlui configuration")
	}
	seen := map[string]bool{}
	for _, p := range cfg.Connections {
		if err := p.Validate(); err != nil {
			return nil, err
		}
		if seen[p.Name] {
			return nil, errors.New("duplicate connection name in configuration")
		}
		seen[p.Name] = true
	}
	return cfg.Connections, nil
}

// Save merges one edit under a lock so two xlui processes cannot lose each
// other's changes. A stale edit is rejected before replacing the file.
func (s Store) Save(before *Profile, after Profile) ([]Profile, error) {
	if err := after.Validate(); err != nil {
		return nil, err
	}
	lock, err := lockConfig(s.Path)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	profiles, err := s.Load()
	if err != nil {
		return nil, err
	}
	index := -1
	if before != nil {
		index = slices.Index(profiles, *before)
		if index < 0 {
			return nil, errors.New("connection changed on disk; reopen the connection manager")
		}
	}
	for i, p := range profiles {
		if i != index && p.Name == after.Name {
			return nil, errors.New("connection name already exists")
		}
	}
	if index < 0 {
		profiles = append(profiles, after)
	} else {
		profiles[index] = after
	}
	if err := writeJSON(s.Path, settings{1, profiles}); err != nil {
		return nil, err
	}
	return profiles, nil
}
