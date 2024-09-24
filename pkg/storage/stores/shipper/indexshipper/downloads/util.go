package downloads

import (
	"context"
	"errors"
	"sync"
	"time"
)

// mtxWithReadiness combines a mutex with readiness channel. It would acquire lock only when the channel is closed to mark it ready.
type mtxWithReadiness struct {
	mtx   sync.RWMutex
	ready chan struct{}
}

func newMtxWithReadiness() *mtxWithReadiness {
	return &mtxWithReadiness{
		ready: make(chan struct{}),
	}
}

func (m *mtxWithReadiness) markReady() {
	close(m.ready)
}

func (m *mtxWithReadiness) awaitReady(ctx context.Context) error {
	ctx, cancel := context.WithTimeoutCause(ctx, 30*time.Second, errors.New("exceeded 30 seconds in awaitReady"))
	defer cancel()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-m.ready:
		return nil
	}
}

func (m *mtxWithReadiness) lock(ctx context.Context) error {
	err := m.awaitReady(ctx)
	if err != nil {
		return err
	}

	m.mtx.Lock()
	return nil
}

func (m *mtxWithReadiness) unlock() {
	m.mtx.Unlock()
}

func (m *mtxWithReadiness) rLock(ctx context.Context) error {
	err := m.awaitReady(ctx)
	if err != nil {
		return err
	}

	m.mtx.RLock()
	return nil
}

func (m *mtxWithReadiness) rUnlock() {
	m.mtx.RUnlock()
}
