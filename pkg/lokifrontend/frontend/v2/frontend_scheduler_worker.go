package v2

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/middleware"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/pkg/errors"
	"google.golang.org/grpc"

	"github.com/grafana/loki/v3/pkg/scheduler/schedulerpb"
	"github.com/grafana/loki/v3/pkg/util"
)

type frontendSchedulerWorkers struct {
	services.Service

	cfg             Config
	logger          log.Logger
	frontendAddress string

	// Channel with requests that should be forwarded to the scheduler.
	requestsCh <-chan *frontendRequest

	watcher services.Service

	mu sync.Mutex
	// Set to nil when stop is called... no more workers are created afterwards.
	workers map[string]*frontendSchedulerWorker
}

func newFrontendSchedulerWorkers(cfg Config, frontendAddress string, ring ring.ReadRing, requestsCh <-chan *frontendRequest, logger log.Logger) (*frontendSchedulerWorkers, error) {
	f := &frontendSchedulerWorkers{
		cfg:             cfg,
		logger:          logger,
		frontendAddress: frontendAddress,
		requestsCh:      requestsCh,
		workers:         map[string]*frontendSchedulerWorker{},
	}

	switch {
	case ring != nil:
		// Use the scheduler ring and RingWatcher to find schedulers.
		w, err := util.NewRingWatcher(log.With(logger, "component", "frontend-scheduler-worker"), ring, cfg.DNSLookupPeriod, f)
		if err != nil {
			return nil, err
		}
		f.watcher = w
	default:
		// If there is no ring config fallback on using DNS for the frontend scheduler worker to find the schedulers.
		w, err := util.NewDNSWatcher(cfg.SchedulerAddress, cfg.DNSLookupPeriod, f)
		if err != nil {
			return nil, err
		}
		f.watcher = w
	}

	f.Service = services.NewIdleService(f.starting, f.stopping)
	return f, nil
}

func (f *frontendSchedulerWorkers) starting(_ context.Context) error {
	// Instead of re-using `ctx` from the frontendSchedulerWorkers service,
	// `watcher` needs to use their own service context, because we want to
	// control the stopping process in the `stopping` function of the
	// frontendSchedulerWorkers. If we would use the same context, the child
	// service would be stopped automatically as soon as the context of the
	// parent service is cancelled.
	return services.StartAndAwaitRunning(context.Background(), f.watcher)
}

func (f *frontendSchedulerWorkers) stopping(_ error) error {
	err := services.StopAndAwaitTerminated(context.Background(), f.watcher)

	f.mu.Lock()
	defer f.mu.Unlock()

	for _, w := range f.workers {
		w.stop()
	}
	f.workers = nil

	return err
}

func (f *frontendSchedulerWorkers) AddressAdded(address string) {
	f.mu.Lock()
	ws := f.workers
	w := f.workers[address]

	// Already stopped or we already have worker for this address.
	if ws == nil || w != nil {
		f.mu.Unlock()
		return
	}
	f.mu.Unlock()

	level.Info(f.logger).Log("msg", "adding connection to scheduler", "addr", address)
	conn, err := f.connectToScheduler(context.Background(), address)
	if err != nil {
		level.Error(f.logger).Log("msg", "error connecting to scheduler", "addr", address, "err", err)
		return
	}

	// No worker for this address yet, start a new one.
	w = newFrontendSchedulerWorker(conn, address, f.frontendAddress, f.requestsCh, f.cfg.WorkerConcurrency, f.logger)

	f.mu.Lock()
	defer f.mu.Unlock()

	// Can be nil if stopping has been called already.
	if f.workers == nil {
		return
	}
	// We have to recheck for presence in case we got called again while we were
	// connecting and that one finished first.
	if f.workers[address] != nil {
		return
	}
	f.workers[address] = w
	w.start()
}

