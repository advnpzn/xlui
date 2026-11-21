// Package config stores the operator's named connections. Credentials stay with
// OpenSSH or in the running process.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
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
	data, err := ReadFile(s.Path, 1<<20)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cfg settings
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("read xlui configuration: %w", err)
	}
	if dec.Decode(new(any)) != io.EOF || cfg.Version != 1 {
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
	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(s.Path+".lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, fmt.Errorf("configuration busy: %w", err)
	}
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
	data, err := json.MarshalIndent(settings{1, profiles}, "", "  ")
	if err != nil {
		return nil, err
	}
	if len(data)+1 > 1<<20 {
		return nil, errors.New("configuration exceeds 1 MiB")
	}
	tmp, err := os.CreateTemp(dir, ".connections-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.Write(append(data, '\n'))
	if err == nil {
		err = tmp.Sync()
	}
	if err = errors.Join(err, tmp.Close()); err != nil {
		return nil, err
	}
	if err = os.Rename(tmp.Name(), s.Path); err != nil {
		return nil, err
	}
	return profiles, nil
}
