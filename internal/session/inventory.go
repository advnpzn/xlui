// Package session collects host observations independently of their presentation.
package session

import (
	"context"
	"errors"
	"sync/atomic"
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
	cpu        map[xen.Identity]cpuSample
}

// Reader returns a fresh slice that it will not subsequently modify.
type Reader func(context.Context) ([]xen.Domain, error)

// Inventory owns a single reader, sequential refresh timer and latest-frame channel.
type Inventory struct {
	updates    chan Snapshot
	interval   atomic.Int64
	reschedule chan struct{}
	cancel     context.CancelFunc
	done       chan struct{}
}

func WatchInventory(ctx context.Context, interval time.Duration, read Reader) (*Inventory, error) {
	if interval <= 0 || read == nil {
		return nil, errors.New("inventory requires a positive refresh interval and a reader")
	}
	ctx, cancel := context.WithCancel(ctx)
	i := &Inventory{
		updates:    make(chan Snapshot, 1),
		reschedule: make(chan struct{}, 1),
		cancel:     cancel,
		done:       make(chan struct{}),
	}
	i.interval.Store(int64(interval))
	go i.poll(ctx, read)
	return i, nil
}

// Updates is a single-consumer stream. An unread frame may be replaced by a newer one.
func (i *Inventory) Updates() <-chan Snapshot { return i.updates }

func (i *Inventory) Interval() time.Duration { return time.Duration(i.interval.Load()) }

// SetInterval wakes the sampler without interrupting a read or losing its CPU baseline.
// Rapid changes coalesce. Nonpositive intervals are ignored.
func (i *Inventory) SetInterval(interval time.Duration) {
	if interval <= 0 || i.interval.Swap(int64(interval)) == int64(interval) {
		return
	}
	select {
	case i.reschedule <- struct{}{}:
	default:
	}
}

// Close cancels any active read and waits for the stream to close.
func (i *Inventory) Close() {
	i.cancel()
	<-i.done
}

func (i *Inventory) poll(ctx context.Context, read Reader) {
	defer close(i.done)
	defer close(i.updates)
	timer := time.NewTimer(i.Interval())
	defer timer.Stop()
	var latest Snapshot
	for ctx.Err() == nil {
		started := time.Now()
		scan, cancel := context.WithTimeout(ctx, 30*time.Second)
		domains, err := read(scan)
		finished := time.Now()
		cancel()
		if ctx.Err() != nil {
			return
		}
		latest = latest.afterRead(domains, finished, err)
		// Only poll publishes. A slow consumer still receives the last good data
		// if a failed refresh replaces its unread successful frame.
		select {
		case <-i.updates:
		default:
		}
		i.updates <- latest
		// Anchor the cadence to the start of the read. Skip any slots the
		// provider was busy for, including when the interval changes mid-read.
		for {
			period := i.Interval()
			next := finished.Add(period - finished.Sub(started)%period)
			delay := time.Until(next)
			if delay <= 0 {
				break
			}
			timer.Reset(delay)
			select {
			case <-ctx.Done():
				return
			case <-i.reschedule:
			case <-timer.C:
			}
		}
	}
}

type Source struct {
	Read  Reader
	Close func()
}
