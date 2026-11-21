package tui

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/advnpzn/xlui/internal/config"
	"github.com/advnpzn/xlui/internal/session"
)

// OpenSource prepares a provider. Network I/O belongs in the cancellable reader.
type OpenSource func(config.Profile, string) (session.Source, error)

type liveSession struct {
	profile config.Profile
	view    *browser
	cancel  context.CancelFunc
}

type hostUpdate struct {
	host     *liveSession
	snapshot session.Snapshot
}

type workspace struct {
	ctx      context.Context
	cancel   context.CancelFunc
	workers  sync.WaitGroup
	initial  config.Profile
	store    config.Store
	interval time.Duration
	open     OpenSource
	live     map[config.Profile]*liveSession
	order    []config.Profile
	active   *liveSession
	updates  chan hostUpdate
	dialog   *connectionDialog
	quit     bool
}

func newWorkspace(parent context.Context, initial config.Profile, interval time.Duration, open OpenSource, store config.Store) *workspace {
	ctx, cancel := context.WithCancel(parent)
	return &workspace{ctx: ctx, cancel: cancel, initial: initial, interval: interval, open: open, store: store, live: make(map[config.Profile]*liveSession), updates: make(chan hostUpdate)}
}

func (w *workspace) connect(p config.Profile, password string) {
	if h := w.live[p]; h != nil {
		w.activate(h)
		return
	}
	if p.Type == config.XAPI && password == "" {
		w.login(p)
		return
	}
	source, err := w.open(p, password)
	if err != nil {
		w.manager(err.Error())
		return
	}
	ctx, cancel := context.WithCancel(w.ctx)
	inv, err := session.WatchInventory(ctx, w.interval, source.Read)
	if err != nil {
		cancel()
		if source.Close != nil {
			source.Close()
		}
		w.manager(err.Error())
		return
	}
	h := &liveSession{profile: p, view: newBrowser(p.Name), cancel: cancel}
	h.view.sessions = func() { w.manager("") }
	h.view.nextSession = w.next
	w.order = append(w.order, p)
	w.live[p] = h
	w.activate(h)
	w.workers.Add(1)
	go func() {
		defer w.workers.Done()
		if source.Close != nil {
			defer source.Close()
		}
		defer inv.Close()
		for sample := range inv.Updates() {
			select {
			case w.updates <- hostUpdate{h, sample}:
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (w *workspace) activate(h *liveSession) {
	w.active = h
	h.view.pointer = pointerState{}
	w.dialog = nil
}

func (w *workspace) next() {
	if len(w.order) > 1 && w.active != nil {
		i := slices.Index(w.order, w.active.profile)
		w.activate(w.live[w.order[(i+1)%len(w.order)]])
	}
}

func (w *workspace) disconnect(p config.Profile) {
	if h := w.live[p]; h != nil {
		w.order = slices.DeleteFunc(w.order, func(other config.Profile) bool { return other == p })
		delete(w.live, p)
		h.cancel()
		if w.active == h {
			w.active = nil
			if len(w.order) > 0 {
				w.active = w.live[w.order[0]]
			}
		}
	}
}

func (w *workspace) close() { w.cancel(); w.workers.Wait() }
