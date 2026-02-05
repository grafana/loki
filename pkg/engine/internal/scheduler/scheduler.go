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

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/oklog/ulid/v2"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/propagation"
	"golang.org/x/sync/errgroup"

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

	resourcesMut sync.RWMutex
	streams      map[ulid.ULID]*stream // All known streams (regardless of state)
	tasks        map[ulid.ULID]*task   // All known tasks (regardless of state)

	assignMut    sync.RWMutex
	taskQueue    fair.Queue[*task]
	readyWorkers map[*workerConn]struct{}

	assignSema chan struct{} // assignSema signals that task assignment is ready.

	wireMetrics *wire.Metrics
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

		readyWorkers: make(map[*workerConn]struct{}),

		assignSema: make(chan struct{}, 1),

		wireMetrics: wire.NewMetrics(),
	}

	s.metrics = newMetrics()
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

	wc := new(workerConn)

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
			switch msg := msg.(type) {
			case wire.StreamDataMessage:
				return s.handleStreamData(ctx, wc, msg)
			case wire.WorkerHelloMessage:
				return s.handleWorkerHello(ctx, wc, msg)
			case wire.WorkerReadyMessage:
				return s.markWorkerReady(ctx, wc)
			case wire.TaskStatusMessage:
				return s.handleTaskStatus(ctx, wc, msg)
			case wire.StreamStatusMessage:
				return s.handleStreamStatus(ctx, wc, msg)

			default:
				return fmt.Errorf("unsupported message kind %q", msg.Kind())
			}
		},
	}

	wc.Peer = peer

	// Handle communication with the peer until the context is canceled or some
	// error occurs.
	err := peer.Serve(ctx)
	if err != nil && ctx.Err() != nil && !errors.Is(err, wire.ErrConnClosed) {
		level.Warn(logger).Log("msg", "serve error", "err", err)
	} else {
		level.Debug(logger).Log("msg", "connection closed")
	}

	// If our peer exited, we need to make sure we clean up any tasks still
	// assigned to it by aborting them.
	s.removeWorker(ctx, wc, err)
}

func (s *Scheduler) handleStreamData(ctx context.Context, worker *workerConn, msg wire.StreamDataMessage) error {
	if err := worker.MarkDataPlane(); err != nil {
		return err
	}

	s.resourcesMut.RLock()
	defer s.resourcesMut.RUnlock()

	registered, found := s.streams[msg.StreamID]
	if !found {
		return fmt.Errorf("stream %d not found", msg.StreamID)
	} else if registered.localReceiver == nil {
		return fmt.Errorf("scheduler is not listening for data for stream %s", msg.StreamID)
	}

	return registered.localReceiver.Write(ctx, msg.Data)
}

func (s *Scheduler) handleWorkerHello(ctx context.Context, worker *workerConn, msg wire.WorkerHelloMessage) error {
	if err := worker.HandleHello(msg); err != nil {
		return err
	}

	// Request to be notified when the worker is ready.
	s.workerSubscribe(ctx, worker)
	return nil
}

