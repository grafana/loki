package downloads

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
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

func (m *mtxWithReadiness) isReady() bool {
	select {
	case <-m.ready:
		return true
	default:
		return false
	}
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
	start := time.Now()
	err := m.awaitReady(ctx)
	if err != nil {
		stats.FromContext(ctx).AddIndexLockWaitTime(time.Since(start))
		return err
	}

	m.mtx.Lock()
	stats.FromContext(ctx).AddIndexLockWaitTime(time.Since(start))
	return nil
}

func (m *mtxWithReadiness) unlock() {
	m.mtx.Unlock()
}

func (m *mtxWithReadiness) rLock(ctx context.Context) error {
	start := time.Now()
	err := m.awaitReady(ctx)
	if err != nil {
		stats.FromContext(ctx).AddIndexLockWaitTime(time.Since(start))
		return err
	}

	m.mtx.RLock()
	stats.FromContext(ctx).AddIndexLockWaitTime(time.Since(start))
	return nil
}

func (m *mtxWithReadiness) rUnlock() {
	m.mtx.RUnlock()
}
