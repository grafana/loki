// Package scheduler provides an implementation of [workflow.Runner] that works
// by scheduling tasks to be executed by a set of workers.
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/http"
	"sync"
	"time"

	gotrace "runtime/trace"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/oklog/ulid/v2"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/propagation"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/engine/internal/metrictimer"
	"github.com/grafana/loki/v3/pkg/engine/internal/obslock"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/queue/fair"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
	"github.com/grafana/loki/v3/pkg/xcap"
)

// Config holds configuration options for [Scheduler].
type Config struct {
	// Logger for optional log messages.
	Logger log.Logger

	// Listener is the listener used for communication with workers.
	Listener wire.Listener
}

// Scheduler is a service that can schedule tasks to connected worker instances.
type Scheduler struct {
	logger    log.Logger
	metrics   *metrics
	collector *collector

	initOnce sync.Once
	svc      services.Service

	listener wire.Listener

	// Current set of connections, used for collecting metrics.
	connections sync.Map // map[*workerConn]struct{}

	resourcesMut obslock.RWMutex
	streams      map[ulid.ULID]*stream // All known streams (regardless of state)
	tasks        map[ulid.ULID]*task   // All known tasks (regardless of state)

	assignMut        obslock.RWMutex
	taskQueue        fair.Queue[*task]
	connectedWorkers map[*workerConn]struct{}

	assignSema chan struct{}       // assignSema signals that task assignment is ready.
	tasksCh    chan taskAssignment // channel for sending task assignments to worker routines.

	wireMetrics *wire.Metrics
}

// taskAssignment holds a popped task ready for assignment to a worker.
type taskAssignment struct {
	t   *task
	pos fair.Position
	msg wire.TaskAssignMessage
}

var _ workflow.Runner = (*Scheduler)(nil)

// New creates a new instance of a scheduler. Use [Scheduler.Service] to manage
// the lifecycle of the returned scheduler.
func New(config Config) (*Scheduler, error) {
	if config.Logger == nil {
		config.Logger = log.NewNopLogger()
	}

	if config.Listener == nil {
		return nil, errors.New("listener must be provided")
	}

	s := &Scheduler{
		logger: config.Logger,

		listener: config.Listener,

		streams: make(map[ulid.ULID]*stream),
		tasks:   make(map[ulid.ULID]*task),

		connectedWorkers: make(map[*workerConn]struct{}),

		assignSema: make(chan struct{}, 1),
		tasksCh:    make(chan taskAssignment),

		wireMetrics: wire.NewMetrics(),
	}

	s.metrics = newMetrics()
	s.resourcesMut.Init("resourcesMut", s.metrics.lock)
	s.assignMut.Init("assignMut", s.metrics.lock)
	s.collector = newCollector(s)

	return s, nil
}

// Service returns the service used to manage the lifecycle of the Scheduler.
func (s *Scheduler) Service() services.Service {
	s.initOnce.Do(func() {
		s.svc = services.NewBasicService(nil, s.run, nil)
	})

	return s.svc
}

func (s *Scheduler) run(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error { return s.collector.Process(ctx) })
	g.Go(func() error { return s.runAcceptLoop(ctx) })
	g.Go(func() error { return s.runAssignLoop(ctx) })

	return g.Wait()
}

func (s *Scheduler) runAcceptLoop(ctx context.Context) error {
	for {
		conn, err := s.listener.Accept(ctx)
		if err != nil && ctx.Err() != nil {
			return nil
		} else if err != nil {
			level.Warn(s.logger).Log("msg", "failed to accept connection", "err", err)
			continue
		}

		go s.handleConn(ctx, conn)
	}
}

func (s *Scheduler) handleConn(ctx context.Context, conn wire.Conn) {
	logger := log.With(s.logger, "remote_addr", conn.RemoteAddr())
	level.Info(logger).Log("msg", "handling connection")

	wc := &workerConn{
		done: make(chan struct{}),
		wake: make(chan struct{}, 1),
	}

	s.connections.Store(wc, struct{}{})
	defer s.connections.Delete(wc)

	s.metrics.connsTotal.Inc()

	peer := &wire.Peer{
		Logger:  logger,
		Metrics: s.wireMetrics,
		Conn:    conn,

		// Allow for a backlog of 128 frames before backpressure is applied.
		Buffer: 128,

		Handler: func(ctx context.Context, _ *wire.Peer, msg wire.Message) error {
			return s.handleMessage(ctx, wc, msg)
		},
	}

	wc.Peer = peer

	// Handle communication with the peer until the context is canceled or some
	// error occurs.
	err := peer.Serve(ctx)
	if ctx.Err() != nil || errors.Is(err, wire.ErrConnClosed) {
		level.Debug(logger).Log("msg", "connection closed")
	} else if err != nil {
		level.Warn(logger).Log("msg", "serve error", "err", err)
	}

	// Signal any worker routines associated with this connection to exit.
	close(wc.done)

	// If our peer exited, we need to make sure we clean up any tasks still
	// assigned to it by aborting them.
	s.removeWorker(ctx, wc, err)
}

func (s *Scheduler) handleMessage(ctx context.Context, worker *workerConn, msg wire.Message) (err error) {
	switch msg := msg.(type) {
	case wire.StreamDataMessage:
		return s.handleStreamData(ctx, worker, msg)
	case wire.WorkerHelloMessage:
		return s.handleWorkerHello(ctx, worker, msg)
	case wire.WorkerReadyMessage:
		return s.markWorkerReady(ctx, worker)
	case wire.TaskStatusMessage:
		return s.handleTaskStatus(ctx, worker, msg)
	case wire.StreamStatusMessage:
		return s.handleStreamStatus(ctx, worker, msg)
	default:
		phases := s.metrics.startHandler(msg.Kind())
		defer func() { phases.Done(handlerOutcome(err)) }()
		return fmt.Errorf("unsupported message kind %q", msg.Kind())
	}
}

func (s *Scheduler) handleStreamData(ctx context.Context, worker *workerConn, msg wire.StreamDataMessage) (err error) {
	phases := s.metrics.startHandler(msg.Kind())
	defer func() { phases.Done(handlerOutcome(err)) }()

	if err := worker.MarkDataPlane(); err != nil {
		return err
	}

	resourcesGuard := s.resourcesMut.RLock("handle_stream_data")
	defer resourcesGuard.RUnlock()

	registered, found := s.streams[msg.StreamID]
	if !found {
		return fmt.Errorf("stream %d not found", msg.StreamID)
	} else if registered.localReceiver == nil {
		return fmt.Errorf("scheduler is not listening for data for stream %s", msg.StreamID)
	}

	return phases.Region("downstream_write", func() error {
		return registered.localReceiver.Write(ctx, msg.Data)
	})
}

