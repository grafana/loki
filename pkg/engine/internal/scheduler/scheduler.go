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

	// connCtx is scoped to this connection so goroutines started for it (e.g.
	// workerLoop) stop when it closes, independent of any single message handler.
	connCtx, cancelConn := context.WithCancel(ctx)
	defer cancelConn()

	wc := &workerConn{
		ctx:  connCtx,
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
		return s.markWorkerReady(worker)
	case wire.TaskResultMessage:
		return s.handleTaskResult(ctx, worker, msg)
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

func (s *Scheduler) markWorkerReady(worker *workerConn) (err error) {
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
		// workerLoop outlives this WorkerReady handler, so it runs under the
		// connection context, not the handler's (which ends when the handler
		// returns).
		go s.workerLoop(worker.ctx, worker)
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

func (s *Scheduler) handleTaskResult(ctx context.Context, worker *workerConn, msg wire.TaskResultMessage) (err error) {
	phases := s.metrics.startHandler(msg.Kind())
	defer func() { phases.Done(handlerOutcome(err)) }()

	if got, want := worker.Type(), connectionTypeControlPlane; got != want {
		return fmt.Errorf("worker connection must be in state %q, got %q", want, got)
	}

	var n notifier
	defer n.Notify(ctx)

	resourcesGuard := s.resourcesMut.Lock("handle_task_result")
	defer resourcesGuard.Unlock()

	task, found := s.tasks[msg.ID]
	if !found {
		return fmt.Errorf("task %s not found", msg.ID)
	}

	// TryAssign holds task.mut through the TaskAssign ACK and records assignTime
	// before releasing it. A racing result therefore blocks in AssignTime until
	// accepted-assignment evidence is visible.
	if task.AssignTime().IsZero() {
		return fmt.Errorf("task %s produced a result before accepting an assignment", msg.ID)
	}

	changed, err := task.SetResult(s.metrics, msg.Result)
	if err != nil {
		return err
	} else if !changed {
		return nil
	}

	s.closeTaskSinks(ctx, &n, task)

	if owner := task.owner; owner != nil {
		owner.Unassign(task)
	}

	switch msg.Result.Outcome {
	case workflow.TaskOutcomeCompleted:
		task.wfRegion.Record(StatExecutedTasks.Observe(1))

		// The execution time of the task is the duration from when it was first
		// assigned to when we received the completion result.
		s.metrics.taskExecSeconds.Observe(time.Since(task.AssignTime()).Seconds())
	case workflow.TaskOutcomeCancelled:
		// The worker has confirmed cancellation of a previously assigned task
		// from [Scheduler.Cancel].
		task.wfRegion.Record(StatCanceledAssignedTasks.Observe(1))
	case workflow.TaskOutcomeFailed:
		task.wfRegion.Record(StatFailedTasks.Observe(1))
	}

	// Results may include a capture from the worker. Merge it into the
	// scheduler's per-task capture so the handler sees one unified capture.
	task.RecordTerminalObservations(time.Now())
	task.capture.Merge(nil, msg.Result.Capture)
	result := msg.Result
	result.Capture = task.capture

	n.AddTaskEvent(taskNotification{
		Handler: task.handler,
		Task:    task.inner,
		Result:  result,
	})
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

	resourcesGuard := s.resourcesMut.Lock("handle_stream_status")
	defer resourcesGuard.Unlock()

	stream, found := s.streams[msg.StreamID]
	if !found {
		return fmt.Errorf("stream %s not found", msg.StreamID)
	}
	return s.changeStreamState(ctx, &n, stream, msg.State)
}

// changeStreamState updates the state of the target stream. changeStreamState
// must be called while the resourcesMut lock is held.
func (s *Scheduler) changeStreamState(ctx context.Context, n *notifier, target *stream, newState workflow.StreamState) error {
	changed, err := target.setState(s.metrics, newState)
	if err != nil {
		return err
	} else if !changed {
		return nil
	}

	// If we have a receiver, inform them about the change. This is a
	// best-effort message so we don't need to wait for acknowledgement.
	receiver, found := s.tasks[target.taskReceiver]
	if found && receiver.owner != nil {
		_ = receiver.owner.SendMessageAsync(ctx, wire.StreamStatusMessage{
			StreamID: target.inner.ULID,
			State:    newState,
		})
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
		pendingCancel := task.PendingCancel()
		worker.Unassign(task)

		// Even though the worker disconnected we still have some stats about
		// the task that the handler may be interested in.
		result := workflow.TaskResult{Capture: task.capture}
		switch {
		case pendingCancel:
			// Cancellation had already been requested for this task. Honor that
			// intent regardless of whether the worker disconnected cleanly or
			// with an error.
			result.Outcome = workflow.TaskOutcomeCancelled
		case reason != nil:
			result.Outcome = workflow.TaskOutcomeFailed
			result.Error = reason
		default:
			result.Outcome = workflow.TaskOutcomeCancelled
		}

		if changed, _ := task.SetResult(s.metrics, result); !changed {
			continue
		}

		switch result.Outcome {
		case workflow.TaskOutcomeCancelled:
			if pendingCancel {
				// The worker disconnected before confirming a cancellation we
				// had requested. Record it against the assigned-cancellation
				// stat to match the disposition that [Scheduler.Cancel] would
				// have produced had the worker survived long enough to confirm.
				task.wfRegion.Record(StatCanceledAssignedTasks.Observe(1))
			}
			// Otherwise the worker disconnected cleanly with no pending
			// cancellation request; we have no specific stat for that category
			// today.
		case workflow.TaskOutcomeFailed:
			task.wfRegion.Record(StatFailedTasks.Observe(1))
		}

		task.RecordTerminalObservations(time.Now())

		// We only need to inform the handler about the result. There's nothing
		// to send to the owner of the task since the worker has disconnected.
		n.AddTaskEvent(taskNotification{
			Handler: task.handler,
			Task:    task.inner,
			Result:  result,
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
			// Task already has a terminal result or cancellation was requested,
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

// parkWorker blocks until the worker advertises freed capacity (a ready message
// nudges worker.wake), the worker disconnects, or the scheduler shuts down. It
// returns true if the assignment loop should resume, or false if it should
// exit.
func (s *Scheduler) parkWorker(ctx context.Context, worker *workerConn) bool {
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
	}
}

// prepareAssignment pops the next task from the queue and prepares the
// TaskAssignMessage to send to the worker.
//
// Returns false if the queue is empty or if there are no ready workers.
func (s *Scheduler) prepareAssignment() (taskAssignment, bool) {
	resourcesGuard := s.resourcesMut.RLock("prepare_assignment")
	defer resourcesGuard.RUnlock()

	assignGuard := s.assignMut.Lock("prepare_assignment")
	defer assignGuard.Unlock()

	// clean up any terminal tasks at the front of the queue.
	for s.taskQueue.Len() > 0 && s.peekTask().HasResult() {
		s.taskQueue.Pop()
	}

	if len(s.connectedWorkers) == 0 || s.taskQueue.Len() == 0 {
		return taskAssignment{}, false
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

			msg.StreamStates[rawSource.ULID] = source.state
		}
	}

	return msg
}

// requeueTask re-inserts a task at its original position after a failed
// assignment, undoing the scope cost adjustment.
func (s *Scheduler) requeueTask(t *task, pos fair.Position) {
	assignGuard := s.assignMut.Lock("requeue_task")
	defer assignGuard.Unlock()

	if t.HasResult() {
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

	// Collect bookkeeping and messages under lock.
	func() {
		resourcesGuard := s.resourcesMut.Lock("finalize_assignment")
		defer resourcesGuard.Unlock()

		assignTime := t.AssignTime() // Set by [task.TryAssign] by the previous caller before this runs.
		s.metrics.taskQueueSeconds.Observe(assignTime.Sub(t.QueueTime()).Seconds())

		if t.HasResult() {
			// The task produced a terminal result between TryAssign and now. The
			// worker may still have the assignment, so we tell it to not
			// bother.
			pendingMsgs = append(pendingMsgs, pendingMessage{peer: worker, msg: wire.TaskCancelMessage{ID: t.inner.ULID}})
			return
		}

		worker.Assign(t)
		s.metrics.tasksAssignedTotal.Inc()

		if t.wfRegion != nil {
			t.wfRegion.Record(StatAssignedTasks.Observe(1))
		}

		// Reconcile stream states: send updates for any that changed while sending.
		for streamID, sentState := range sentStates {
			if current, found := s.streams[streamID]; found && current.state != sentState {
				pendingMsgs = append(pendingMsgs, pendingMessage{
					peer: worker,
					msg: wire.StreamStatusMessage{
						StreamID: streamID,
						State:    current.state,
					},
				})
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
	if !hasSendingTask || sendingTask.owner == nil {
		// No sender, abort early.
		return nil
	}

	receivingTask, hasReceivingTask := s.tasks[check.taskReceiver]
	if hasReceivingTask && receivingTask.owner != nil {
		// Bind the address of the receiving owner to the sender.
		return &pendingMessage{
			peer: sendingTask.owner,
			msg: wire.StreamBindMessage{
				StreamID: check.inner.ULID,
				Receiver: receivingTask.owner.RemoteAddr(),
			},
		}
	} else if check.localReceiver != nil {
		// We're listening for results ourselves; bind our address to the sender.
		return &pendingMessage{
			peer: sendingTask.owner,
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
		// worker upon receiving a terminal result.
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
			handler:    manifest.TaskResultHandler,
			capture:    taskCapture,
			region:     taskRegion,
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	for range manifestStreams {
		s.metrics.streamsTotal.WithLabelValues(workflow.StreamStateIdle.String()).Inc()
	}

	// Once we hit this point, the manifest has been validated and we can
	// atomically update our internal state.
	maps.Copy(s.streams, manifestStreams)
	maps.Copy(s.tasks, manifestTasks)
	s.metrics.tasksRegisteredTotal.Add(float64(len(manifestTasks)))

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

	// Remove tasks first. If any are assigned, ask their workers to cancel them.
	for _, taskToRemove := range manifest.Tasks {
		registered := s.tasks[taskToRemove.ULID] // Validated to exist above

		// Immediately clean up our own resources.
		s.deleteTask(registered)

		result := workflow.TaskResult{Outcome: workflow.TaskOutcomeCancelled, Capture: registered.capture}
		if changed, _ := registered.SetResult(s.metrics, result); !changed {
			continue
		}
		s.closeTaskSinks(ctx, &n, registered)

		// If the task has an owner, inform it that the task has been cancelled
		// and it can stop processing it. This is a best-effort message, so we do
		// not wait for acknowledgement.
		if owner := registered.owner; owner != nil {
			_ = owner.SendMessageAsync(ctx, wire.TaskCancelMessage{ID: registered.inner.ULID})
		}

		registered.RecordTerminalObservations(time.Now())

		// Attach the per-task capture so the workflow consumer can read
		// scheduler-side observations. Any later result from a still-running
		// worker is ignored because the first terminal result is authoritative.
		n.AddTaskEvent(taskNotification{
			Handler: registered.handler,
			Task:    taskToRemove,
			Result:  result,
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

	if owner := t.owner; owner != nil {
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

	s.enqueueTasks(trackedTasks)
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
		// Ignore tasks that were already submitted or preemptively cancelled.
		if !task.MarkQueued() {
			continue
		}

		if err := s.taskQueue.Push(task.scope, task); err != nil {
			level.Error(s.logger).Log("msg", "failed to enqueue task; task will not be executed", "id", task.inner.ULID, "err", err)
		}
	}

	if len(s.connectedWorkers) > 0 && s.taskQueue.Len() > 0 {
		nudgeSemaphore(s.assignSema)
	}
}

// Cancel requests cancellation of the specified tasks. Cancel returns an error
// if any of the tasks were not found.
//
// For tasks which are currently running on a worker, Cancel send a best-effort
// cancellation message to the worker. Cancel does not wait for these tasks to
// produce a terminal result.
func (s *Scheduler) Cancel(ctx context.Context, tasks ...*workflow.Task) error {
	var n notifier
	defer n.Notify(ctx)

	resourcesGuard := s.resourcesMut.Lock("cancel")
	defer resourcesGuard.Unlock()

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

		if registered.HasResult() {
			s.closeTaskSinks(ctx, &n, registered)
			continue
		}

		if owner := registered.owner; owner != nil {
			// Let the worker return its capture with the cancellation result.
			// RequestCancel makes the best-effort message at most once.
			if registered.RequestCancel() {
				_ = owner.SendMessageAsync(ctx, wire.TaskCancelMessage{ID: registered.inner.ULID})
			}
			s.closeTaskSinks(ctx, &n, registered)
			continue
		}

		// No worker owns this task, so the scheduler produces its result.
		result := workflow.TaskResult{Outcome: workflow.TaskOutcomeCancelled, Capture: registered.capture}
		changed, _ := registered.SetResult(s.metrics, result)
		if !changed {
			continue
		}

		if registered.Queued() {
			registered.wfRegion.Record(obsCanceledQueued)
		} else {
			registered.wfRegion.Record(obsCanceledPending)
		}

		registered.RecordTerminalObservations(time.Now())
		n.AddTaskEvent(taskNotification{
			Handler: registered.handler,
			Task:    taskToCancel,
			Result:  result,
		})
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

			_ = s.changeStreamState(ctx, n, sink, workflow.StreamStateClosed)
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
