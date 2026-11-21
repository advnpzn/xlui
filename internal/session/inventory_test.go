package session

import (
	"context"
	"errors"
	"testing"
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