func (s *Scheduler) handleWorkerHello(ctx context.Context, worker *workerConn, msg wire.WorkerHelloMessage) (err error) {
	phases := s.metrics.startHandler(msg.Kind())
	defer func() { phases.Done(handlerOutcome(err)) }()

	if err := worker.HandleHello(msg); err != nil {
		return err
	}

	// Request to be notified when the worker is ready.
	s.workerSubscribe(ctx, worker)
	return nil
}

func (s *Scheduler) markWorkerReady(ctx context.Context, worker *workerConn) (err error) {
	phases := s.metrics.startHandler(wire.WorkerReadyMessage{}.Kind())
	defer func() { phases.Done(handlerOutcome(err)) }()

	hop := s.metrics.startAssignmentHop("worker_ready_handler")
	defer func() { hop.Done(handlerOutcome(err)) }()

	assignGuard := s.assignMut.Lock("mark_worker_ready")
	defer assignGuard.Unlock()

	if err := worker.MarkReady(); err != nil {
		return err
	}

	if _, exists := s.connectedWorkers[worker]; exists {
		nudgeSemaphore(worker.wake)
	} else {
		s.connectedWorkers[worker] = struct{}{}
		go s.workerLoop(ctx, worker)
	}

	// Wake [Scheduler.runAssignLoop] so it feeds tasks into tasksCh.
	if s.taskQueue.Len() > 0 {
		nudgeSemaphore(s.assignSema)
	}
	return nil
}

// nudgeSemaphore wakes a goroutine listening on the channel sema. sema must be
// a buffered channel.
//
// If a write is already buffered for sema, nudgeSemaphore exits immediately.
func nudgeSemaphore(sema chan struct{}) {
	select {
	case sema <- struct{}{}:
	default:
	}
}

func (s *Scheduler) handleTaskStatus(ctx context.Context, worker *workerConn, msg wire.TaskStatusMessage) (err error) {
	phases := s.metrics.startHandler(msg.Kind())
	defer func() { phases.Done(handlerOutcome(err)) }()

	if got, want := worker.Type(), connectionTypeControlPlane; got != want {
		return fmt.Errorf("worker connection must be in state %q, got %q", want, got)
	}

	var n notifier
	defer n.Notify(ctx)

	// A read lock is sufficient here: handleTaskStatus only reads the
	// resourcesMut-protected maps (s.tasks) and task.owner (which is only ever
	// written under the full write lock, in finalizeAssignment). All mutations
	// it performs are to per-task state (guarded by task.mut via SetState /
	// RecordTerminalObservations) or per-worker state (guarded by worker.mut via
	// Unassign), and status messages for a given task are serialized on a single
	// connection's handler goroutine.
	//
	// Task status messages are the highest-frequency scheduler event; taking the
	// write lock here would serialize every status update against dispatch
	// (finalizeAssignment) and cancellation (Cancel), throttling the scheduler
	// under load. Using a read lock lets status processing run concurrently.
	resourcesGuard := s.resourcesMut.RLock("handle_task_status")
	defer resourcesGuard.RUnlock()

	task, found := s.tasks[msg.ID]
	if !found {
		return fmt.Errorf("task %s not found", msg.ID)
	}

	changed, err := task.SetState(s.metrics, msg.Status)
	if err != nil {
		return err
	} else if changed {
		newState := msg.Status.State
		if owner := task.owner.Load(); owner != nil && newState.Terminal() {
			owner.Unassign(task)
		}

		switch newState {
		case workflow.TaskStateCompleted:
			task.wfRegion.Record(StatExecutedTasks.Observe(1))

			if assignTime := task.AssignTime(); !assignTime.IsZero() {
				// The execution time of the task is the duration from when it
				// was first assigned to when we received the completion status.
				//
				// We skip the observation when assignTime is zero, which can
				// happen when a task completes before we can process the
				// assignment. If we didn't skip these, we'd record an
				// observation of the maximum time.Duration value (290 years).
				s.metrics.taskExecSeconds.Observe(time.Since(assignTime).Seconds())
			}
		case workflow.TaskStateCancelled:
			// The worker has confirmed cancellation of a previously assigned
			// task from [Scheduler.Cancel].
			task.wfRegion.Record(StatCanceledAssignedTasks.Observe(1))
		case workflow.TaskStateFailed:
			task.wfRegion.Record(StatFailedTasks.Observe(1))
		}

		// Terminal states may include a capture from the worker; if so, we want
		// to roll these up into our own per-task capture to give the event
		// handler full visibility into the task's execution.
		notifyStatus := msg.Status
		if newState.Terminal() {
			task.RecordTerminalObservations(time.Now())
			task.capture.Merge(nil, notifyStatus.Capture)
			notifyStatus.Capture = task.capture
		}

		// Notify the handler about the change.
		n.AddTaskEvent(taskNotification{
			Handler:   task.handler,
			Task:      task.inner,
			NewStatus: notifyStatus,
		})
	}
	return nil
}

func (s *Scheduler) handleStreamStatus(ctx context.Context, worker *workerConn, msg wire.StreamStatusMessage) (err error) {
	phases := s.metrics.startHandler(msg.Kind())
	defer func() { phases.Done(handlerOutcome(err)) }()

	if got, want := worker.Type(), connectionTypeControlPlane; got != want {
		return fmt.Errorf("worker connection must be in state %q, got %q", want, got)
	}

	var n notifier
	defer n.Notify(ctx)

	// A read lock is sufficient: stream state transitions are guarded by the
	// per-stream stateMut (via changeStreamState -> setState), and we only read
	// the resourcesMut-protected maps (s.streams, s.tasks) and task.owner (only
	// ever written under the full write lock). Stream-status messages are the
	// highest-frequency scheduler event during fan-in teardown; holding the
	// write lock here serializes them against dispatch and cancellation and
	// backs up the per-connection message goroutines. Using a read lock lets
	// them run concurrently.
	resourcesGuard := s.resourcesMut.RLock("handle_stream_status")
	defer resourcesGuard.RUnlock()

	stream, found := s.streams[msg.StreamID]
	if !found {
		return fmt.Errorf("stream %s not found", msg.StreamID)
	}
	return s.changeStreamState(&n, stream, msg.State)
}

