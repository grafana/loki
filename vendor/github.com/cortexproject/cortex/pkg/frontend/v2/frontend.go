package v2

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/weaveworks/common/httpgrpc"
	"go.uber.org/atomic"

	"github.com/cortexproject/cortex/pkg/frontend/v2/frontendv2pb"
	"github.com/cortexproject/cortex/pkg/querier/stats"
	"github.com/cortexproject/cortex/pkg/tenant"
	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/cortexproject/cortex/pkg/util/grpcclient"
	"github.com/cortexproject/cortex/pkg/util/grpcutil"
	"github.com/cortexproject/cortex/pkg/util/services"
)

// Config for a Frontend.
type Config struct {
	SchedulerAddress  string            `yaml:"scheduler_address"`
	DNSLookupPeriod   time.Duration     `yaml:"scheduler_dns_lookup_period"`
	WorkerConcurrency int               `yaml:"scheduler_worker_concurrency"`
	GRPCClientConfig  grpcclient.Config `yaml:"grpc_client_config"`

	// Used to find local IP address, that is sent to scheduler and querier-worker.
	InfNames []string `yaml:"instance_interface_names"`

	// If set, address is not computed from interfaces.
	Addr string `yaml:"address" doc:"hidden"`
	Port int    `doc:"hidden"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.SchedulerAddress, "frontend.scheduler-address", "", "DNS hostname used for finding query-schedulers.")
	f.DurationVar(&cfg.DNSLookupPeriod, "frontend.scheduler-dns-lookup-period", 10*time.Second, "How often to resolve the scheduler-address, in order to look for new query-scheduler instances.")
	f.IntVar(&cfg.WorkerConcurrency, "frontend.scheduler-worker-concurrency", 5, "Number of concurrent workers forwarding queries to single query-scheduler.")

	cfg.InfNames = []string{"eth0", "en0"}
	f.Var((*flagext.StringSlice)(&cfg.InfNames), "frontend.instance-interface-names", "Name of network interface to read address from. This address is sent to query-scheduler and querier, which uses it to send the query response back to query-frontend.")
	f.StringVar(&cfg.Addr, "frontend.instance-addr", "", "IP address to advertise to querier (via scheduler) (resolved via interfaces by default).")
	f.IntVar(&cfg.Port, "frontend.instance-port", 0, "Port to advertise to querier (via scheduler) (defaults to server.grpc-listen-port).")

	cfg.GRPCClientConfig.RegisterFlagsWithPrefix("frontend.grpc-client-config", f)
}

// Frontend implements GrpcRoundTripper. It queues HTTP requests,
// dispatches them to backends via gRPC, and handles retries for requests which failed.
type Frontend struct {
	services.Service

	cfg Config
	log log.Logger

	lastQueryID atomic.Uint64

	// frontend workers will read from this channel, and send request to scheduler.
	requestsCh chan *frontendRequest

	schedulerWorkers *frontendSchedulerWorkers
	requests         *requestsInProgress
}

type frontendRequest struct {
	queryID      uint64
	request      *httpgrpc.HTTPRequest
	userID       string
	statsEnabled bool

	cancel context.CancelFunc

	enqueue  chan enqueueResult
	response chan *frontendv2pb.QueryResultRequest
}

type enqueueStatus int

const (
	// Sent to scheduler successfully, and frontend should wait for response now.
	waitForResponse enqueueStatus = iota

	// Failed to forward request to scheduler, frontend will try again.
	failed
)

type enqueueResult struct {
	status enqueueStatus

	cancelCh chan<- uint64 // Channel that can be used for request cancellation. If nil, cancellation is not possible.
}

// NewFrontend creates a new frontend.
func NewFrontend(cfg Config, log log.Logger, reg prometheus.Registerer) (*Frontend, error) {
	requestsCh := make(chan *frontendRequest)

	schedulerWorkers, err := newFrontendSchedulerWorkers(cfg, fmt.Sprintf("%s:%d", cfg.Addr, cfg.Port), requestsCh, log)
	if err != nil {
		return nil, err
	}

	f := &Frontend{
		cfg:              cfg,
		log:              log,
		requestsCh:       requestsCh,
		schedulerWorkers: schedulerWorkers,
		requests:         newRequestsInProgress(),
	}
	// Randomize to avoid getting responses from queries sent before restart, which could lead to mixing results
	// between different queries. Note that frontend verifies the user, so it cannot leak results between tenants.
	// This isn't perfect, but better than nothing.
	f.lastQueryID.Store(rand.Uint64())

	promauto.With(reg).NewGaugeFunc(prometheus.GaugeOpts{
		Name: "cortex_query_frontend_queries_in_progress",
		Help: "Number of queries in progress handled by this frontend.",
	}, func() float64 {
		return float64(f.requests.count())
	})

	promauto.With(reg).NewGaugeFunc(prometheus.GaugeOpts{
		Name: "cortex_query_frontend_connected_schedulers",
		Help: "Number of schedulers this frontend is connected to.",
	}, func() float64 {
		return float64(f.schedulerWorkers.getWorkersCount())
	})

	f.Service = services.NewIdleService(f.starting, f.stopping)
	return f, nil
}

func (f *Frontend) starting(ctx context.Context) error {
	return errors.Wrap(services.StartAndAwaitRunning(ctx, f.schedulerWorkers), "failed to start frontend scheduler workers")
}

func (f *Frontend) stopping(_ error) error {
	return errors.Wrap(services.StopAndAwaitTerminated(context.Background(), f.schedulerWorkers), "failed to stop frontend scheduler workers")
}

// RoundTripGRPC round trips a proto (instead of a HTTP request).
func (f *Frontend) RoundTripGRPC(ctx context.Context, req *httpgrpc.HTTPRequest) (*httpgrpc.HTTPResponse, error) {
	if s := f.State(); s != services.Running {
		return nil, fmt.Errorf("frontend not running: %v", s)
	}

	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return nil, err
	}
	userID := tenant.JoinTenantIDs(tenantIDs)

	// Propagate trace context in gRPC too - this will be ignored if using HTTP.
	tracer, span := opentracing.GlobalTracer(), opentracing.SpanFromContext(ctx)
	if tracer != nil && span != nil {
		carrier := (*grpcutil.HttpgrpcHeadersCarrier)(req)
		if err := tracer.Inject(span.Context(), opentracing.HTTPHeaders, carrier); err != nil {
			return nil, err
		}
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	freq := &frontendRequest{
		queryID:      f.lastQueryID.Inc(),
		request:      req,
		userID:       userID,
		statsEnabled: stats.IsEnabled(ctx),

		cancel: cancel,

		// Buffer of 1 to ensure response or error can be written to the channel
		// even if this goroutine goes away due to client context cancellation.
		enqueue:  make(chan enqueueResult, 1),
		response: make(chan *frontendv2pb.QueryResultRequest, 1),
	}

	f.requests.put(freq)
	defer f.requests.delete(freq.queryID)

	retries := f.cfg.WorkerConcurrency + 1 // To make sure we hit at least two different schedulers.

enqueueAgain:
	select {
	case <-ctx.Done():
		return nil, ctx.Err()

	case f.requestsCh <- freq:
		// Enqueued, let's wait for response.
	}

	var cancelCh chan<- uint64

	select {
	case <-ctx.Done():
		return nil, ctx.Err()

	case enqRes := <-freq.enqueue:
		if enqRes.status == waitForResponse {
			cancelCh = enqRes.cancelCh
			break // go wait for response.
		} else if enqRes.status == failed {
			retries--
			if retries > 0 {
				goto enqueueAgain
			}
		}

		return nil, httpgrpc.Errorf(http.StatusInternalServerError, "failed to enqueue request")
	}

	select {
	case <-ctx.Done():
		if cancelCh != nil {
			select {
			case cancelCh <- freq.queryID:
				// cancellation sent.
			default:
				// failed to cancel, ignore.
			}
		}
		return nil, ctx.Err()

	case resp := <-freq.response:
		if stats.ShouldTrackHTTPGRPCResponse(resp.HttpResponse) {
			stats := stats.FromContext(ctx)
			stats.Merge(resp.Stats) // Safe if stats is nil.
		}

		return resp.HttpResponse, nil
	}
}

func (f *Frontend) QueryResult(ctx context.Context, qrReq *frontendv2pb.QueryResultRequest) (*frontendv2pb.QueryResultResponse, error) {
	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return nil, err
	}
	userID := tenant.JoinTenantIDs(tenantIDs)

	req := f.requests.get(qrReq.QueryID)
	// It is possible that some old response belonging to different user was received, if frontend has restarted.
	// To avoid leaking query results between users, we verify the user here.
	// To avoid mixing results from different queries, we randomize queryID counter on start.
	if req != nil && req.userID == userID {
		select {
		case req.response <- qrReq:
			// Should always be possible, unless QueryResult is called multiple times with the same queryID.
		default:
			level.Warn(f.log).Log("msg", "failed to write query result to the response channel", "queryID", qrReq.QueryID, "user", userID)
		}
	}

	return &frontendv2pb.QueryResultResponse{}, nil
}

// CheckReady determines if the query frontend is ready.  Function parameters/return
// chosen to match the same method in the ingester
func (f *Frontend) CheckReady(_ context.Context) error {
	workers := f.schedulerWorkers.getWorkersCount()

	// If frontend is connected to at least one scheduler, we are ready.
	if workers > 0 {
		return nil
	}

	msg := fmt.Sprintf("not ready: number of schedulers this worker is connected to is %d", workers)
	level.Info(f.log).Log("msg", msg)
	return errors.New(msg)
}

type requestsInProgress struct {
	mu       sync.Mutex
	requests map[uint64]*frontendRequest
}

func newRequestsInProgress() *requestsInProgress {
	return &requestsInProgress{
		requests: map[uint64]*frontendRequest{},
	}
}

func (r *requestsInProgress) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return len(r.requests)
}

func (r *requestsInProgress) put(req *frontendRequest) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.requests[req.queryID] = req
}

func (r *requestsInProgress) delete(queryID uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.requests, queryID)
}

func (r *requestsInProgress) get(queryID uint64) *frontendRequest {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.requests[queryID]
}