func (s *Scheduler) markWorkerReady(_ context.Context, worker *workerConn) error {
	s.assignMut.Lock()
	defer s.assignMut.Unlock()

	if err := worker.MarkReady(); err != nil {
		return err
	}
	s.readyWorkers[worker] = struct{}{}

	// Wake [Scheduler.runAssignLoop] if we have both peers and tasks available.
	if len(s.readyWorkers) > 0 && len(s.tasks) > 0 {
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

func (s *Scheduler) handleTaskStatus(ctx context.Context, worker *workerConn, msg wire.TaskStatusMessage) error {
	if got, want := worker.Type(), connectionTypeControlPlane; got != want {
		return fmt.Errorf("worker connection must be in state %q, got %q", want, got)
	}

	var n notifier
	defer n.Notify(ctx)

	s.resourcesMut.Lock()
	defer s.resourcesMut.Unlock()

	task, found := s.tasks[msg.ID]
	if !found {
		return fmt.Errorf("task %s not found", msg.ID)
	}

	changed, err := task.setState(s.metrics, msg.Status)
	if err != nil {
		return err
	} else if changed {
		if owner := task.owner; owner != nil && task.status.State.Terminal() {
			owner.Unassign(task)
		}

		if task.status.State == workflow.TaskStateCompleted {
			// The execution time of the task is the duration from when it was
			// first assigned to when we received the completion status.
			s.metrics.taskExecSeconds.Observe(time.Since(task.assignTime).Seconds())
		}

		// Notify the handler about the change.
		n.AddTaskEvent(taskNotification{
			Handler:   task.handler,
			Task:      task.inner,
			NewStatus: msg.Status,
		})
	}
	return nil
}

func (s *Scheduler) handleStreamStatus(ctx context.Context, worker *workerConn, msg wire.StreamStatusMessage) error {
	if got, want := worker.Type(), connectionTypeControlPlane; got != want {
		return fmt.Errorf("worker connection must be in state %q, got %q", want, got)
	}

	var n notifier
	defer n.Notify(ctx)

	s.resourcesMut.Lock()
	defer s.resourcesMut.Unlock()

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
// If the reason argument is nil, the tasks are cancelled. Otherwise, they are
// marked as failed with the provided reason.
func (s *Scheduler) removeWorker(ctx context.Context, worker *workerConn, reason error) {
	var n notifier
	defer n.Notify(ctx)

	s.resourcesMut.Lock()
	defer s.resourcesMut.Unlock()

	s.assignMut.Lock()
	defer s.assignMut.Unlock()

	newStatus := workflow.TaskStatus{State: workflow.TaskStateCancelled}
	if reason != nil {
		newStatus = workflow.TaskStatus{
			State: workflow.TaskStateFailed,
			Error: reason,
		}
	}

	for _, task := range worker.Assigned() {
		worker.Unassign(task)

		if changed, _ := task.setState(s.metrics, newStatus); !changed {
			continue
		}

		// We only need to inform the handler about the change. There's nothing
		// to send to the owner of the task since worker has disconnected.
		n.AddTaskEvent(taskNotification{
			Handler:   task.handler,
			Task:      task.inner,
			NewStatus: newStatus,
		})
	}

	// Remove the worker from the ready list, if it exists.
	delete(s.readyWorkers, worker)
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

	assignOne := func(worker *workerConn, msg wire.TaskAssignMessage) bool {
		// TODO(rfratto): allow assignment timeout to be configurable.
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		level.Debug(s.logger).Log("msg", "assigning task", "id", msg.Task.ULID, "conn", worker.RemoteAddr())

		err := worker.SendMessage(ctx, msg)
		if err == nil {
			return true
		}

		level.Warn(s.logger).Log("msg", "failed to assign task", "id", msg.Task.ULID, "conn", worker.RemoteAddr(), "err", err)
		if isTooManyRequestsError(err) {
			// The worker has no more capacity available, so remove it from the
			// ready list and wait to receive another Ready message.
			s.assignMut.Lock()
			delete(s.readyWorkers, worker)
			s.assignMut.Unlock()

			s.metrics.backoffsTotal.Inc()
			s.workerSubscribe(ctx, worker)
		}

		return false
	}

	for ctx.Err() == nil {
		t, worker, msg, ok := s.prepareAssignment()
		if !ok {
			return
		}

		if assigned := assignOne(worker, msg); !assigned {
			continue
		}

		// Remove from queue on successful assignment, and apply a cost to the
		// scope.
		s.assignMut.Lock()
		{
			// TODO(rfratto): It's currently possible for other goroutines to
			// have modified taskQueue in between calling prepareAssignment and
			// Pop.
			//
			// For example, if the only running query gets canceled in between
			// the two operations, unregistering the scope will drain the
			// remainder of the queue, causing Pop to return nil.
			//
			// This should be fixed by removing the race condition, but for now
			// we'll log an error to track it.
			popped, scope := s.taskQueue.Pop()
			s.validatePop(t, popped)

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
		}
		s.assignMut.Unlock()

		s.finalizeAssignment(ctx, t, worker, msg.StreamStates)
	}
}

// validatePop logs an error if the expected task does not match the actual task.
func (s *Scheduler) validatePop(expect, actual *task) {
	if expect == actual {
		return
	}

	var expectedULID, actualULID ulid.ULID
	if expect != nil && expect.inner != nil {
		expectedULID = expect.inner.ULID
	}
	if actual != nil && actual.inner != nil {
		actualULID = actual.inner.ULID
	}

	level.Error(s.logger).Log("msg", "unexpected task removed from queue", "expected", expectedULID, "actual", actualULID)
}

// prepareAssignment builds a TaskAssignMessage for the next task and worker.
//
// Returns false if no candidates available.
func (s *Scheduler) prepareAssignment() (*task, *workerConn, wire.TaskAssignMessage, bool) {
	s.resourcesMut.RLock()
	defer s.resourcesMut.RUnlock()

	s.assignMut.Lock()
	defer s.assignMut.Unlock()

	// clean up any terminal tasks at the front of the queue.
	for s.taskQueue.Len() > 0 && s.peekTask().status.State.Terminal() {
		s.taskQueue.Pop()
	}

	if s.taskQueue.Len() == 0 || len(s.readyWorkers) == 0 {
		return nil, nil, wire.TaskAssignMessage{}, false
	}

	task := s.peekTask()
	worker := nextWorker(s.readyWorkers)

	msg := wire.TaskAssignMessage{
		Task:         task.inner,
		StreamStates: make(map[ulid.ULID]workflow.StreamState),
		Metadata:     task.metadata,
	}

	// Populate stream states based on our view of streams that the task reads
	// from.
	for _, sources := range task.inner.Sources {
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

	return task, worker, msg, true
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
	var pendingMsgs []pendingMessage

	// Collect bookkeeping and messages under lock.
	func() {
		s.resourcesMut.Lock()
		defer s.resourcesMut.Unlock()

		s.assignMut.Lock()
		defer s.assignMut.Unlock()

		if t.status.State.Terminal() {
			// Worker received the assignment but task was cancelled in the meantime.
			// Notify the worker about the cancellation.
			pendingMsgs = append(pendingMsgs, pendingMessage{peer: worker, msg: wire.TaskCancelMessage{ID: t.inner.ULID}})
			return
		}

		worker.Assign(t)
		t.assignTime = time.Now()
		queueDuration := t.assignTime.Sub(t.queueTime).Seconds()
		s.metrics.taskQueueSeconds.Observe(queueDuration)

		if t.wfRegion != nil {
			t.wfRegion.Record(xcap.StatTaskMaxQueueDuration.Observe(queueDuration))

			// Record time from workflow start until this task assignment.
			assignmentTailDuration := t.assignTime.Sub(t.wfRegion.StartTime()).Seconds()
			t.wfRegion.Record(xcap.StatTaskAssignmentTailDuration.Observe(assignmentTailDuration))
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

// nextWorker returns the next worker from the map. The worker returned is
// random.
func nextWorker(m map[*workerConn]struct{}) *workerConn {
	for worker := range m {
		return worker
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
func (s *Scheduler) RegisterManifest(_ context.Context, manifest *workflow.Manifest) error {
	scope, err := s.registerManifestScope(manifest)
	if err != nil {
		return err
	}

	s.resourcesMut.Lock()
	defer s.resourcesMut.Unlock()

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

		manifestTasks[taskToAdd.ULID] = &task{
			scope:   scope,
			inner:   taskToAdd,
			handler: manifest.TaskEventHandler,
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
	return nil
}

// registerManifestScope generates and registers a [fair.Scope] for the given
// manifest.
func (s *Scheduler) registerManifestScope(manifest *workflow.Manifest) (fair.Scope, error) {
	scope := manifestScope(manifest)

	s.assignMut.Lock()
	defer s.assignMut.Unlock()

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

	s.resourcesMut.Lock()
	defer s.resourcesMut.Unlock()

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

		if changed, _ := registered.setState(s.metrics, workflow.TaskStatus{State: workflow.TaskStateCancelled}); !changed {
			// Ignore if the task couldn't move into the canceled state, which
			// indicates it's already in a terminal state.
			continue
		}

		// If the task has an owner, we'll inform it that the task has been
		// canceled and it can stop processing it.
		//
		// This is a best-effort message, so we don't wait for acknowledgement.
		if owner := registered.owner; owner != nil {
			_ = owner.SendMessageAsync(ctx, wire.TaskCancelMessage{ID: registered.inner.ULID})
		}

		// Inform the owner about the change.
		n.AddTaskEvent(taskNotification{
			Handler:   registered.handler,
			Task:      taskToRemove,
			NewStatus: registered.status,
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

	s.assignMut.Lock()
	defer s.assignMut.Unlock()

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
		s.resourcesMut.Lock()
		defer s.resourcesMut.Unlock()

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
	var tc propagation.TraceContext
	metadata := make(http.Header)
	tc.Inject(ctx, propagation.HeaderCarrier(metadata))

	// Copy all headers from context to task metadata.
	// Headers are stored in context by PropagateAllHeadersMiddleware.
	if headers := httpreq.ExtractAllHeaders(ctx); headers != nil {
		maps.Copy(metadata, headers)
	}

	wfRegion := xcap.RegionFromContext(ctx)

	for _, t := range trackedTasks {
		if t.metadata == nil {
			t.metadata = make(http.Header)
		}

		maps.Copy(t.metadata, metadata)

		// Assign the workflow region to the task for metrics recording.
		t.wfRegion = wfRegion
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
	s.resourcesMut.RLock()
	defer s.resourcesMut.RUnlock()

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
	s.assignMut.Lock()
	defer s.assignMut.Unlock()

	for _, task := range tasks {
		// Ignore tasks that aren't in the initial state (created). This
		// prevents us from rejecting tasks which were preemptively canceled by
		// callers.
		if got, want := task.status.State, workflow.TaskStateCreated; got != want {
			continue
		}

		task.queueTime = time.Now()
		if err := s.taskQueue.Push(task.scope, task); err != nil {
			level.Error(s.logger).Log("msg", "failed to enqueue task; task will not be executed", "id", task.inner.ULID, "err", err)
		}
	}

	if len(s.readyWorkers) > 0 && s.taskQueue.Len() > 0 {
		nudgeSemaphore(s.assignSema)
	}
}

func (s *Scheduler) markPending(ctx context.Context, tasks []*task) {
	var n notifier
	defer n.Notify(ctx)

	s.resourcesMut.Lock()
	defer s.resourcesMut.Unlock()

	for _, task := range tasks {
		if changed, _ := task.setState(s.metrics, workflow.TaskStatus{State: workflow.TaskStatePending}); !changed {
			// If the state change failed, the task either got canceled or
			// picked up by a worker in between enqueueing it and calling this
			// method.
			continue
		}

		// Inform the owner about the state change from Created to Pending.
		n.AddTaskEvent(taskNotification{
			Handler:   task.handler,
			Task:      task.inner,
			NewStatus: task.status,
		})
	}
}

// Cancel requests cancellation of the specified tasks. Cancel returns an error
// if any of the tasks were not found.
func (s *Scheduler) Cancel(ctx context.Context, tasks ...*workflow.Task) error {
	var n notifier
	defer n.Notify(ctx)

	s.resourcesMut.Lock()
	defer s.resourcesMut.Unlock()

	var errs []error

	for _, taskToCancel := range tasks {
		registered, exist := s.tasks[taskToCancel.ULID]
		if !exist {
			errs = append(errs, fmt.Errorf("task %s not found", taskToCancel.ULID))
			continue
		}

		if changed, _ := registered.setState(s.metrics, workflow.TaskStatus{State: workflow.TaskStateCancelled}); changed {
			// If the task has an owner, we'll inform it that the task has been
			// canceled and it can stop processing it.
			//
			// This is a best-effort message, so we don't wait for acknowledgement.
			if owner := registered.owner; owner != nil {
				owner.Unassign(registered)
				_ = owner.SendMessageAsync(ctx, wire.TaskCancelMessage{ID: registered.inner.ULID})
			}

			// Inform the owner about the change.
			n.AddTaskEvent(taskNotification{
				Handler:   registered.handler,
				Task:      taskToCancel,
				NewStatus: registered.status,
			})
		}

		// Close all associated sink streams (if they are not already closed).
		for _, sinks := range registered.inner.Sinks {
			for _, rawSink := range sinks {
				sink, ok := s.streams[rawSink.ULID]
				if !ok {
					continue
				}

				_ = s.changeStreamState(ctx, &n, sink, workflow.StreamStateClosed)
			}
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
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