// changeStreamState updates the state of the target stream. changeStreamState
// must be called while the resourcesMut lock is held.
func (s *Scheduler) changeStreamState(n *notifier, target *stream, newState workflow.StreamState) error {
	changed, err := target.setState(s.metrics, newState)
	if err != nil {
		return err
	} else if !changed {
		return nil
	}

	// If we have a receiver, inform them about the change. This is a
	// best-effort message; we buffer it on the notifier so it is sent after the
	// caller releases resourcesMut rather than blocking the lock on a full
	// connection buffer.
	receiver, found := s.tasks[target.taskReceiver]
	if found {
		if owner := receiver.owner.Load(); owner != nil {
			n.AddMessage(owner, wire.StreamStatusMessage{
				StreamID: target.inner.ULID,
				State:    newState,
			})
		}
	}

	// Inform the owner about the change.
	n.AddStreamEvent(streamNotification{
		Handler:  target.handler,
		Stream:   target.inner,
		NewState: newState,
	})
	return nil
}

// removeWorker immediately cleans up state for a worker:
//
// - Aborts all tasks assigned to the given worker.
// - Removes the worker from the ready list.
//
// If reason is non-nil, tasks that were running normally on the worker are
// marked as failed with the provided reason.
//
// If reason is nil, all tasks are marked as cancelled. Tasks which were pending
// cancellation will be marked as Canceled regardless of reason.
func (s *Scheduler) removeWorker(ctx context.Context, worker *workerConn, reason error) {
	var n notifier
	defer n.Notify(ctx)

	resourcesGuard := s.resourcesMut.Lock("remove_worker")
	defer resourcesGuard.Unlock()

	assignGuard := s.assignMut.Lock("remove_worker")
	defer assignGuard.Unlock()

	for _, task := range worker.Assigned() {
		wasInterrupted := task.Interrupted()
		worker.Unassign(task)

		// Even though the worker disconnected we still have some stats about
		// the task that the handler may be interested in.
		newStatus := workflow.TaskStatus{Capture: task.capture}
		switch {
		case wasInterrupted:
			// Cancellation had already been requested for this task. Honor that
			// intent regardless of whether the worker disconnected cleanly or
			// with an error.
			newStatus.State = workflow.TaskStateCancelled
		case reason != nil:
			newStatus.State = workflow.TaskStateFailed
			newStatus.Error = reason
		default:
			newStatus.State = workflow.TaskStateCancelled
		}

		if changed, _ := task.SetState(s.metrics, newStatus); !changed {
			continue
		}

		switch newStatus.State {
		case workflow.TaskStateCancelled:
			if wasInterrupted {
				// The worker disconnected before confirming a cancellation we
				// had requested. Record it against the assigned-cancellation
				// stat to match the disposition that [Scheduler.Cancel] would
				// have produced had the worker survived long enough to confirm.
				task.wfRegion.Record(StatCanceledAssignedTasks.Observe(1))
			}
			// Otherwise the worker disconnected cleanly with no pending
			// cancellation request; we have no specific stat for that category
			// today.
		case workflow.TaskStateFailed:
			task.wfRegion.Record(StatFailedTasks.Observe(1))
		}

		task.RecordTerminalObservations(time.Now())

		// We only need to inform the handler about the change. There's nothing
		// to send to the owner of the task since worker has disconnected.
		n.AddTaskEvent(taskNotification{
			Handler:   task.handler,
			Task:      task.inner,
			NewStatus: newStatus,
		})
	}

	// Remove the worker from the ready list, if it exists.
	delete(s.connectedWorkers, worker)
}

func (s *Scheduler) runAssignLoop(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-s.assignSema:
			s.assignTasks(ctx)
		}
	}
}

func (s *Scheduler) assignTasks(ctx context.Context) {
	level.Debug(s.logger).Log("msg", "performing task assignment")

	for ctx.Err() == nil {
		prepare := s.metrics.startAssignmentHop("prepare_assignment")
		assignment, ok := s.prepareAssignment()
		prepareOutcome := outcomeSuccess
		if !ok {
			prepareOutcome = outcomeEmpty
		}
		prepare.Done(prepareOutcome)
		if !ok {
			return
		}

		// Send the task to a waiting worker goroutine. This blocks until a
		// worker is available, providing natural backpressure.
		handoff := s.metrics.startAssignmentHop("tasks_ch_handoff_wait")
		select {
		case <-ctx.Done():
			handoff.Done(outcomeCanceled)
			return
		case s.tasksCh <- assignment:
			handoff.Done(outcomeSuccess)
		}
	}
}

// TODO(ashwanth): When there is only a single worker (say single-binary)
// all assignments go through a single workerLoop. Because SendMessage blocks
// until the worker ACKs, the per-task round-trip overhead adds up
// when there are a large number of tasks, resulting in tail latencies.
func (s *Scheduler) workerLoop(ctx context.Context, worker *workerConn) {
	for {
		var assignment taskAssignment

		waitAssignment := s.metrics.startAssignmentHop("wait_assignment")
		select {
		case <-ctx.Done():
			waitAssignment.Done(outcomeCanceled)
			return
		case <-worker.done:
			waitAssignment.Done(outcomeConnClosed)
			return
		case assignment = <-s.tasksCh:
			waitAssignment.Done(outcomeAssigned)
		}

		level.Debug(s.logger).Log("msg", "assigning task", "id", assignment.msg.Task.ULID, "conn", worker.RemoteAddr())

		// TryAssign holds the task's mut for the duration of the send so a
		// fast-responding worker can't race with the post-assignment
		// bookkeeping in finalizeAssignment. On success it also records the
		// assignment timestamp on the task before releasing mut.
		roundtrip := s.metrics.startAssignmentHop("taskassign_roundtrip")
		err := assignment.t.TryAssign(func() error {
			// TODO(rfratto): allow assignment timeout to be configurable.
			sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()

			return worker.SendMessage(sendCtx, assignment.msg)
		})
		attemptOutcome := assignmentAttemptOutcome(err)
		roundtrip.Done(attemptOutcome)
		s.metrics.incAssignmentAttempt(attemptOutcome)

		if err != nil && errors.Is(err, errUnassignable) {
			// Task is already in a terminal state or has been interrupted,
			// so we can skip the send and move on to the next task.
			continue
		} else if err != nil {
			// Generic error: restore the task to its original queue position.
			requeue := s.metrics.startAssignmentHop("error_requeue")
			s.requeueTask(assignment.t, assignment.pos)
			requeue.Done(attemptOutcome)

			if isTooManyRequestsError(err) {
				s.metrics.backoffsTotal.Inc()
				if resume := s.parkWorker(ctx, worker); !resume {
					return
				}
				continue
			}

			level.Warn(s.logger).Log("msg", "failed to assign task", "id", assignment.msg.Task.ULID, "conn", worker.RemoteAddr(), "err", err)
			// other errors are treated as transient, continue with next assignment.
			// if the worker disconnected, the loop will exit on worker.done.
			continue
		}

		gotrace.Log(assignment.t.runtimeTraceCtx, "task_assigned", assignment.t.inner.ULID.String()+" -> "+worker.RemoteAddr().String())

		s.finalizeAssignment(ctx, assignment.t, worker, assignment.msg.StreamStates)
	}
}

