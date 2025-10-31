// Package scheduler provides an implementation of [workflow.Runner] that works
// by scheduling tasks to be executed by a set of workers.
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/oklog/ulid/v2"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

// Config holds configuration options for [Scheduler].
type Config struct {
	// Logger for optional log messages.
	Logger log.Logger
}

// Scheduler is a service that can schedule tasks to connected worker instances.
type Scheduler struct {
	logger log.Logger

	initOnce sync.Once
	svc      services.Service

	// local is the local listener used for communication with workers running
	// within the same process. May be unused if no workers are running locally.
	local *wire.Local

	resourcesMut sync.RWMutex
	streams      map[ulid.ULID]*stream             // All known streams (regardless of state)
	tasks        map[ulid.ULID]*task               // All known tasks (regardless of state)
	workerTasks  map[*wire.Peer]map[*task]struct{} // A map of assigned tasks to peers.

	assignMut    sync.RWMutex
	taskQueue    []*task
	readyWorkers []*wire.Peer

	assignSema chan struct{} // assignSema signals that task assignment is ready.
}

var _ workflow.Runner = (*Scheduler)(nil)

// New creates a new instance of a scheduler. Use [Scheduler.Service] to manage
// the lifecycle of the returned scheduler.
func New(config Config) (*Scheduler, error) {
	if config.Logger == nil {
		config.Logger = log.NewNopLogger()
	}

	return &Scheduler{
		logger: config.Logger,
		local:  &wire.Local{Address: wire.LocalScheduler},

		streams:     make(map[ulid.ULID]*stream),
		tasks:       make(map[ulid.ULID]*task),
		workerTasks: make(map[*wire.Peer]map[*task]struct{}),

		assignSema: make(chan struct{}, 1),
	}, nil
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

	g.Go(func() error { return s.runAcceptLoop(ctx) })
	g.Go(func() error { return s.runAssignLoop(ctx) })

	return g.Wait()
}

