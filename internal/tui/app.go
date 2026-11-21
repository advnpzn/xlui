// Package tui provides the interactive Xen dashboard.
package tui

import (
	"context"
	"time"

	"github.com/advnpzn/xlui/internal/config"
	"github.com/gdamore/tcell/v2"
)

func Run(ctx context.Context, initial config.Profile, interval time.Duration, layout Layout, password string, open OpenSource) error {
	store, err := config.UserStore()
	if err != nil {
		return err
	}
	screen, err := tcell.NewScreen()
	if err != nil {
		return err
	}
	if err = screen.Init(); err != nil {
		return err
	}
	defer screen.Fini()
	return runScreen(ctx, screen, initial, interval, layout, password, open, store)
}

func runScreen(ctx context.Context, screen tcell.Screen, initial config.Profile, interval time.Duration, layout Layout, password string, open OpenSource, store config.Store) error {
	w := newWorkspace(ctx, initial, interval, open, store)
	w.layout = layout
	defer w.close()
	w.connect(initial, password)
	screen.EnableMouse(tcell.MouseButtonEvents)
	defer screen.DisableMouse()
	events := make(chan tcell.Event)
	quitEvents := make(chan struct{})
	go screen.ChannelEvents(events, quitEvents)
	defer func() {
		close(quitEvents)
		for range events {
		}
	}()
	for !w.quit {
		if d := w.dialog; d != nil {
			d.draw(screen)
		} else if w.active != nil {
			w.active.view.openCount = len(w.live)
			w.active.view.draw(screen)
		}
		select {
		case <-ctx.Done():
			return nil
		case show := <-w.settingsDone:
			show()
		case result := <-w.dialogResults:
			w.applyDialogResult(result)
		case update := <-w.updates:
			w.applyUpdate(update)
		case ev, ok := <-events:
			if !ok {
				return nil
			}
			switch e := ev.(type) {
			case *tcell.EventKey:
				if e.Key() == tcell.KeyCtrlC {
					return nil
				}
				if d := w.dialog; d != nil {
					d.key(e)
				} else if w.active != nil {
					w.active.view.pointer = pointerState{}
					w.quit = w.active.view.key(e)
				}
			case *tcell.EventMouse:
				if d := w.dialog; d != nil {
					d.mouse(e)
				} else if w.active != nil {
					x, y := screen.Size()
					w.quit = w.active.view.mouse(e, x, y)
				}
			case *tcell.EventResize:
				if w.active != nil {
					w.active.view.pointer = pointerState{}
				}
				if w.dialog != nil {
					w.dialog.down = false
				}
				screen.Clear()
			case *tcell.EventError:
				return e
			}
		}
	}
	return nil
}