func (f *frontendSchedulerWorkers) AddressRemoved(address string) {
	level.Info(f.logger).Log("msg", "removing connection to scheduler", "addr", address)

	f.mu.Lock()
	// This works fine if f.workers is nil already.
	w := f.workers[address]
	delete(f.workers, address)
	f.mu.Unlock()

	if w != nil {
		w.stop()
	}
}

// Get number of workers.
func (f *frontendSchedulerWorkers) getWorkersCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return len(f.workers)
}

func (f *frontendSchedulerWorkers) connectToScheduler(ctx context.Context, address string) (*grpc.ClientConn, error) {
	// Because we only use single long-running method, it doesn't make sense to inject user ID, send over tracing or add metrics.
	opts, err := f.cfg.GRPCClientConfig.DialOption(nil, nil, middleware.NoOpInvalidClusterValidationReporter)
	if err != nil {
		return nil, err
	}

	// nolint:staticcheck // grpc.DialContext() has been deprecated; we'll address it before upgrading to gRPC 2.
	conn, err := grpc.DialContext(ctx, address, opts...)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// Worker managing single gRPC connection to Scheduler. Each worker starts multiple goroutines for forwarding
// requests and cancellations to scheduler.
type frontendSchedulerWorker struct {
	log log.Logger

	conn          *grpc.ClientConn
	concurrency   int
	schedulerAddr string
	frontendAddr  string

	// Context and cancellation used by individual goroutines.
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Shared between all frontend workers.
	requestCh <-chan *frontendRequest

	// Cancellation requests for this scheduler are received via this channel. It is passed to frontend after
	// query has been enqueued to scheduler.
	cancelCh chan uint64
}

func newFrontendSchedulerWorker(conn *grpc.ClientConn, schedulerAddr string, frontendAddr string, requestCh <-chan *frontendRequest, concurrency int, log log.Logger) *frontendSchedulerWorker {
	w := &frontendSchedulerWorker{
		log:           log,
		conn:          conn,
		concurrency:   concurrency,
		schedulerAddr: schedulerAddr,
		frontendAddr:  frontendAddr,
		requestCh:     requestCh,
		// Allow to enqueue enough cancellation requests. ~ 8MB memory size.
		cancelCh: make(chan uint64, 1000000),
	}
	w.ctx, w.cancel = context.WithCancel(context.Background())

	return w
}

func (w *frontendSchedulerWorker) start() {
	client := schedulerpb.NewSchedulerForFrontendClient(w.conn)
	for i := 0; i < w.concurrency; i++ {
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			w.runOne(w.ctx, client)
		}()
	}
}

func (w *frontendSchedulerWorker) stop() {
	w.cancel()
	w.wg.Wait()
	if err := w.conn.Close(); err != nil {
		level.Error(w.log).Log("msg", "error while closing connection to scheduler", "err", err)
	}
}

func (w *frontendSchedulerWorker) runOne(ctx context.Context, client schedulerpb.SchedulerForFrontendClient) {
	backoffConfig := backoff.Config{
		MinBackoff: 500 * time.Millisecond,
		MaxBackoff: 5 * time.Second,
	}

	backoff := backoff.New(ctx, backoffConfig)
	for backoff.Ongoing() {
		// Each stream gets its own context, cancelled once we are done with the stream. This releases the
		// stream's resources and ends the goroutine receiving from it (see schedulerLoop).
		loopCtx, loopCancel := context.WithCancel(ctx)

		loop, loopErr := client.FrontendLoop(loopCtx)
		if loopErr != nil {
			loopCancel()
			level.Error(w.log).Log("msg", "error contacting scheduler", "err", loopErr, "addr", w.schedulerAddr)
			backoff.Wait()
			continue
		}

		loopErr = w.schedulerLoop(loop)
		if closeErr := loop.CloseSend(); closeErr != nil {
			level.Debug(w.log).Log("msg", "failed to close frontend loop", "err", loopErr, "addr", w.schedulerAddr)
		}
		loopCancel()

		if loopErr != nil {
			level.Error(w.log).Log("msg", "error sending requests to scheduler", "err", loopErr, "addr", w.schedulerAddr)
			backoff.Wait()
			continue
		}

		backoff.Reset()
	}
}

