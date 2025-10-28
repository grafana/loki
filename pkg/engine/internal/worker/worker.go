// Package worker provides a mechanism to to connect to the [scheduler] for
// executing tasks.
package worker

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"runtime"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/services"
	"github.com/oklog/ulid/v2"
	"github.com/thanos-io/objstore"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

// Config holds configuration options for [Worker].
type Config struct {
	// Logger for optional log messages.
	Logger log.Logger

	// Bucket to read stored data from.
	Bucket objstore.Bucket

	// Local scheduler to connect to. If LocalScheduler is nil, the worker can
	// still connect to remote schedulers.
	LocalScheduler *scheduler.Scheduler

	// BatchSize specifies the maximum number of rows to retrieve in a single
	// read call of a task pipeline.
	BatchSize int64

	// NumThreads is the number of worker threads to spawn. The number of
	// threads corresponds to the number of tasks that can be executed
	// concurrently.
	//
	// If NumThreads is set to 0, NumThreads defaults to [runtime.GOMAXPROCS].
	NumThreads int
}

// readyRequest is a message sent from a thread to notify the worker that it's
// ready for a task.
type readyRequest struct {
	// Context associated with the request. Jobs created from this request
	// should use this context.
	Context context.Context

	// Response is the channel to send assigned tasks to. Response must be a
	// buffered channel with at least one slot.
	Response chan readyResponse
}

// readyResponse is the response for a readyRequest.
type readyResponse struct {
	Job *threadJob

	// Error is set if the task failed to be assigned, including when the
	// connection to a chosen scheduler has been lost. Threads should use this
	// to request a new task.
	Error error
}

// Worker requests tasks from a set of [scheduler.Scheduler] instances and
// executes them. Task results are forwarded along streams, which are received
// by other [Worker] instances or the scheduler.
type Worker struct {
	config Config
	logger log.Logger

	initOnce sync.Once
	svc      services.Service

	// local is the local listener used for communication between worker
	// threads within the same process.
	local *wire.Local

	resourcesMut sync.RWMutex
	sources      map[ulid.ULID]*streamSource
	sinks        map[ulid.ULID]*streamSink
	jobs         map[ulid.ULID]*threadJob

	readyCh chan readyRequest // Written to whenever a thread is ready for a task.
}

// New creates a new instance of a worker. Use [Worker.Service] to manage
// the lifecycle of the returned worker.
//
// New returns an error if the provided config is invalid.
func New(config Config) (*Worker, error) {
	if config.Logger == nil {
		config.Logger = log.NewNopLogger()
	}

	// TODO(rfratto): support connecting to a remote scheduler, so that a local
	// scheduler isn't always required.
	if config.LocalScheduler == nil {
		return nil, errors.New("local scheduler is required")
	}

	return &Worker{
		config: config,
		logger: config.Logger,

		local: &wire.Local{Address: wire.LocalWorker},

		sources: make(map[ulid.ULID]*streamSource),
		sinks:   make(map[ulid.ULID]*streamSink),
		jobs:    make(map[ulid.ULID]*threadJob),

		readyCh: make(chan readyRequest),
	}, nil
}

// Service returns the service used to manage the lifecycle of the worker.
func (w *Worker) Service() services.Service {
	w.initOnce.Do(func() {
		w.svc = services.NewBasicService(nil, w.run, nil)
	})

	return w.svc
}

// run starts the worker, running until the provided context is canceled.
func (w *Worker) run(ctx context.Context) error {
	numThreads := w.config.NumThreads
	if numThreads == 0 {
		numThreads = runtime.GOMAXPROCS(0)
	}

	g, ctx := errgroup.WithContext(ctx)

	// Spin up worker threads.
	for i := range numThreads {
		t := &thread{
			BatchSize: w.config.BatchSize,
			Logger:    log.With(w.logger, "thread", i),
			Bucket:    w.config.Bucket,

			Ready: w.readyCh,
		}

		g.Go(func() error { return t.Run(ctx) })
	}

	g.Go(func() error { return w.runAcceptLoop(ctx) })
	g.Go(func() error { return w.runSchedulersLoop(ctx) })

	return g.Wait()
}

// runAcceptLoop handles incoming connections from peers. Incoming connections
// are exclusively used to receive task results from other workers, or between
// threads within this worker.
func (w *Worker) runAcceptLoop(ctx context.Context) error {
	for {
		conn, err := w.local.Accept(ctx)
		if err != nil && ctx.Err() != nil {
			return nil
		} else if err != nil {
			level.Warn(w.logger).Log("msg", "failed to accept connection", "err", err)
			continue
		}

		go w.handleConn(ctx, conn)
	}
}

