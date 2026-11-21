// Package session collects host observations independently of their presentation.
package session

import (
	"context"
	"errors"
	"time"

	"github.com/advnpzn/xlui/internal/xen"
)

// Snapshot retains the last successful observation and reports the latest read error.
// A zero ObservedAt means no read has succeeded. Sequence counts completed
// reads in this host session, including failures. Treat Domains as read-only.
type Snapshot struct {
	Sequence   uint64
	Domains    []xen.Domain
	ObservedAt time.Time
	Err        error
	cpu        map[domainInstance]cpuSample
}

// Reader returns a fresh slice that it will not subsequently modify.
type Reader func(context.Context) ([]xen.Domain, error)

// Inventory owns a single reader, sequential refresh timer and latest-frame channel.
type Inventory struct {
	updates chan Snapshot
	cancel  context.CancelFunc
	done    chan struct{}
}

func WatchInventory(ctx context.Context, interval time.Duration, read Reader) (*Inventory, error) {
	if interval <= 0 || read == nil {
		return nil, errors.New("inventory requires a positive refresh interval and a reader")
	}
	ctx, cancel := context.WithCancel(ctx)
	i := &Inventory{updates: make(chan Snapshot, 1), cancel: cancel, done: make(chan struct{})}
	go i.poll(ctx, interval, read)
	return i, nil
}

// Updates is a single-consumer stream. An unread frame may be replaced by a newer one.
func (i *Inventory) Updates() <-chan Snapshot { return i.updates }

// Close cancels any active read and waits for the stream to close.
func (i *Inventory) Close() {
	i.cancel()
	<-i.done
}

func (i *Inventory) poll(ctx context.Context, interval time.Duration, read Reader) {
	defer close(i.done)
	defer close(i.updates)
	timer := time.NewTimer(0)
	defer timer.Stop()
	var latest Snapshot
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		scan, cancel := context.WithTimeout(ctx, 30*time.Second)
		domains, err := read(scan)
		cancel()
		if ctx.Err() != nil {
			return
		}
		latest = latest.afterRead(domains, time.Now(), err)
		// Only poll publishes. A slow consumer still receives the last good data
		// if a failed refresh replaces its unread successful frame.
		select {
		case <-i.updates:
		default:
		}
		i.updates <- latest
		timer.Reset(interval)
	}
}