type schedulerResponse struct {
	resp *schedulerpb.SchedulerToFrontend
	err  error
}

// schedulerLoop forwards requests and cancellations to the scheduler over a single FrontendLoop stream, until
// the stream fails, the scheduler reports that it is shutting down, or the worker is stopped.
//
// Requests that the scheduler accepted through this stream are tracked until they complete. When this function
// returns, those still waiting for a response are failed with a retryable error. The scheduler cancels a
// frontend's requests once that frontend's streams are gone, and drops them when it stops or leaves the
// scheduler ring; in none of these cases is the frontend told, so without this a caller without a deadline
// would wait forever.
func (w *frontendSchedulerWorker) schedulerLoop(loop schedulerpb.SchedulerForFrontend_FrontendLoopClient) error {
	enqueued := newEnqueuedRequests()
	defer enqueued.failAll(w.log, w.schedulerAddr)

	if err := loop.Send(&schedulerpb.FrontendToScheduler{
		Type:            schedulerpb.INIT,
		FrontendAddress: w.frontendAddr,
	}); err != nil {
		return err
	}

	if resp, err := loop.Recv(); err != nil || resp.Status != schedulerpb.OK {
		if err != nil {
			return err
		}
		return errors.Errorf("unexpected status received for init: %v", resp.Status)
	}

	ctx := loop.Context()

	// Receive on a separate goroutine, so that the scheduler ending the stream (or sending an unsolicited
	// SHUTTING_DOWN) is noticed while this worker is idle, not only when it next sends something.
	// runOne cancels the stream context once this function returns, which ends this goroutine.
	responses := make(chan schedulerResponse, 1)
	go func() {
		for {
			resp, err := loop.Recv()
			select {
			case responses <- schedulerResponse{resp: resp, err: err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()

	recv := func() (*schedulerpb.SchedulerToFrontend, error) {
		select {
		case r := <-responses:
			return r.resp, r.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	for {
		select {
		case <-ctx.Done():
			// No need to report error if our internal context is canceled. This can happen during shutdown,
			// or when scheduler is no longer resolvable.
			//
			// Reporting error here would delay reopening the stream (if the worker context is not done yet).
			level.Debug(w.log).Log("msg", "stream context finished", "err", ctx.Err())
			return nil

		case r := <-responses:
			// Nothing is expected from the scheduler while no reply is outstanding. The scheduler sends an
			// unsolicited SHUTTING_DOWN when it leaves the scheduler ring, and ends the stream when it stops.
			if r.err != nil {
				if errors.Is(r.err, io.EOF) {
					return errors.New("scheduler closed the stream")
				}
				return r.err
			}
			if r.resp.Status == schedulerpb.SHUTTING_DOWN {
				return errors.New("scheduler is shutting down")
			}
			return errors.Errorf("unexpected message received from scheduler: %v", r.resp.Status)

		case req := <-w.requestCh:
			msg := &schedulerpb.FrontendToScheduler{
				Type:      schedulerpb.ENQUEUE,
				QueryID:   req.queryID,
				UserID:    req.tenantID,
				QueuePath: req.actor,
				Request: &schedulerpb.FrontendToScheduler_HttpRequest{
					HttpRequest: req.request,
				},
				FrontendAddress: w.frontendAddr,
				StatsEnabled:    req.statsEnabled,
			}

			if req.queryRequest != nil {
				msg.Request = &schedulerpb.FrontendToScheduler_QueryRequest{
					QueryRequest: req.queryRequest,
				}
			}

			err := loop.Send(msg)
			if err != nil {
				req.enqueue <- enqueueResult{status: failed}
				return err
			}

			resp, err := recv()
			if err != nil {
				req.enqueue <- enqueueResult{status: failed}
				return err
			}

			switch resp.Status {
			case schedulerpb.OK:
				// Track the request before handing it back, so that it is failed if this stream ends
				// before its response (which will come from a querier) arrives.
				enqueued.add(req)
				req.enqueue <- enqueueResult{status: waitForResponse, cancelCh: w.cancelCh, done: enqueued.remover(req.queryID)}
				// Response will come from querier.

			case schedulerpb.SHUTTING_DOWN:
				// Scheduler is shutting down, report failure to enqueue and stop this loop.
				req.enqueue <- enqueueResult{status: failed}
				return errors.New("scheduler is shutting down")

			case schedulerpb.ERROR:
				req.enqueue <- enqueueResult{status: waitForResponse}
				req.response <- ResponseTuple{nil, httpgrpc.Errorf(http.StatusInternalServerError, "%s", resp.Error)}
			case schedulerpb.TOO_MANY_REQUESTS_PER_TENANT:
				req.enqueue <- enqueueResult{status: waitForResponse}
				req.response <- ResponseTuple{nil, httpgrpc.Errorf(http.StatusTooManyRequests, "too many outstanding requests")}
			default:
				level.Error(w.log).Log("msg", "unknown response status from the scheduler", "status", resp.Status, "queryID", req.queryID)
				req.enqueue <- enqueueResult{status: failed}
			}

		case reqID := <-w.cancelCh:
			err := loop.Send(&schedulerpb.FrontendToScheduler{
				Type:    schedulerpb.CANCEL,
				QueryID: reqID,
			})
			if err != nil {
				return err
			}

			resp, err := recv()
			if err != nil {
				return err
			}

			// Scheduler may be shutting down, report that.
			if resp.Status != schedulerpb.OK {
				return errors.Errorf("unexpected status received for cancellation: %v", resp.Status)
			}
		}
	}
}

// enqueuedRequests tracks the requests a scheduler accepted through a single FrontendLoop stream that are still
// waiting for their response.
type enqueuedRequests struct {
	mu       sync.Mutex
	requests map[uint64]*frontendRequest
}

func newEnqueuedRequests() *enqueuedRequests {
	return &enqueuedRequests{requests: map[uint64]*frontendRequest{}}
}

func (e *enqueuedRequests) add(req *frontendRequest) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.requests[req.queryID] = req
}

// remover returns a function that stops tracking the request. It is called once the request has completed.
func (e *enqueuedRequests) remover(queryID uint64) func() {
	return func() {
		e.mu.Lock()
		defer e.mu.Unlock()
		delete(e.requests, queryID)
	}
}

func (e *enqueuedRequests) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.requests)
}

// failAll fails all tracked requests with a retryable (5xx) error and stops tracking them.
//
// This includes requests the scheduler may already have dispatched to a querier. A querier runs a request with
// a context derived from its own stream to the scheduler, and the scheduler cancels the request (by closing that
// stream) when the frontend disconnects or when the scheduler stops; in these cases no result will be delivered.
// Should a result still arrive (for example because the scheduler kept the request alive for another stream of
// this frontend), it is dropped: the response channel has a buffer of one and every writer does a non-blocking
// send, so the caller sees whichever arrived first, and a retry uses a new query ID.
func (e *enqueuedRequests) failAll(logger log.Logger, schedulerAddr string) {
	e.mu.Lock()
	requests := e.requests
	e.requests = map[uint64]*frontendRequest{}
	e.mu.Unlock()

	if len(requests) == 0 {
		return
	}

	level.Warn(logger).Log("msg", "connection to scheduler ended while requests were waiting for a response, failing them so they can be retried", "addr", schedulerAddr, "requests", len(requests))

	err := httpgrpc.Errorf(http.StatusServiceUnavailable, "connection to query-scheduler %s ended before the query completed", schedulerAddr)
	for _, req := range requests {
		select {
		case req.response <- ResponseTuple{nil, err}:
		default:
			// A response has already been delivered.
		}
	}
}
