// Package tui provides the interactive domain browser.
package tui

import (
	"context"
	"time"

	"github.com/advnpzn/xlui/internal/session"
	"github.com/gdamore/tcell/v2"
)

// Run displays a read-only browser until the user quits or the context is cancelled.
// read must honor cancellation. It is called by a single background sampler.
func Run(ctx context.Context, hostLabel string, interval time.Duration, read session.Reader) error {
	screen, err := tcell.NewScreen()
	if err != nil {
		return err
	}
	if err := screen.Init(); err != nil {
		return err
	}
	defer screen.Fini()
	return runScreen(ctx, screen, hostLabel, interval, read)
}

func runScreen(ctx context.Context, screen tcell.Screen, host string, interval time.Duration, read session.Reader) error {
	inventory, err := session.WatchInventory(ctx, interval, read)
	if err != nil {
		return err
	}
	defer inventory.Close()

	events := make(chan tcell.Event)
	quitEvents := make(chan struct{})
	go screen.ChannelEvents(events, quitEvents)
	defer func() {
		close(quitEvents)
		for range events {
			// ChannelEvents closes events when it has stopped using the screen.
		}
	}()

	b := newBrowser(host)
	for {
		b.draw(screen)
		select {
		case <-ctx.Done():
			return nil
		case next, ok := <-inventory.Updates():
			if !ok {
				return nil
			}
			b.update(next)
		case event, ok := <-events:
			if !ok {
				return nil
			}
			switch event := event.(type) {
			case *tcell.EventKey:
				if b.key(event) {
					return nil
				}
			case *tcell.EventResize:
				screen.Clear()
			case *tcell.EventError:
				return event
			}
		}
	}
}
