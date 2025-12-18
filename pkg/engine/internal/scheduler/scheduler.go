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
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
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
	taskQueue    []*task
	readyWorkers map[*workerConn]struct{}

	assignSema chan struct{} // assignSema signals that task assignment is ready.
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
		Logger: logger,
		Conn:   conn,

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
	var n notifier
	defer n.Notify(ctx)

	// We need to grab the lock on resources to prevent stream states from being
	// modified while we're assigning the task.
	//
	// This prevents a race condition where a task owner misses a stream state
	// change while we're assigning tasks at the same time as a state change.
	//
	// TODO(rfratto): Is there going to be too much overhead for locking this
	// for this long?
	s.resourcesMut.Lock()
	defer s.resourcesMut.Unlock()

	s.assignMut.Lock()
	defer s.assignMut.Unlock()

	level.Debug(s.logger).Log("msg", "performing task assignment")

	for len(s.taskQueue) > 0 && len(s.readyWorkers) > 0 {
		task := s.taskQueue[0]
		worker := nextWorker(s.readyWorkers)

		// We may have a canceled task in our queue; we take this opportunity to
		// clean them up.
		if state := task.status.State; state.Terminal() {
			s.taskQueue = s.taskQueue[1:]
			continue
		}

		level.Debug(s.logger).Log("msg", "assigning task", "id", task.inner.ULID, "conn", worker.RemoteAddr())
		if err := s.assignTask(ctx, task, worker); err != nil && ctx.Err() != nil {
			// Our context got canceled, abort task assignment.
			return
		} else if err != nil && isTooManyRequestsError(err) {
			// The worker has no more capacity available, so remove it from the
			// ready list and wait to receive another Ready message.
			delete(s.readyWorkers, worker)
			s.metrics.backoffsTotal.Inc()

			s.workerSubscribe(ctx, worker)
			continue
		} else if err != nil {
			level.Warn(s.logger).Log("msg", "failed to assign task", "id", task.inner.ULID, "conn", worker.RemoteAddr(), "err", err)
			continue
		}

		// Pop the task now that it's been officially assigned.
		s.taskQueue = s.taskQueue[1:]
	}
}

// nextWorker returns the next worker from the map. The worker returned is
// random.
func nextWorker(m map[*workerConn]struct{}) *workerConn {
	for worker := range m {
		return worker
	}
	return nil
}

func (s *Scheduler) assignTask(ctx context.Context, task *task, worker *workerConn) error {
	// TODO(rfratto): allow assignment timeout to be configurable.
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

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

	if err := worker.SendMessage(ctx, msg); err != nil {
		// TODO(rfratto): Should we forcibly close peer connections if they fail
		// to accept tasks?
		return err
	}

	// The worker accepted the message, so we can assign the task to it now.
	worker.Assign(task)

	// The queue time of a task is the duration from when it entered the queue
	// to when a worker accepted the assignment.
	//
	// We track this moment as the "assign time" to be able to calculate
	// execution time later.
	task.assignTime = time.Now()
	s.metrics.taskQueueSeconds.Observe(task.assignTime.Sub(task.queueTime).Seconds())

	// Now that the task has been accepted, we can attempt address bindings. We
	// do this on task assignment to simplify the implementation, though it
	// means that the first call to tryBind will always fail (because one end
	// isn't available yet).
	for _, sources := range task.inner.Sources {
		for _, rawSource := range sources {
			source, found := s.streams[rawSource.ULID]
			if !found {
				continue
			}
			s.tryBind(ctx, source)
		}
	}
	for _, sinks := range task.inner.Sinks {
		for _, rawSink := range sinks {
			sink, found := s.streams[rawSink.ULID]
			if !found {
				continue
			}
			s.tryBind(ctx, sink)
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

// workerSubscribe sends a WorkerSubscribe message to the provided worker. The
// worker will eventually send a WorkerReady message in response.
func (s *Scheduler) workerSubscribe(ctx context.Context, worker *workerConn) {
	if err := worker.SendMessageAsync(ctx, wire.WorkerSubscribeMessage{}); err != nil {
		level.Warn(s.logger).Log("msg", "failed to request subscription for ready worker thread", "err", err)
	}
}

// tryBind attempts to bind the receiver's address of check to the sender.
// tryBind is a no-op if the stream does not have both a sender and receiver
// yet.
//
// tryBind must be called while the resourcesMut lock is held.
func (s *Scheduler) tryBind(ctx context.Context, check *stream) {
	sendingTask, hasSendingTask := s.tasks[check.taskSender]
	if !hasSendingTask || sendingTask.owner == nil {
		// No sender, abort early.
		return
	}

	receivingTask, hasReceivingTask := s.tasks[check.taskReceiver]
	if hasReceivingTask && receivingTask.owner != nil {
		// Bind the address of the receiving owner to the sender.
		_ = sendingTask.owner.SendMessageAsync(ctx, wire.StreamBindMessage{
			StreamID: check.inner.ULID,
			Receiver: receivingTask.owner.RemoteAddr(),
		})
	} else if check.localReceiver != nil {
		// We're listening for results ourselves; bind our address to the
		// sender.
		_ = sendingTask.owner.SendMessageAsync(ctx, wire.StreamBindMessage{
			StreamID: check.inner.ULID,
			Receiver: s.listener.Addr(),
		})
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

// UnregisterManifest removes a manifest from the scheduler.
func (s *Scheduler) UnregisterManifest(ctx context.Context, manifest *workflow.Manifest) error {
	var n notifier
	defer n.Notify(ctx)

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

func (s *Scheduler) deleteTask(t *task) {
	delete(s.tasks, t.inner.ULID)

	if owner := t.owner; owner != nil {
		owner.Unassign(t)
	}
}

// Listen binds the caller as the receiver of the specified stream. Listening on
// a stream prevents tasks from reading from it.
func (s *Scheduler) Listen(ctx context.Context, writer workflow.RecordWriter, stream *workflow.Stream) error {
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
	s.tryBind(ctx, registered)
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

	for _, t := range trackedTasks {
		if t.metadata == nil {
			t.metadata = make(http.Header)
		}

		maps.Copy(t.metadata, metadata)
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
		s.taskQueue = append(s.taskQueue, task)
	}

	if len(s.readyWorkers) > 0 && len(s.taskQueue) > 0 {
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

	return errors.Join(errs...)
}

// UnregisterMetrics unregisters metrics about s from reg.
func (s *Scheduler) UnregisterMetrics(reg prometheus.Registerer) {
	reg.Unregister(s.collector)
	s.metrics.Unregister(reg)
}