// parkRetryInterval bounds how long a parked workerLoop waits before
// re-attempting dispatch even without an explicit wake-up. See parkWorker.
const parkRetryInterval = 100 * time.Millisecond

// parkWorker blocks until the worker advertises freed capacity (a ready message
// nudges worker.wake), the worker disconnects, or the scheduler shuts down. It
// returns true if the assignment loop should resume, or false if it should
// exit.
//
// A parked workerLoop is normally resumed by a WorkerReady (worker.wake) sent
// when a worker thread frees. That readiness signal is edge-triggered and
// demand-based (the worker only re-advertises after rejecting an assignment
// with 429), which makes it possible to lose an edge: a parked workerLoop makes
// no further dispatch attempts, so it can never trigger a new 429 to re-arm the
// worker's readiness — leaving a worker with idle threads stranded and its tasks
// undispatched. To guarantee liveness we also wake up periodically and
// re-attempt dispatch; if the worker is genuinely full it simply returns 429 and
// re-parks, and if it has freed capacity the assignment succeeds.
func (s *Scheduler) parkWorker(ctx context.Context, worker *workerConn) bool {
	timer := time.NewTimer(parkRetryInterval)
	defer timer.Stop()

	hop := s.metrics.startAssignmentHop("park_worker_wait")

	select {
	case <-ctx.Done():
		hop.Done(outcomeCanceled)
		return false
	case <-worker.done:
		hop.Done(outcomeConnClosed)
		return false
	case <-worker.wake:
		hop.Done(outcomeReady)
		return true
	case <-timer.C:
		return true
	}
}

// prepareAssignment pops the next task from the queue and prepares the
// TaskAssignMessage to send to the worker.
//
// Returns false if the queue is empty or if there are no ready workers.
func (s *Scheduler) prepareAssignment() (taskAssignment, bool) {
	t, pos, ok := func() (*task, fair.Position, bool) {
		resourcesGuard := s.resourcesMut.RLock("prepare_assignment")
		defer resourcesGuard.RUnlock()

		assignGuard := s.assignMut.Lock("prepare_assignment")
		defer assignGuard.Unlock()

		// clean up any terminal tasks at the front of the queue.
		//
		// Read the state through State() (which holds task.mut) rather than
		// touching task.status directly: the field is mutated concurrently by
		// SetState under task.mut.
		for s.taskQueue.Len() > 0 && s.peekTask().State().Terminal() {
			s.taskQueue.Pop()
		}

		if len(s.connectedWorkers) == 0 || s.taskQueue.Len() == 0 {
			return nil, fair.Position{}, false
		}

		t, scope, pos := s.taskQueue.Pop()
		if scope != nil {
			// For now, we will treat every task as having an equal cost of 1.
			//
			// This provides some level of fairness, but not all tasks need the
			// same amount of time to complete, so tenants running heavy queries
			// do not get properly penalized.
			//
			// TODO(rfratto): Introduce a better cost estimate for tasks, which
			// may need to include re-adjusting the scope after task completion
			// if the cost estimate was wrong.
			_ = s.taskQueue.AdjustScope(scope, 1)
		}

		return t, pos, true
	}()

	if !ok {
		return taskAssignment{}, false
	}

	guard := s.resourcesMut.Lock("prepare_assignment")
	defer guard.Unlock()

	msg := s.buildAssignMessage(t)
	return taskAssignment{t: t, pos: pos, msg: msg}, true
}

// buildAssignMessage constructs a TaskAssignMessage for the given task.
//
// buildAssignMessage must be called while resourcesMut is held (at least RLock).
func (s *Scheduler) buildAssignMessage(t *task) wire.TaskAssignMessage {
	msg := wire.TaskAssignMessage{
		Task:         t.inner,
		StreamStates: make(map[ulid.ULID]workflow.StreamState),
		Metadata:     t.metadata,
	}

	// Populate stream states based on our view of streams that the task reads
	// from.
	for _, sources := range t.inner.Sources {
		for _, rawSource := range sources {
			source, found := s.streams[rawSource.ULID]
			if !found {
				// This shouldn't happen since all streams must be registered
				// before creating tasks, but we'll ignore it if it does.
				continue
			}

			msg.StreamStates[rawSource.ULID] = source.getState()
		}
	}

	return msg
}

// requeueTask re-inserts a task at its original position after a failed
// assignment, undoing the scope cost adjustment.
func (s *Scheduler) requeueTask(t *task, pos fair.Position) {
	assignGuard := s.assignMut.Lock("requeue_task")
	defer assignGuard.Unlock()

	if t.State().Terminal() {
		return
	}

	_ = s.taskQueue.Requeue(t.scope, t, pos)
	// Undo the scope cost that was applied when the task was popped.
	_ = s.taskQueue.AdjustScope(t.scope, -1)

	t.MarkRequeued()
	s.metrics.requeueTotal.Inc()

	if len(s.connectedWorkers) > 0 {
		nudgeSemaphore(s.assignSema)
	}
}

func (s *Scheduler) peekTask() *task {
	t, _ := s.taskQueue.Peek()
	return t
}

// pendingMessage represents a message to send to a worker after releasing locks.
type pendingMessage struct {
	peer *workerConn
	msg  wire.Message
}

