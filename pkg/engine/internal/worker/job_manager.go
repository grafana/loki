package worker

import (
	"context"
	"errors"
	"sync"

	"github.com/oklog/ulid/v2"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

// errNoReadyThreads is returned by [jobManager.Send] when there are no ready
// threads waiting to receive a task.
var errNoReadyThreads = errors.New("no ready threads")

// threadJob is an individual task to run.
type threadJob struct {
	Context context.Context
	Cancel  context.CancelFunc

	// interrupted reports whether the scheduler has explicitly requested
	// cancellation of this job via a TaskCancelMessage. It is used by the
	// running thread to distinguish a scheduler-requested cancellation from
	// an ambient context cancellation (e.g., worker shutdown).
	//
	// Only interrupted jobs should be reported as canceled; any other context
	// cancellation should be treated as a failure.
	interrupted atomic.Bool

	Scheduler *wire.Peer     // Scheduler which owns the task.
	Task      *workflow.Task // Task to execute.

	Sources map[ulid.ULID]*streamSource // Sources to read task data from.
	Sinks   map[ulid.ULID]*streamSink   // Sinks to write task data to.

	Close func() // Close function to clean up resources for the job.
}

// jobManager is the bridge between the worker's communication to a scheduler
// and worker threads.
//
//   - Worker threads call [jobManager.Recv] when they are ready for a new job.
//
//   - The worker calls [jobManager.Send] when it is assigned a task from the
//     scheduler, which fails if there are no ready threads.
//
//   - The worker's readiness emitter calls [jobManager.WaitReadyGen] to learn
//     when threads become ready, so it can advertise spare capacity to a
//     scheduler once per batch of newly-ready threads.
type jobManager struct {
	mut             sync.RWMutex
	waiting         int
	readyGeneration uint64        // Monotonic count of threads becoming ready; bumped on every Recv.
	waitCond        chan struct{} // Closed and recreated on every change to waiting.
	cancelCond      chan struct{} // Channel-based condition variable to cancel Recv

	jobCh chan *threadJob
}

// newJobManager returns a new jobManager.
func newJobManager() *jobManager {
	return &jobManager{
		waitCond:   make(chan struct{}),
		cancelCond: make(chan struct{}),

		jobCh: make(chan *threadJob),
	}
}

// Recv retrieves the next job to execute, blocking until a job is sent or
// ctx is done. Jobs are sent by a call to [jobManager.Send].
//
// Recv returns an error if ctx completes before a job is sent.
func (jm *jobManager) Recv(ctx context.Context) (*threadJob, error) {
	// Notify all waiters that there's a call to Recv.
	jm.broadcast()

	select {
	case <-ctx.Done():
		// The caller gave up on waiting for a job. Before we return, we want to
		// inform any waiting Send calls that they should try again, otherwise
		// they would be blocked forever.
		jm.cancelWaiting()

		return nil, ctx.Err()
	case job := <-jm.jobCh:
		return job, nil
	}
}

// broadcast records that another thread is waiting for a job and wakes any
// blocked calls to WaitReady / WaitBusy.
func (jm *jobManager) broadcast() {
	jm.mut.Lock()
	defer jm.mut.Unlock()

	jm.waiting++
	jm.readyGeneration++
	jm.notify()
}

// notify wakes all goroutines blocked on a change to waiting (WaitReadyGen).
// Callers must hold jm.mut for writing.
func (jm *jobManager) notify() {
	close(jm.waitCond) // Wakes all blocked calls to WaitReadyGen.
	jm.waitCond = make(chan struct{})
}

// cancelWaiting wakes all blocked calls to Send.
func (jm *jobManager) cancelWaiting() {
	jm.mut.Lock()
	defer jm.mut.Unlock()

	if jm.waiting == 0 {
		panic("jobManager.cancelWaiting called with no waiting calls")
	}
	jm.waiting--
	jm.notify()

	close(jm.cancelCond)
	jm.cancelCond = make(chan struct{})
}

// Send delivers a job to a waiting call to [jobManager.Recv]. If there are no
// waiting calls to Recv, Send returns an error.
func (jm *jobManager) Send(ctx context.Context, job *threadJob) error {
	jm.mut.Lock()
	defer jm.mut.Unlock()

	var sent bool

	for ctx.Err() == nil && !sent && jm.waiting != 0 {
		// Get the current Send cancellation condition variable channel.
		//
		// cancelCh will be closed if *any* waiting call to Recv is cancelled.
		// This may cause spurious wake ups of calls to Send, but since
		// cancelling Recv is rare (only on shutdown), it should have minimal
		// overhead.
		cancelCh := jm.cancelCond

		// Unlock the mutex while waiting to make progress.
		jm.mut.Unlock()

		select {
		case <-ctx.Done():
		case <-cancelCh:
		case jm.jobCh <- job:
			sent = true
		}

		// Re-lock the mutex so the condition can be safely checked again on the
		// next loop.
		jm.mut.Lock()
	}

	switch {
	case sent:
		jm.waiting--
		jm.notify()
		return nil
	case !sent && ctx.Err() != nil:
		return ctx.Err()
	default:
		return errNoReadyThreads
	}
}

// WaitReady blocks until a thread has become ready since generation `last`,
// returning the new generation. It lets the readiness emitter advertise spare
// capacity to the scheduler as a batch, regardless of when the threads became ready.
func (jm *jobManager) WaitReady(ctx context.Context, lastGeneration uint64) (uint64, error) {
	jm.mut.RLock()
	defer jm.mut.RUnlock()

	for ctx.Err() == nil && jm.readyGeneration == lastGeneration {
		waitCh := jm.waitCond

		jm.mut.RUnlock()
		select {
		case <-ctx.Done():
		case <-waitCh:
		}
		jm.mut.RLock()
	}

	return jm.readyGeneration, ctx.Err()
}