func (w *Worker) handleConn(ctx context.Context, conn wire.Conn) {
	logger := log.With(w.logger, "remote_addr", conn.RemoteAddr())
	level.Info(logger).Log("msg", "handling connection")

	peer := &wire.Peer{
		Logger: logger,
		Conn:   conn,

		// Allow for a backlog of 128 frames before backpressure is applied.
		Buffer: 128,

		Handler: func(ctx context.Context, _ *wire.Peer, msg wire.Message) error {
			switch msg := msg.(type) {
			case wire.StreamDataMessage:
				return w.handleDataMessage(ctx, msg)
			default:
				return fmt.Errorf("unsupported message type %T", msg)
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
}

// runSchedulersLoop periodically discovers schedulers and maintains connections
// to them.
func (w *Worker) runSchedulersLoop(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx)

	// TODO(rfratto): If connecting to remote schedulers, discover them here
	// (DNS lookup to get all the pods?) and maintain connections to them.
	//
	// TODO(rfratto): Once we support multiple schedulers, we'll want
	// per-scheduler contexts to be able to cancel individual connections (for
	// when a scheduler disappears from discovery).

	if w.config.LocalScheduler != nil {
		g.Go(func() error { return w.schedulerLoop(ctx, wire.LocalScheduler) })
	}
	return nil
}

// schedulerLoop manages the lifecycle of a connection to a specific scheduler.
func (w *Worker) schedulerLoop(ctx context.Context, addr net.Addr) error {
	logger := log.With(w.logger, "remote_addr", addr)

	bo := backoff.New(ctx, backoff.Config{
		MinBackoff: 100 * time.Millisecond,
		MaxBackoff: 1 * time.Second,
		MaxRetries: 0, // Never stop retrying
	})

	for bo.Ongoing() {
		conn, err := w.dial(ctx, addr)
		if err != nil {
			level.Warn(logger).Log("msg", "dial to scheduler failed, will retry after backoff", "err", err)
			bo.Wait()
			continue
		}

		// We only want to increase the backoff for failed dials rather than
		// terminated connections, so we reset it as long as the dial succeeds.
		bo.Reset()

		if err := w.handleSchedulerConn(ctx, logger, conn); err != nil && ctx.Err() != nil {
			level.Warn(logger).Log("msg", "connection to scheduler closed; will reconnect after backoff", "err", err)
			bo.Wait()
			continue
		}
	}

	return nil
}

// dial opens a connection to the given address.
func (w *Worker) dial(ctx context.Context, addr net.Addr) (wire.Conn, error) {
	switch addr {
	case wire.LocalScheduler:
		return w.config.LocalScheduler.DialFrom(ctx, w.local.Address)
	case wire.LocalWorker:
		return w.local.DialFrom(ctx, w.local.Address)
	default:
		// TODO(rfratto): Support for network transports
		return nil, fmt.Errorf("unsupported address %s", addr)
	}
}

// handleSchedulerConn handles a single connection to a scheduler.
func (w *Worker) handleSchedulerConn(ctx context.Context, logger log.Logger, conn wire.Conn) error {
	level.Info(logger).Log("msg", "connected to scheduler")

	var (
		reqsMut   sync.Mutex
		readyReqs []readyRequest
	)

	cancelRequests := func(err error) {
		reqsMut.Lock()
		defer reqsMut.Unlock()

		for _, req := range readyReqs {
			req.Response <- readyResponse{Error: err}
		}
	}

	handleAssignment := func(peer *wire.Peer, msg wire.TaskAssignMessage) error {
		reqsMut.Lock()
		defer reqsMut.Unlock()

		req := readyReqs[0]
		readyReqs = readyReqs[1:]

		job, err := w.newJob(req.Context, peer, logger, msg)
		if err != nil {
			return err
		}

		// Response is guaranteed to be buffered so we don't need to worry about
		// blocking here.
		req.Response <- readyResponse{Job: job}
		return nil
	}

	requestAssignment := func(ctx context.Context, peer *wire.Peer, req readyRequest) {
		reqsMut.Lock()
		defer reqsMut.Unlock()

		// Keep the lock held while we send the request to the scheduler. Once
		// we get the acknowledgement back we can move req to the readyReqs
		// stack.
		err := peer.SendMessage(ctx, wire.WorkerReadyMessage{})
		if err != nil {
			// Response is guaranteed to be buffered so we don't need to worry about
			// blocking here.
			req.Response <- readyResponse{Error: err}
			return
		}

		readyReqs = append(readyReqs, req)
	}

	peer := &wire.Peer{
		Logger: logger,
		Conn:   conn,

		// Allow for a backlog of 128 frames before backpressure is applied.
		Buffer: 128,

		Handler: func(ctx context.Context, peer *wire.Peer, msg wire.Message) error {
			switch msg := msg.(type) {
			case wire.TaskAssignMessage:
				return handleAssignment(peer, msg)

			case wire.TaskCancelMessage:
				return w.handleCancelMessage(ctx, msg)

			case wire.StreamBindMessage:
				return w.handleBindMessage(ctx, msg)

			case wire.StreamStatusMessage:
				return w.handleStreamStatusMessage(ctx, msg)

			default:
				level.Warn(logger).Log("msg", "unsupported message type", "type", reflect.TypeOf(msg).String())
				return fmt.Errorf("unsupported message type %T", msg)
			}
		},
	}

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error { return peer.Serve(ctx) })

	g.Go(func() error {
		for {
			select {
			case <-ctx.Done():
				return nil
			case req := <-w.readyCh:
				requestAssignment(ctx, peer, req)
			}
		}
	})

	// Wait for all worker goroutines to exit. Before we return, we'll clean up
	// any pending ready requests to tell them that they won't receive a task.
	err := g.Wait()
	cancelRequests(err)

	level.Info(logger).Log("msg", "disconnected from scheduler", "err", err)
	return err
}

// newJob creates a new job for the given assigned task. The job will have a
// context bound to the provided ctx.
//
// newJob returns an error if there was a ULID collision for the job or any of
// its streams.
func (w *Worker) newJob(ctx context.Context, scheduler *wire.Peer, logger log.Logger, msg wire.TaskAssignMessage) (job *threadJob, err error) {
	w.resourcesMut.Lock()
	defer w.resourcesMut.Unlock()

	var (
		sources = make(map[ulid.ULID]*streamSource)
		sinks   = make(map[ulid.ULID]*streamSink)
	)
	defer func() {
		if err == nil {
			return
		}

		// If we happened to run into a ULID collision while creating the job,
		// we want to clean up any streams we stored in the maps.
		for sourceID := range sources {
			delete(w.sources, sourceID)
		}
		for sinkID := range sinks {
			delete(w.sinks, sinkID)
		}
	}()

	if w.sources == nil {
		w.sources = make(map[ulid.ULID]*streamSource)
	}
	if w.sinks == nil {
		w.sinks = make(map[ulid.ULID]*streamSink)
	}

	for _, taskSources := range msg.Task.Sources {
		for _, taskSource := range taskSources {
			source := new(streamSource)

			// We keep all sources in the job for consistency, but it's possible
			// that we got assigned a task with a stream that's already closed.
			//
			// To handle this, we immediately close the source before adding it
			// to the job. This will ensure that the stream returns EOF
			// immediately and doesn't block forever.
			if msg.StreamStates != nil && msg.StreamStates[taskSource.ULID] == workflow.StreamStateClosed {
				source.Close()
			}

			if _, exist := w.sources[taskSource.ULID]; exist {
				return nil, fmt.Errorf("source %s already exists", taskSource.ULID)
			}

			w.sources[taskSource.ULID] = source
			sources[taskSource.ULID] = source
		}
	}

	for _, taskSinks := range msg.Task.Sinks {
		for _, taskSink := range taskSinks {
			sink := &streamSink{
				Logger:    log.With(logger, "stream", taskSink.ULID),
				Scheduler: scheduler,
				Stream:    taskSink,
				Dialer:    w.dial,
			}

			if _, exist := w.sinks[taskSink.ULID]; exist {
				return nil, fmt.Errorf("sink %s already exists", taskSink.ULID)
			}

			w.sinks[taskSink.ULID] = sink
			sinks[taskSink.ULID] = sink
		}
	}

	ctx, cancel := context.WithCancel(ctx)

	job = &threadJob{
		Context: ctx,
		Cancel:  cancel,

		Scheduler: scheduler,
		Task:      msg.Task,

		Sources: sources,
		Sinks:   sinks,

		Close: func() {
			w.resourcesMut.Lock()
			defer w.resourcesMut.Unlock()

			for sourceID := range sources {
				delete(w.sources, sourceID)
			}
			for sinkID := range sinks {
				delete(w.sinks, sinkID)
			}

			delete(w.jobs, msg.Task.ULID)
		},
	}

	if _, exist := w.jobs[msg.Task.ULID]; exist {
		return nil, fmt.Errorf("job %s already exists", msg.Task.ULID)
	}

	w.jobs[msg.Task.ULID] = job
	return job, nil
}

func (w *Worker) handleCancelMessage(_ context.Context, msg wire.TaskCancelMessage) error {
	w.resourcesMut.RLock()
	job, found := w.jobs[msg.ID]
	w.resourcesMut.RUnlock()

	if !found {
		return fmt.Errorf("task %s not found", msg.ID)
	}

	job.Cancel()
	return nil
}

func (w *Worker) handleBindMessage(ctx context.Context, msg wire.StreamBindMessage) error {
	w.resourcesMut.RLock()
	sink, found := w.sinks[msg.StreamID]
	w.resourcesMut.RUnlock()

	if !found {
		return fmt.Errorf("stream %s not found", msg.StreamID)
	}
	return sink.Bind(ctx, msg.Receiver)
}

func (w *Worker) handleStreamStatusMessage(_ context.Context, msg wire.StreamStatusMessage) error {
	w.resourcesMut.RLock()
	source, found := w.sources[msg.StreamID]
	w.resourcesMut.RUnlock()

	if !found {
		return fmt.Errorf("stream %s not found", msg.StreamID)
	}

	// At the moment, workers only care about the stream being closed.
	if msg.State == workflow.StreamStateClosed {
		source.Close()
	}
	return nil
}

func (w *Worker) handleDataMessage(ctx context.Context, msg wire.StreamDataMessage) error {
	w.resourcesMut.RLock()
	source, found := w.sources[msg.StreamID]
	w.resourcesMut.RUnlock()

	if !found {
		return fmt.Errorf("stream %s not found for receiving data", msg.StreamID)
	}
	return source.Write(ctx, msg.Data)
}
