package worker

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/go-kit/log"
	"go.uber.org/atomic"
	"golang.org/x/sys/unix"
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

	logger log.Logger

	// Main context to control all goroutines.
	ctx context.Context
	wg  sync.WaitGroup

	// Cancel functions for individual goroutines.
	cancelsMu sync.Mutex
	cancels   []context.CancelFunc

	currentProcessors *atomic.Int32
}

func newProcessorManager(ctx context.Context, logger log.Logger, p processor, conn *grpc.ClientConn, address string) *processorManager {
	return &processorManager{
		p:                 p,
		ctx:               ctx,
		logger:            logger,
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
	pm.concurrency(0, 0)

	// Wait until they finish.
	pm.wg.Wait()

	_ = pm.conn.Close()
}

func (pm *processorManager) concurrency(n, m int) {
	pm.cancelsMu.Lock()
	defer pm.cancelsMu.Unlock()

	if n < 0 {
		n = 0
	}

	if m < 1 {
		m = runtime.NumCPU()
	}
	pm.logger.Log("msg", "max usable CPU cores", "count", m)

	count := 0
	for len(pm.cancels) < n {
		ctx, cancel := context.WithCancel(pm.ctx)
		pm.cancels = append(pm.cancels, cancel)

		pm.wg.Add(1)
		go func(i int) {
			defer pm.wg.Done()

			// "Bind" the worker goroutine to a specific CPU core:
			//
			// After a call to sched_setaffinity(), the set of CPUs on which the
			// thread will actually run is the intersection of the set specified
			// in the mask argument and the set of CPUs actually present on the
			// system.  The system may further restrict the set of CPUs on which
			// the thread runs if the "cpuset" mechanism described in cpuset(7)
			// is being used.  These restrictions on the actual set of CPUs on
			// which the thread will run are silently imposed by the kernel.
			//
			// https://man7.org/linux/man-pages/man2/sched_setaffinity.2.html
			cpu := new(unix.CPUSet)
			cpu.Set(i)
			unix.SchedSetaffinity(0, cpu)
			pm.logger.Log("msg", "set affinitiy for querier worker goroutine", "core", i, "cpuset", fmt.Sprintf("%v", cpu))

			pm.currentProcessors.Inc()
			defer pm.currentProcessors.Dec()

			pm.p.processQueriesOnSingleStream(ctx, pm.conn, pm.address)
		}(count % m)

		// pm.logger.Log("msg", "set affinitiy for worker goroutine", "core", count%m)

		count++
	}

	for len(pm.cancels) > n {
		pm.cancels[0]()
		pm.cancels = pm.cancels[1:]
	}
}
