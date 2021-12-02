package worker

import (
	"context"
	"sync"
	"time"

	"go.uber.org/atomic"
	"google.golang.org/grpc"
)

const (
	notifyShutdownTimeout = 5 * time.Second
)

// Manages processor goroutines for single grpc connection.
type processorManager struct {
	p       processor
	conn    *grpc.ClientConn
	address string

	// Main context to control all goroutines.
	ctx context.Context
	wg  sync.WaitGroup

	// Cancel functions for individual goroutines.
	cancelsMu sync.Mutex
	cancels   []context.CancelFunc

	currentProcessors *atomic.Int32
}

func newProcessorManager(ctx context.Context, p processor, conn *grpc.ClientConn, address string) *processorManager {
	return &processorManager{
		p:                 p,
		ctx:               ctx,
		conn:              conn,
		address:           address,
		currentProcessors: atomic.NewInt32(0),
	}
}

func (pm *processorManager) stop() {
	// Notify the remote query-frontend or query-scheduler we're shutting down.
	// We use a new context to make sure it's not cancelled.
	notifyCtx, cancel := context.WithTimeout(context.Background(), notifyShutdownTimeout)
	defer cancel()
	pm.p.notifyShutdown(notifyCtx, pm.conn, pm.address)

	// Stop all goroutines.
	pm.concurrency(0)

	// Wait until they finish.
	pm.wg.Wait()

	_ = pm.conn.Close()
}

func (pm *processorManager) concurrency(n int) {
	pm.cancelsMu.Lock()
	defer pm.cancelsMu.Unlock()

	if n < 0 {
		n = 0
	}

	for len(pm.cancels) < n {
		ctx, cancel := context.WithCancel(pm.ctx)
		pm.cancels = append(pm.cancels, cancel)

		pm.wg.Add(1)
		go func() {
			defer pm.wg.Done()

			pm.currentProcessors.Inc()
			defer pm.currentProcessors.Dec()

			pm.p.processQueriesOnSingleStream(ctx, pm.conn, pm.address)
		}()
	}

	for len(pm.cancels) > n {
		pm.cancels[0]()
		pm.cancels = pm.cancels[1:]
	}
}