// finalizeAssignment completes the assignment of the task to the worker.
func (s *Scheduler) finalizeAssignment(ctx context.Context, t *task, worker *workerConn, sentStates map[ulid.ULID]workflow.StreamState) {
	hop := s.metrics.startAssignmentHop("finalize_assignment")
	var pendingMsgs []pendingMessage

	// Collect bookkeeping and messages under a read lock. A read lock is
	// sufficient because everything here either reads the resourcesMut-protected
	// maps (s.streams, s.tasks) or mutates state guarded by its own lock: the
	// owner via task.markAssigned (task.mut) and the worker's task set via
	// worker.Assign (worker.mut). Using the write lock here would serialize every
	// dispatch — including the expensive per-source bind-message loop for
	// fan-in root tasks — and starve concurrent readers, stalling dispatch.
	func() {
		resourcesGuard := s.resourcesMut.RLock("finalize_assignment")
		defer resourcesGuard.RUnlock()

		assignTime := t.AssignTime() // Set by [task.TryAssign] by the previous caller before this runs.
		s.metrics.taskQueueSeconds.Observe(assignTime.Sub(t.QueueTime()).Seconds())

		if !t.markAssigned(worker) {
			// The task reached a terminal state between TryAssign and now. The
			// worker may still have the assignment, so we tell it to not
			// bother.
			pendingMsgs = append(pendingMsgs, pendingMessage{peer: worker, msg: wire.TaskCancelMessage{ID: t.inner.ULID}})
			return
		}

		worker.Assign(t)

		if t.wfRegion != nil {
			t.wfRegion.Record(StatAssignedTasks.Observe(1))
		}

		// Reconcile stream states: send updates for any that changed while sending.
		for streamID, sentState := range sentStates {
			if current, found := s.streams[streamID]; found {
				if curState := current.getState(); curState != sentState {
					pendingMsgs = append(pendingMsgs, pendingMessage{
						peer: worker,
						msg: wire.StreamStatusMessage{
							StreamID: streamID,
							State:    curState,
						},
					})
				}
			}
		}

		// Collect address binding messages.
		for _, sources := range t.inner.Sources {
			for _, rawSource := range sources {
				if source, found := s.streams[rawSource.ULID]; found {
					if msg := s.prepareBindMessage(source); msg != nil {
						pendingMsgs = append(pendingMsgs, *msg)
					}
				}
			}
		}
		for _, sinks := range t.inner.Sinks {
			for _, rawSink := range sinks {
				if sink, found := s.streams[rawSink.ULID]; found {
					if msg := s.prepareBindMessage(sink); msg != nil {
						pendingMsgs = append(pendingMsgs, *msg)
					}
				}
			}
		}
	}()

	// TODO(rfratto): allow timeout to be configurable.
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for _, p := range pendingMsgs {
		_ = p.peer.SendMessageAsync(ctx, p.msg)
	}
	hop.Done(outcomeSuccess)
}

// prepareBindMessage prepares a StreamBindMessage for the given stream if both
// sender and receiver are available. Returns nil if binding is not possible yet.
//
// prepareBindMessage must be called while the resourcesMut lock is held.
func (s *Scheduler) prepareBindMessage(check *stream) *pendingMessage {
	sendingTask, hasSendingTask := s.tasks[check.taskSender]
	var sendingOwner *workerConn
	if hasSendingTask {
		sendingOwner = sendingTask.owner.Load()
	}
	if sendingOwner == nil {
		// No sender, abort early.
		return nil
	}

	receivingTask, hasReceivingTask := s.tasks[check.taskReceiver]
	var receivingOwner *workerConn
	if hasReceivingTask {
		receivingOwner = receivingTask.owner.Load()
	}
	if receivingOwner != nil {
		// Bind the address of the receiving owner to the sender.
		return &pendingMessage{
			peer: sendingOwner,
			msg: wire.StreamBindMessage{
				StreamID: check.inner.ULID,
				Receiver: receivingOwner.RemoteAddr(),
			},
		}
	} else if check.localReceiver != nil {
		// We're listening for results ourselves; bind our address to the sender.
		return &pendingMessage{
			peer: sendingOwner,
			msg: wire.StreamBindMessage{
				StreamID: check.inner.ULID,
				Receiver: s.listener.Addr(),
			},
		}
	}

	return nil
}

func isTooManyRequestsError(err error) bool {
	if err == nil {
		return false
	}

	var wireError *wire.Error
	return errors.As(err, &wireError) && wireError.Code == http.StatusTooManyRequests
}

func assignmentAttemptOutcome(err error) metrictimer.Outcome {
	switch {
	case err == nil:
		return outcomeSuccess
	case errors.Is(err, errUnassignable):
		return outcomeUnassignable
	case isTooManyRequestsError(err):
		return outcomeNack429
	case errors.Is(err, context.DeadlineExceeded):
		return outcomeTimeout
	default:
		return outcomeSendError
	}
}

// workerSubscribe sends a WorkerSubscribe message to the provided worker. The
// worker will eventually send a WorkerReady message in response.
func (s *Scheduler) workerSubscribe(ctx context.Context, worker *workerConn) {
	if err := worker.SendMessageAsync(ctx, wire.WorkerSubscribeMessage{}); err != nil {
		level.Warn(s.logger).Log("msg", "failed to request subscription for ready worker thread", "err", err)
	}
}

// DialFrom connects to the scheduler using its local transport. The from
// address denotes the connecting peer.
func (s *Scheduler) DialFrom(ctx context.Context, from net.Addr) (wire.Conn, error) {
	if s == nil || s.listener == nil {
		return nil, errors.New("scheduler not initialized")
	}
	local, ok := s.listener.(*wire.Local)
	if !ok {
		// This really should not happen. Should we panic instead?
		return nil, errors.New("not a local scheduler")
	}
	return local.DialFrom(ctx, from)
}

