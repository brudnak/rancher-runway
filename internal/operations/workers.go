// Package operations owns application workers and their shutdown lifecycle.
package operations

import (
	"context"
	"fmt"
	"sync"
)

// Workers tracks cancellable background work. Its zero value is ready to use.
// Registration and closing share a lock: shutdown cannot miss a worker that
// registers concurrently, and rejected workers never start side effects.
type Workers struct {
	mu      sync.Mutex
	ctx     context.Context
	cancel  context.CancelFunc
	closing bool
	active  int
	idle    chan struct{}
}

// Context returns the shared context cancelled by Stop.
func (w *Workers) Context() context.Context {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.initLocked()
	return w.ctx
}
func (w *Workers) initLocked() {
	if w.ctx == nil {
		w.ctx, w.cancel = context.WithCancel(context.Background())
		w.idle = make(chan struct{})
		close(w.idle)
	}
}

// Begin registers work before it starts. The caller must invoke the returned
// completion function after all final writes; repeated completion is harmless.
func (w *Workers) Begin() (context.Context, func(), error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.initLocked()
	if w.closing {
		return nil, nil, fmt.Errorf("Runway is shutting down; no new work can start")
	}
	if w.active == 0 {
		w.idle = make(chan struct{})
	}
	w.active++
	var once sync.Once
	return w.ctx, func() {
		once.Do(func() {
			w.mu.Lock()
			defer w.mu.Unlock()
			w.active--
			if w.active == 0 {
				close(w.idle)
			}
		})
	}, nil
}

// Start registers and launches a task, returning false after shutdown begins.
func (w *Workers) Start(task func(context.Context)) bool {
	ctx, done, err := w.Begin()
	if err != nil {
		return false
	}
	go func() { defer done(); task(ctx) }()
	return true
}

// Stop closes admission permanently and cancels every registered worker.
func (w *Workers) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.initLocked()
	w.closing = true
	w.cancel()
}

// Wait drains the current group of workers. Call Stop first when shutting down
// so no new registrations can arrive after the group becomes idle.
func (w *Workers) Wait(ctx context.Context) error {
	w.mu.Lock()
	w.initLocked()
	idle := w.idle
	w.mu.Unlock()
	select {
	case <-idle:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
