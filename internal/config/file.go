package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
)

// ReadFile reads bounded configuration inputs, including private CA bundles.
// O_NONBLOCK lets us reject a FIFO without waiting for a writer.
func ReadFile(path string, limit int64) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("expected a regular configuration file")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("configuration input exceeds %d bytes", limit)
	}
	return data, err
}