// RegisterManifest registers a manifest to use with the scheduler, recording
// all streams and task inside of it for use.
func (s *Scheduler) RegisterManifest(ctx context.Context, manifest *workflow.Manifest) error {
	scope, err := s.registerManifestScope(manifest)
	if err != nil {
		return err
	}

	resourcesGuard := s.resourcesMut.Lock("register_manifest")
	defer resourcesGuard.Unlock()

	var errs []error

	var (
		manifestStreams = make(map[ulid.ULID]*stream, len(manifest.Streams))
		manifestTasks   = make(map[ulid.ULID]*task, len(manifest.Tasks))
	)

	for _, streamToAdd := range manifest.Streams {
		if _, exist := s.streams[streamToAdd.ULID]; exist {
			errs = append(errs, fmt.Errorf("stream %s already registered by other manifest", streamToAdd.ULID))
			continue
		} else if _, exist := manifestStreams[streamToAdd.ULID]; exist {
			errs = append(errs, fmt.Errorf("duplicate stream ID %s in manifest", streamToAdd.ULID))
			continue
		} else if streamToAdd.ULID == ulid.Zero {
			errs = append(errs, fmt.Errorf("stream %p has zero-value ULID", streamToAdd))
			continue
		}

		manifestStreams[streamToAdd.ULID] = &stream{
			inner:   streamToAdd,
			handler: manifest.StreamEventHandler,

			state: workflow.StreamStateIdle,
		}
	}

NextTask:
	for _, taskToAdd := range manifest.Tasks {
		if _, exist := s.tasks[taskToAdd.ULID]; exist {
			errs = append(errs, fmt.Errorf("task %s already registered by other manifest", taskToAdd.ULID))
			continue
		} else if _, exist := manifestTasks[taskToAdd.ULID]; exist {
			errs = append(errs, fmt.Errorf("duplicate task ID %s in manifest", taskToAdd.ULID))
			continue
		}

		for _, neededStreams := range taskToAdd.Sources {
			for _, neededStream := range neededStreams {
				sourceStream, inManifest := manifestStreams[neededStream.ULID]
				if !inManifest {
					errs = append(errs, fmt.Errorf("source stream %s not found in manifest", neededStream))
					continue NextTask
				} else if err := sourceStream.setTaskReceiver(taskToAdd.ULID); err != nil {
					errs = append(errs, err)
					continue NextTask
				}
			}
		}
		for _, neededStreams := range taskToAdd.Sinks {
			for _, neededStream := range neededStreams {
				sinkStream, inManifest := manifestStreams[neededStream.ULID]
				if !inManifest {
					errs = append(errs, fmt.Errorf("sink stream %s not found in manifest", neededStream))
					continue NextTask
				} else if err := sinkStream.setTaskSender(taskToAdd.ULID); err != nil {
					errs = append(errs, err)
					continue NextTask
				}
			}
		}

		// We create a fresh [xcap.Capture] for each task to generate per-task
		// observations. This will be merged with a capture received by the
		// worker upon receiving a terminal state.
		//
		// The scheduler region holds scheduler-side observations about this
		// specific task; worker-supplied regions are merged into the capture
		// alongside it.
		captureCtx, taskCapture := xcap.NewCapture(ctx, nil)
		_, taskRegion := xcap.StartRegion(captureCtx, "scheduler")

		manifestTasks[taskToAdd.ULID] = &task{
			createTime: time.Now(),
			scope:      scope,
			inner:      taskToAdd,
			handler:    manifest.TaskEventHandler,
			capture:    taskCapture,
			region:     taskRegion,
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	// Observe initial state for the streams and tasks.
	{
		var (
			initialStreamState = workflow.StreamStateIdle.String()
			initialTaskState   = workflow.TaskStateCreated.String()
		)

		for range manifestStreams {
			s.metrics.streamsTotal.WithLabelValues(initialStreamState).Inc()
		}
		for range manifestTasks {
			s.metrics.tasksTotal.WithLabelValues(initialTaskState).Inc()
		}
	}

	// Once we hit this point, the manifest has been validated and we can
	// atomically update our internal state.
	maps.Copy(s.streams, manifestStreams)
	maps.Copy(s.tasks, manifestTasks)

	xcap.RegionFromContext(ctx).Record(StatPlannedTasks.Observe(int64(len(manifestTasks))))
	return nil
}

// registerManifestScope generates and registers a [fair.Scope] for the given
// manifest.
func (s *Scheduler) registerManifestScope(manifest *workflow.Manifest) (fair.Scope, error) {
	scope := manifestScope(manifest)

	assignGuard := s.assignMut.Lock("register_manifest_scope")
	defer assignGuard.Unlock()

	if err := s.taskQueue.RegisterScope(scope); err != nil {
		return nil, err
	}

	level.Debug(s.logger).Log("msg", "registered queue scope", "scope", scope)
	return scope, nil
}

func manifestScope(manifest *workflow.Manifest) fair.Scope {
	scope := make(fair.Scope, 0, 1 /* tenant */ +len(manifest.Actor)+1 /* ID */)

	if manifest.Tenant != "" {
		scope = append(scope, manifest.Tenant)
	} else {
		scope = append(scope, "unknown-tenant")
	}

	scope = append(scope, manifest.Actor...)
	scope = append(scope, manifest.ID.String())

	return scope
}

// UnregisterManifest removes a manifest from the scheduler.
func (s *Scheduler) UnregisterManifest(ctx context.Context, manifest *workflow.Manifest) error {
	var n notifier
	defer n.Notify(ctx)

	if err := s.unregisterManifestScope(manifest); err != nil {
		level.Warn(s.logger).Log("msg", "failed unregistering queue scope", "id", manifest.ID, "err", err)
	}

	resourcesGuard := s.resourcesMut.Lock("unregister_manifest")
	defer resourcesGuard.Unlock()

	var errs []error

	// First, validate the manifest and ensure that all streams and tasks are recognized.
	for _, stream := range manifest.Streams {
		if _, exist := s.streams[stream.ULID]; !exist {
			errs = append(errs, fmt.Errorf("manifest contains unregistered stream %s", stream.ULID))
		}
	}
	for _, task := range manifest.Tasks {
		if _, exist := s.tasks[task.ULID]; !exist {
			errs = append(errs, fmt.Errorf("manifest contains unregistered task %s", task.ULID))
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	// Remove tasks first. If any of them are currently running, we'll cancel them.
	for _, taskToRemove := range manifest.Tasks {
		registered := s.tasks[taskToRemove.ULID] // Validated to exist above

		// Immediately clean up our own resources.
		s.deleteTask(registered)

		if changed, _ := registered.SetState(s.metrics, workflow.TaskStatus{State: workflow.TaskStateCancelled}); !changed {
			// Ignore if the task couldn't move into the canceled state, which
			// indicates it's already in a terminal state.
			continue
		}

		// If the task has an owner, we'll inform it that the task has been
		// canceled and it can stop processing it.
		//
		// This is a best-effort message, so we don't wait for acknowledgement.
		// Buffer it on the notifier so the send happens after resourcesMut is
		// released rather than blocking the lock on a full connection buffer.
		if owner := registered.owner.Load(); owner != nil {
			n.AddMessage(owner, wire.TaskCancelMessage{ID: registered.inner.ULID})
		}

		registered.RecordTerminalObservations(time.Now())

		// Inform the owner about the change. Attach the per-task capture so
		// the workflow consumer can read scheduler-side observations; once we
		// flip the state here, any later status message from a still-running
		// worker would be ignored as a no-op transition.
		notifyStatus := registered.Status()
		notifyStatus.Capture = registered.capture
		n.AddTaskEvent(taskNotification{
			Handler:   registered.handler,
			Task:      taskToRemove,
			NewStatus: notifyStatus,
		})
	}

	// Then, close and remove all remaining streams. All of the tasks associated
	// with the streams have been canceled, so we can safely immediately force
	// the streams to the closed state.
	for _, streamToRemove := range manifest.Streams {
		registered := s.streams[streamToRemove.ULID] // Validated to exist above

		changed, _ := registered.setState(s.metrics, workflow.StreamStateClosed)
		if changed {
			n.AddStreamEvent(streamNotification{
				Handler:  registered.handler,
				Stream:   streamToRemove,
				NewState: workflow.StreamStateClosed,
			})
		}

		delete(s.streams, streamToRemove.ULID)
	}

	return nil
}

// unregisterManifestScope unregisters a [fair.Scope] for the given
// manifest.
func (s *Scheduler) unregisterManifestScope(manifest *workflow.Manifest) error {
	scope := manifestScope(manifest)

	assignGuard := s.assignMut.Lock("unregister_manifest_scope")
	defer assignGuard.Unlock()

	if err := s.taskQueue.UnregisterScope(scope); err != nil {
		return err
	}

	level.Debug(s.logger).Log("msg", "unregistered queue scope", "scope", scope)
	return nil
}

func (s *Scheduler) deleteTask(t *task) {
	delete(s.tasks, t.inner.ULID)

	if owner := t.owner.Load(); owner != nil {
		owner.Unassign(t)
	}
}

// Listen binds the caller as the receiver of the specified stream. Listening on
// a stream prevents tasks from reading from it.
func (s *Scheduler) Listen(ctx context.Context, writer workflow.RecordWriter, stream *workflow.Stream) error {
	var pending *pendingMessage

	err := func() error {
		resourcesGuard := s.resourcesMut.Lock("listen")
		defer resourcesGuard.Unlock()

		registered, found := s.streams[stream.ULID]
		if !found {
			return fmt.Errorf("stream %s not registered", stream.ULID)
		} else if err := registered.setLocalListener(writer); err != nil {
			return err
		}

		// Usually, address binding is handled upon task assignment in
		// [Scheduler.assignTask]. However, calls to Listen can happen after the
		// sending task is already assigned. In this case, we'll attempt to bind
		// now.
		pending = s.prepareBindMessage(registered)
		return nil
	}()
	if err != nil {
		return err
	}

	// Send bind message outside the lock.
	if pending != nil {
		_ = pending.peer.SendMessageAsync(ctx, pending.msg)
	}
	return nil
}

// Start begins executing the provided tasks in the background. Start returns an
// error if any of the Tasks are unrecognized.
//
// The provided handler will be called whenever any of the provided tasks change
// state.
//
// Canceling the provided context does not cancel started tasks. Use
// [Scheduler.Cancel] to cancel started tasks.
func (s *Scheduler) Start(ctx context.Context, tasks ...*workflow.Task) error {
	trackedTasks, err := s.findTasks(tasks)
	if err != nil {
		return err
	}

	// Extract trace context from the query context and add it to each task's metadata.
	metadata := make(http.Header)

	// Copy all headers from context to task metadata.
	// Headers are stored in context by PropagateAllHeadersMiddleware.
	if headers := httpreq.ExtractAllHeaders(ctx); headers != nil {
		maps.Copy(metadata, headers)
	}

	var tc propagation.TraceContext
	tc.Inject(ctx, propagation.HeaderCarrier(metadata))

	wfRegion := xcap.RegionFromContext(ctx)
	wfRegion.Record(StatQueuedTasks.Observe(int64(len(tasks))))

	for _, t := range trackedTasks {
		if t.metadata == nil {
			t.metadata = make(http.Header)
		}

		maps.Copy(t.metadata, metadata)

		// Assign the workflow region to the task for metrics recording.
		t.wfRegion = wfRegion
		t.runtimeTraceCtx = ctx
	}

	// We set markPending *after* enqueueTasks to give tasks an opportunity to
	// immediately transition into running (lowering state transition noise).
	s.enqueueTasks(trackedTasks)
	s.markPending(ctx, trackedTasks)
	return nil
}

// findTasks gets a list of [task] from workflow tasks. Returns an error if any
// of the tasks weren't recognized.
func (s *Scheduler) findTasks(tasks []*workflow.Task) ([]*task, error) {
	resourcesGuard := s.resourcesMut.RLock("find_tasks")
	defer resourcesGuard.RUnlock()

	res := make([]*task, 0, len(tasks))

	var errs []error

	for _, task := range tasks {
		if t, ok := s.tasks[task.ULID]; ok {
			res = append(res, t)
		} else {
			errs = append(errs, fmt.Errorf("task %s not found", task.ULID))
		}
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return res, nil
}

func (s *Scheduler) enqueueTasks(tasks []*task) {
	assignGuard := s.assignMut.Lock("enqueue_tasks")
	defer assignGuard.Unlock()

	for _, task := range tasks {
		// Ignore tasks that aren't in the initial state (created). This
		// prevents us from rejecting tasks which were preemptively canceled by
		// callers.
		if task.State() != workflow.TaskStateCreated {
			continue
		}

		task.MarkQueued()
		if err := s.taskQueue.Push(task.scope, task); err != nil {
			level.Error(s.logger).Log("msg", "failed to enqueue task; task will not be executed", "id", task.inner.ULID, "err", err)
		}
	}

	if len(s.connectedWorkers) > 0 && s.taskQueue.Len() > 0 {
		nudgeSemaphore(s.assignSema)
	}
}

func (s *Scheduler) markPending(ctx context.Context, tasks []*task) {
	var n notifier
	defer n.Notify(ctx)

	// We intentionally do NOT hold s.resourcesMut here. markPending only mutates
	// per-task state (guarded by each task's own mut via SetState) and builds
	// local notifications; it never touches the resourcesMut-protected maps
	// (s.tasks, s.streams) or task.owner.
	//
	// Holding resourcesMut would be actively harmful: the dispatcher assigns
	// tasks concurrently via TryAssign, which holds a task's mut for the whole
	// duration of the (up to 5s) SendMessage to a worker. If markPending held
	// resourcesMut while SetState blocked on that same task's mut, it would pin
	// the global resourcesMut behind a single worker's send latency and starve
	// prepareAssignment (which needs resourcesMut.RLock), stalling all dispatch.

	for _, task := range tasks {
		if changed, _ := task.SetState(s.metrics, workflow.TaskStatus{State: workflow.TaskStatePending}); !changed {
			// If the state change failed, the task either got canceled or
			// picked up by a worker in between enqueueing it and calling this
			// method.
			continue
		}

		// Inform the owner about the state change from Created to Pending.
		n.AddTaskEvent(taskNotification{
			Handler:   task.handler,
			Task:      task.inner,
			NewStatus: task.Status(),
		})
	}
}

// Cancel requests cancellation of the specified tasks. Cancel returns an error
// if any of the tasks were not found.
//
// For tasks which are currently running on a worker, Cancel send a best-effort
// cancellation message to the worker. Cancel does not wait for these tasks to
// transition to a terminal state.
func (s *Scheduler) Cancel(ctx context.Context, tasks ...*workflow.Task) error {
	var n notifier
	defer n.Notify(ctx)

	// A read lock is sufficient: Cancel only reads the resourcesMut-protected
	// maps (s.tasks, s.streams) and task.owner (written only under the full
	// write lock, in finalizeAssignment). Everything it mutates is per-task
	// state (guarded by task.mut) or per-stream state (guarded by the stream's
	// stateMut, via closeTaskSinks -> changeStreamState). During workflow
	// teardown / short-circuit, Cancel is invoked in a storm (hundreds
	// concurrently, from workflow event handlers on the connection goroutines);
	// holding the write lock serializes them and starves the dispatcher's
	// prepareAssignment read lock, stalling dispatch. A read lock lets them run
	// concurrently.
	resourcesGuard := s.resourcesMut.RLock("cancel")
	defer resourcesGuard.RUnlock()

	var errs []error

	var (
		obsCanceledPending = StatCanceledPendingTasks.Observe(1)
		obsCanceledQueued  = StatCanceledQueuedTasks.Observe(1)
	)

	for _, taskToCancel := range tasks {
		registered, exist := s.tasks[taskToCancel.ULID]
		if !exist {
			errs = append(errs, fmt.Errorf("task %s not found", taskToCancel.ULID))
			continue
		}

		// Already in a terminal state — nothing to do for the task itself, but
		// fall through to the sink-stream cleanup below so a redundant Cancel
		// still closes any associated streams.
		prevState := registered.State()
		if prevState.Terminal() {
			s.closeTaskSinks(ctx, &n, registered)
			continue
		}

		region := registered.wfRegion

		if owner := registered.owner.Load(); owner != nil && registered.MarkInterrupted() {
			// The task is currently running on a worker. We don't want to
			// transition the task's state here so we can let the worker send
			// final stats before acknowledging cancellation.
			//
			// The confirmation of cancellation will come in via
			// handleTaskStatus. MarkInterrupted makes the cancel message
			// at most once: a redundant Cancel for an already-interrupted task
			// is a no-op.
			//
			// Buffer the send on the notifier so it happens after resourcesMut is
			// released: a short-circuit cancellation storm can otherwise back up
			// a worker's connection buffer and stall the scheduler while the lock
			// is held.
			n.AddMessage(owner, wire.TaskCancelMessage{ID: registered.inner.ULID})
		} else if changed, _ := registered.SetState(s.metrics, workflow.TaskStatus{State: workflow.TaskStateCancelled}); changed {
			// No worker is executing this task, so we transition the state
			// directly as the scheduler is the source of truth.
			//
			// TaskStateCreated maps to the "pending" stat: from the scheduler's
			// perspective, a task that has been registered via RegisterManifest
			// but not yet handed off to the queue via Start is pending
			// execution. The workflow.TaskStatePending value is a stronger
			// condition (the task is enqueued and waiting for a worker), which
			// is what we report as "queued".
			switch prevState {
			case workflow.TaskStateCreated:
				region.Record(obsCanceledPending)
			case workflow.TaskStatePending:
				region.Record(obsCanceledQueued)
			}

			registered.RecordTerminalObservations(time.Now())

			// Inform the owner about the change. Attach the per-task capture so
			// the workflow consumer can read scheduler-side observations even for
			// tasks that never reached a worker.
			notifyStatus := registered.Status()
			notifyStatus.Capture = registered.capture
			n.AddTaskEvent(taskNotification{
				Handler:   registered.handler,
				Task:      taskToCancel,
				NewStatus: notifyStatus,
			})
		}

		s.closeTaskSinks(ctx, &n, registered)
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// closeTaskSinks closes all sink streams associated with t (if not already
// closed) and queues stream-state notifications onto n.
//
// closeTaskSinks must be called while resourcesMut is held.
func (s *Scheduler) closeTaskSinks(ctx context.Context, n *notifier, t *task) {
	for _, sinks := range t.inner.Sinks {
		for _, rawSink := range sinks {
			sink, ok := s.streams[rawSink.ULID]
			if !ok {
				continue
			}

			_ = s.changeStreamState(n, sink, workflow.StreamStateClosed)
		}
	}
}

// RegisterMetrics registers metrics about s to report to reg.
func (s *Scheduler) RegisterMetrics(reg prometheus.Registerer) error {
	var errs []error

	errs = append(errs, reg.Register(s.collector))
	errs = append(errs, s.metrics.Register(reg))
	errs = append(errs, s.wireMetrics.Register(reg))

	return errors.Join(errs...)
}

// UnregisterMetrics unregisters metrics about s from reg.
func (s *Scheduler) UnregisterMetrics(reg prometheus.Registerer) {
	reg.Unregister(s.collector)
	s.metrics.Unregister(reg)
	s.wireMetrics.Unregister(reg)
}
