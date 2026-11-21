package session

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/advnpzn/xlui/internal/xen"
)

func TestSlowConsumerKeepsTheLastGoodSnapshot(t *testing.T) {
	offline := errors.New("offline")
	third := make(chan struct{})
	reads := 0
	inventory, err := WatchInventory(context.Background(), time.Millisecond, func(ctx context.Context) ([]xen.Domain, error) {
		reads++
		switch reads {
		case 1:
			return []xen.Domain{{Name: "bench"}}, nil
		case 2:
			return nil, offline
		default:
			close(third)
			<-ctx.Done()
			return nil, ctx.Err()
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer inventory.Close()
	select {
	case <-third: // The failed read has replaced the first, still unread frame.
	case <-time.After(3 * time.Second):
		t.Fatal("sampler blocked on the unread frame")
	}
	s := <-inventory.Updates()
	if !errors.Is(s.Err, offline) || s.ObservedAt.IsZero() || len(s.Domains) != 1 || s.Domains[0].Name != "bench" {
		t.Fatalf("lost last good observation on failure: %+v", s)
	}
}

func TestCPULoadFromCounters(t *testing.T) {
	now := time.Now()
	base := xen.Domain{UUID: "a", ID: 1, CPUSeconds: 10, VCPUs: 4}
	previous := Snapshot{}.afterRead([]xen.Domain{base}, now, nil)
	for _, tc := range []struct {
		name   string
		change func(*xen.Domain)
		failed bool
		want   float64
	}{
		{"two busy vCPUs", func(d *xen.Domain) { d.CPUSeconds = 14 }, false, 200},
		{"idle", func(d *xen.Domain) {}, false, 0},
		{"reset", func(d *xen.Domain) { d.CPUSeconds = 1 }, false, -1},
		{"reused ID", func(d *xen.Domain) { d.UUID = "b" }, false, -1},
		{"vCPU hotplug", func(d *xen.Domain) { d.VCPUs++ }, false, -1},
		{"collection gap", func(d *xen.Domain) {}, true, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := []xen.Domain{base}
			tc.change(&d[0])
			p := previous
			if tc.failed {
				p = p.afterRead(nil, now.Add(time.Second), errors.New("offline"))
			}
			sample := p.afterRead(d, now.Add(2*time.Second), nil)
			got, valid := sample.CPUPercent(d[0])
			if tc.want < 0 {
				if valid {
					t.Fatal("unexpected measurement")
				}
			} else if !valid || got != tc.want {
				t.Fatalf("want %v, got %v", tc.want, got)
			}
		})
	}
}

func TestLiveIntervalReschedulesSameReader(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		starts := make(chan time.Duration, 1)
		inv, err := WatchInventory(t.Context(), time.Second, func(context.Context) ([]xen.Domain, error) {
			starts <- time.Since(start)
			time.Sleep(350 * time.Millisecond)
			return []xen.Domain{{ID: 1, UUID: "vm", VCPUs: 1, CPUSeconds: time.Since(start).Seconds()}}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		defer inv.Close()
		if got := <-starts; got != 0 {
			t.Fatalf("first read at %v", got)
		}
		<-inv.Updates()
		inv.SetInterval(2 * time.Second)
		if got := <-starts; got != 2*time.Second {
			t.Fatalf("lengthened interval started at %v", got)
		}

		// Change the interval twice during collection. The final value should
		// apply without starting another read or resetting CPU measurements.
		inv.SetInterval(time.Minute)
		inv.SetInterval(100 * time.Millisecond)
		s := <-inv.Updates()
		if _, ok := s.CPUPercent(s.Domains[0]); !ok || s.Sequence != 2 {
			t.Fatal("lost baseline across interval change")
		}
		if got := <-starts; got != 2400*time.Millisecond {
			t.Fatalf("slow read caused drift or a catch-up poll at %v", got)
		}
		inv.SetInterval(time.Hour)
		<-inv.Updates()
		time.Sleep(time.Second)
		inv.SetInterval(200 * time.Millisecond)
		if got := <-starts; got != 3750*time.Millisecond {
			t.Fatalf("overdue poll after shortening fired at %v", got)
		}
	})
}