func (s *Scheduler) runAcceptLoop(ctx context.Context) error {
	for {
		conn, err := s.local.Accept(ctx)
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

	peer := &wire.Peer{
		Logger: logger,
		Conn:   conn,

		// Allow for a backlog of 128 frames before backpressure is applied.
		Buffer: 128,

		Handler: func(ctx context.Context, peer *wire.Peer, msg wire.Message) error {
			switch msg := msg.(type) {
			case wire.StreamDataMessage:
				return s.handleStreamData(ctx, msg)
			case wire.WorkerReadyMessage:
				return s.markWorkerReady(ctx, peer)
			case wire.TaskStatusMessage:
				return s.handleTaskStatus(ctx, msg)
			case wire.StreamStatusMessage:
				return s.handleStreamStatus(ctx, msg)

			default:
				return fmt.Errorf("unsupported message kind %q", msg.Kind())
			}
		},
	}

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
	s.abortWorkerTasks(ctx, peer, err)
}

func (s *Scheduler) handleStreamData(ctx context.Context, msg wire.StreamDataMessage) error {
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

func (s *Scheduler) markWorkerReady(_ context.Context, worker *wire.Peer) error {
	s.assignMut.Lock()
	defer s.assignMut.Unlock()

	s.readyWorkers = append(s.readyWorkers, worker)

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

func (s *Scheduler) handleTaskStatus(ctx context.Context, msg wire.TaskStatusMessage) error {
	var n notifier
	defer n.Notify(ctx)

	s.resourcesMut.Lock()
	defer s.resourcesMut.Unlock()

	task, found := s.tasks[msg.ID]
	if !found {
		return fmt.Errorf("task %s not found", msg.ID)
	}

	changed, err := task.setState(msg.Status)
	if err != nil {
		return err
	} else if changed {
		// Notify the owner about the change.
		n.AddTaskEvent(taskNotification{
			Handler:   task.handler,
			Task:      task.inner,
			NewStatus: msg.Status,
		})
	}

	// If the task's current state is terminal, we can untrack it now. For
	// simplicity, we lazily check this even if the state hasn't changed.
	if task.status.State.Terminal() {
		s.deleteTask(ctx, &n, task)
	}

	return nil
}

func (s *Scheduler) handleStreamStatus(ctx context.Context, msg wire.StreamStatusMessage) error {
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
	changed, err := target.setState(newState)
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

// abortWorkerTasks immediately aborts all tasks assigned to the given worker.
// abortWorkerTasks should only be used when the worker has disconnected.
//
// If the reason argument is nil, the tasks are cancelled. Otherwise, they are
// marked as failed with the provided reason.
func (s *Scheduler) abortWorkerTasks(ctx context.Context, worker *wire.Peer, reason error) {
	var n notifier
	defer n.Notify(ctx)

	s.resourcesMut.Lock()
	defer s.resourcesMut.Unlock()

	newStatus := workflow.TaskStatus{State: workflow.TaskStateCancelled}
	if reason != nil {
		newStatus = workflow.TaskStatus{
			State: workflow.TaskStateFailed,
			Error: reason,
		}
	}

	for task := range s.workerTasks[worker] {
		if changed, _ := task.setState(newStatus); !changed {
			continue
		}

		s.deleteTask(ctx, &n, task)

		// We only need to inform the handler about the change. There's nothing
		// to send to the owner of the task since worker has disconnected.
		n.AddTaskEvent(taskNotification{
			Handler:   task.handler,
			Task:      task.inner,
			NewStatus: newStatus,
		})
	}

	delete(s.workerTasks, worker)
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
		worker := s.readyWorkers[0]

		// We may have a canceled task in our queue; we take this opportunity to
		// clean them up.
		if state := task.status.State; state.Terminal() {
			s.deleteTask(ctx, &n, task)
			s.taskQueue = s.taskQueue[1:]
			continue
		}

		// Pop the worker immediately. If the worker fails to accept the task,
		// we'll treat it as a failed connection and move on.
		s.readyWorkers = s.readyWorkers[1:]

		level.Debug(s.logger).Log("msg", "assigning task", "id", task.inner.ULID, "conn", worker.RemoteAddr())
		if err := s.assignTask(ctx, task, worker); err != nil && ctx.Err() != nil {
			// Our context got canceled, abort task assignment.
			return
		} else if err != nil {
			level.Warn(s.logger).Log("msg", "failed to assign task", "id", task.inner.ULID, "conn", worker.RemoteAddr(), "err", err)
			continue
		}

		// Pop the task now that it's been officially assigned.
		s.taskQueue = s.taskQueue[1:]
	}
}

func (s *Scheduler) assignTask(ctx context.Context, task *task, worker *wire.Peer) error {
	// TODO(rfratto): allow assignment timeout to be configurable.
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	msg := wire.TaskAssignMessage{
		Task:         task.inner,
		StreamStates: make(map[ulid.ULID]workflow.StreamState),
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
	s.trackAssignment(task, worker)

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

func (s *Scheduler) trackAssignment(assigned *task, owner *wire.Peer) {
	assigned.owner = owner

	tasks := s.workerTasks[owner]
	if tasks == nil {
		tasks = make(map[*task]struct{})
		s.workerTasks[owner] = tasks
	}
	tasks[assigned] = struct{}{}
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
			Receiver: s.local.Address,
		})
	}
}

// DialFrom connects to the scheduler using its local transport. The from
// address denotes the connecting peer.
func (s *Scheduler) DialFrom(ctx context.Context, from net.Addr) (wire.Conn, error) {
	if s == nil || s.local == nil {
		return nil, errors.New("scheduler not initialized")
	}
	return s.local.DialFrom(ctx, from)
}

// AddStreams registers a list of Streams that can be used by Tasks.
// AddStreams returns an error if any of the streams (by ID) are already
// registered.
//
// The provided handler will be called whenever any of the provided streams
// change state.
func (s *Scheduler) AddStreams(_ context.Context, handler workflow.StreamEventHandler, streams ...*workflow.Stream) error {
	s.resourcesMut.Lock()
	defer s.resourcesMut.Unlock()

	var errs []error

	for _, streamToAdd := range streams {
		if _, exist := s.streams[streamToAdd.ULID]; exist {
			errs = append(errs, fmt.Errorf("stream %s already registered", streamToAdd.ULID))
			continue
		} else if streamToAdd.ULID == ulid.Zero {
			errs = append(errs, fmt.Errorf("stream %p has zero-value ULID", streamToAdd))
			continue
		}

		s.streams[streamToAdd.ULID] = &stream{
			inner:   streamToAdd,
			handler: handler,

			state: workflow.StreamStateIdle,
		}
	}

	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

// RemoveStreams removes a list of Streams that can be used by Tasks. The
// associated handler will no longer be called for removed streams.
//
// RemoveStreams returns an error if there are active tasks using the streams.
func (s *Scheduler) RemoveStreams(ctx context.Context, streams ...*workflow.Stream) error {
	var n notifier
	defer n.Notify(ctx)

	s.resourcesMut.Lock()
	defer s.resourcesMut.Unlock()

	var errs []error

	for _, streamToRemove := range streams {
		registered, exist := s.streams[streamToRemove.ULID]
		if !exist {
			errs = append(errs, fmt.Errorf("stream %s not registered", streamToRemove.ULID))
			continue
		}

		// Check whether the stream has non-terminal tasks associated with it.
		// In this case, we don't care if there's a local receiver listening.
		if receiver, ok := s.tasks[registered.taskReceiver]; ok && !receiver.status.State.Terminal() {
			errs = append(errs, fmt.Errorf("stream %s has active tasks", streamToRemove.ULID))
			continue
		} else if sender, ok := s.tasks[registered.taskSender]; ok && !sender.status.State.Terminal() {
			errs = append(errs, fmt.Errorf("stream %s has active tasks", streamToRemove.ULID))
			continue
		}

		changed, _ := registered.setState(workflow.StreamStateClosed)
		if changed {
			n.AddStreamEvent(streamNotification{
				Handler:  registered.handler,
				Stream:   streamToRemove,
				NewState: workflow.StreamStateClosed,
			})
		}

		delete(s.streams, streamToRemove.ULID)
	}

	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

// Listen binds the caller as the receiver of the specified stream. Listening on
// a stream prevents tasks from reading from it.
func (s *Scheduler) Listen(ctx context.Context, stream *workflow.Stream) (executor.Pipeline, error) {
	s.resourcesMut.Lock()
	defer s.resourcesMut.Unlock()

	registered, found := s.streams[stream.ULID]
	if !found {
		return nil, fmt.Errorf("stream %s not registered", stream.ULID)
	}

	// Create a pipe for the caller to receive results.
	pipe := newStreamPipe()
	if err := registered.setLocalListener(pipe); err != nil {
		return nil, err
	}

	if registered.state == workflow.StreamStateClosed {
		// If we tried listening to a closed stream, we can immediately close
		// the pipe so that the first call to Read returns EOF.
		//
		// This seems preferable to considering listening to a closed stream as
		// an error and avoiding potential race conditions where sometimes
		// Listen fails.
		pipe.Close()
	}

	// Usually, address binding is handled upon task assignment in
	// [Scheduler.assignTask]. However, calls to Listen can happen after the
	// sending task is already assigned. In this case, we'll attempt to bind
	// now.
	s.tryBind(ctx, registered)
	return pipe, nil
}

// Start begins executing the provided tasks in the background. Start returns an
// error if any of the Tasks references an unregistered Stream, or if any of the
// tasks are a reader of a stream that's already bound.
//
// The provided handler will be called whenever any of the provided tasks change
// state.
//
// Canceling the provided context does not cancel started tasks. Use
// [Scheduler.Cancel] to cancel started tasks.
func (s *Scheduler) Start(ctx context.Context, handler workflow.TaskEventHandler, tasks ...*workflow.Task) error {
	createdTasks, err := s.registerTasks(ctx, handler, tasks...)
	if len(createdTasks) > 0 {
		// Even if registerTasks returned an error, we may have still created
		// some tasks successfully, and we'll want to enqueue those.
		s.enqueueTasks(createdTasks)
	}
	return err
}

// registerTasks registers the provided tasks with the scheduler without
// enqueuing them.
func (s *Scheduler) registerTasks(ctx context.Context, handler workflow.TaskEventHandler, tasks ...*workflow.Task) ([]*task, error) {
	var n notifier
	defer n.Notify(ctx)

	newTasks := make([]*task, 0, len(tasks))

	s.resourcesMut.Lock()
	defer s.resourcesMut.Unlock()

	var errs []error

NextTask:
	for _, taskToStart := range tasks {
		if _, exist := s.tasks[taskToStart.ULID]; exist {
			errs = append(errs, fmt.Errorf("task %s already exists", taskToStart.ULID))
			continue
		}

		for _, neededStreams := range taskToStart.Sources {
			for _, neededStream := range neededStreams {
				sourceStream, exist := s.streams[neededStream.ULID]
				if !exist {
					errs = append(errs, fmt.Errorf("source stream %s not found", neededStream))
					continue NextTask
				} else if err := sourceStream.setTaskReceiver(taskToStart.ULID); err != nil {
					errs = append(errs, err)
					continue NextTask
				}
			}
		}
		for _, neededStreams := range taskToStart.Sinks {
			for _, neededStream := range neededStreams {
				sinkStream, exist := s.streams[neededStream.ULID]
				if !exist {
					errs = append(errs, fmt.Errorf("sink stream %s not found", neededStream))
					continue NextTask
				} else if err := sinkStream.setTaskSender(taskToStart.ULID); err != nil {
					errs = append(errs, err)
					continue NextTask
				}
			}
		}

		newTask := &task{
			inner:   taskToStart,
			handler: handler,

			// We initialize the status as Pending, which is an implicit
			// transition from the default Created state.
			status: workflow.TaskStatus{State: workflow.TaskStatePending},
		}
		s.tasks[taskToStart.ULID] = newTask

		newTasks = append(newTasks, newTask)

		// Inform the owner about the state change from Created to Pending.
		n.AddTaskEvent(taskNotification{
			Handler:   handler,
			Task:      taskToStart,
			NewStatus: newTask.status,
		})
	}

	if len(errs) == 0 {
		return newTasks, nil
	}
	return newTasks, errors.Join(errs...)
}

func (s *Scheduler) enqueueTasks(tasks []*task) {
	s.assignMut.Lock()
	defer s.assignMut.Unlock()

	s.taskQueue = append(s.taskQueue, tasks...)

	if len(s.readyWorkers) > 0 && len(s.taskQueue) > 0 {
		nudgeSemaphore(s.assignSema)
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

		// Immediately clean up our own resources.
		s.deleteTask(ctx, &n, registered)

		if changed, _ := registered.setState(workflow.TaskStatus{State: workflow.TaskStateCancelled}); !changed {
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
			Task:      taskToCancel,
			NewStatus: registered.status,
		})
	}

	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

func (s *Scheduler) deleteTask(ctx context.Context, n *notifier, t *task) {
	delete(s.tasks, t.inner.ULID)

	if owner := t.owner; owner != nil {
		knownTasks := s.workerTasks[owner]
		if knownTasks != nil {
			delete(knownTasks, t)
		}
	}

	// Close all associated sink streams.
	for _, sinks := range t.inner.Sinks {
		for _, rawSink := range sinks {
			sink, ok := s.streams[rawSink.ULID]
			if !ok {
				continue
			}

			// changeStreamState only returns an error for an invalid state
			// change, which isn't possible here (it's never invalid to move
			// to Closed, only a no-op if it's already Closed).
			_ = s.changeStreamState(ctx, n, sink, workflow.StreamStateClosed)
		}
	}
}
