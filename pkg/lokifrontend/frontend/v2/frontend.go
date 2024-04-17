package v2

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/grpcclient"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/netutil"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/atomic"

	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/v3/pkg/lokifrontend/frontend/transport"
	"github.com/grafana/loki/v3/pkg/lokifrontend/frontend/v2/frontendv2pb"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/querier/stats"
	lokigrpc "github.com/grafana/loki/v3/pkg/util/httpgrpc"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

const (
	EncodingJSON     = "json"
	EncodingProtobuf = "protobuf"
)

// Config for a Frontend.
type Config struct {
	SchedulerAddress        string            `yaml:"scheduler_address"`
	DNSLookupPeriod         time.Duration     `yaml:"scheduler_dns_lookup_period"`
	WorkerConcurrency       int               `yaml:"scheduler_worker_concurrency"`
	GRPCClientConfig        grpcclient.Config `yaml:"grpc_client_config"`
	GracefulShutdownTimeout time.Duration     `yaml:"graceful_shutdown_timeout"`

	// Used to find local IP address, that is sent to scheduler and querier-worker.
	InfNames []string `yaml:"instance_interface_names" doc:"default=[<private network interfaces>]"`

	// If set, address is not computed from interfaces.
	Addr string `yaml:"address" doc:"hidden"`
	Port int    `doc:"hidden"`

	// Defines the encoding for requests to and responses from the scheduler and querier.
	Encoding string `yaml:"encoding"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.SchedulerAddress, "frontend.scheduler-address", "", "DNS hostname used for finding query-schedulers.")
	f.DurationVar(&cfg.DNSLookupPeriod, "frontend.scheduler-dns-lookup-period", 10*time.Second, "How often to resolve the scheduler-address, in order to look for new query-scheduler instances. Also used to determine how often to poll the scheduler-ring for addresses if the scheduler-ring is configured.")
	f.IntVar(&cfg.WorkerConcurrency, "frontend.scheduler-worker-concurrency", 5, "Number of concurrent workers forwarding queries to single query-scheduler.")
	f.DurationVar(&cfg.GracefulShutdownTimeout, "frontend.graceful-shutdown-timeout", 5*time.Minute, "Time to wait for inflight requests to finish before forcefully shutting down. This needs to be aligned with the query timeout and the graceful termination period of the process orchestrator.")

	cfg.InfNames = netutil.PrivateNetworkInterfacesWithFallback([]string{"eth0", "en0"}, util_log.Logger)
	f.Var((*flagext.StringSlice)(&cfg.InfNames), "frontend.instance-interface-names", "Name of network interface to read address from. This address is sent to query-scheduler and querier, which uses it to send the query response back to query-frontend.")
	f.StringVar(&cfg.Addr, "frontend.instance-addr", "", "IP address to advertise to querier (via scheduler) (resolved via interfaces by default).")
	f.IntVar(&cfg.Port, "frontend.instance-port", 0, "Port to advertise to querier (via scheduler) (defaults to server.grpc-listen-port).")

	cfg.GRPCClientConfig.RegisterFlagsWithPrefix("frontend.grpc-client-config", f)

	f.StringVar(&cfg.Encoding, "frontend.encoding", "json", "Defines the encoding for requests to and responses from the scheduler and querier. Can be 'json' or 'protobuf' (defaults to 'json').")
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

	codec transport.Codec
}

var _ queryrangebase.Handler = &Frontend{}
var _ transport.GrpcRoundTripper = &Frontend{}

type ResponseTuple = struct {
	*frontendv2pb.QueryResultRequest
	error
}

type frontendRequest struct {
	queryID      uint64
	request      *httpgrpc.HTTPRequest
	queryRequest *queryrange.QueryRequest
	tenantID     string
	actor        []string
	statsEnabled bool

	cancel context.CancelFunc

	enqueue  chan enqueueResult
	response chan ResponseTuple
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
func NewFrontend(cfg Config, ring ring.ReadRing, log log.Logger, reg prometheus.Registerer, codec transport.Codec, metricsNamespace string) (*Frontend, error) {
	requestsCh := make(chan *frontendRequest)

	schedulerWorkers, err := newFrontendSchedulerWorkers(cfg, net.JoinHostPort(cfg.Addr, strconv.Itoa(cfg.Port)), ring, requestsCh, log)
	if err != nil {
		return nil, err
	}

	f := &Frontend{
		cfg:              cfg,
		log:              log,
		requestsCh:       requestsCh,
		schedulerWorkers: schedulerWorkers,
		requests:         newRequestsInProgress(),
		codec:            codec,
	}
	// Randomize to avoid getting responses from queries sent before restart, which could lead to mixing results
	// between different queries. Note that frontend verifies the user, so it cannot leak results between tenants.
	// This isn't perfect, but better than nothing.
	f.lastQueryID.Store(rand.Uint64())

	promauto.With(reg).NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Name:      "query_frontend_queries_in_progress",
		Help:      "Number of queries in progress handled by this frontend.",
	}, func() float64 {
		return float64(f.requests.count())
	})

	promauto.With(reg).NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Name:      "query_frontend_connected_schedulers",
		Help:      "Number of schedulers this frontend is connected to.",
	}, func() float64 {
		return float64(f.schedulerWorkers.getWorkersCount())
	})

	f.Service = services.NewIdleService(f.starting, f.stopping)
	return f, nil
}

func (f *Frontend) starting(_ context.Context) error {
	// Instead of re-using `ctx` from the frontend service, `schedulerWorkers`
	// needs to use their own service context, because we want to control the
	// stopping process in the `stopping` function of the frontend. If we would
	// use the same context, the child service would be stopped automatically as
	// soon as the context of the parent service is cancelled.
	return errors.Wrap(services.StartAndAwaitRunning(context.Background(), f.schedulerWorkers), "failed to start frontend scheduler workers")
}

func (f *Frontend) stopping(_ error) error {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()

	timeout := time.NewTimer(f.cfg.GracefulShutdownTimeout)
	defer timeout.Stop()

	start := time.Now()
	for loop := true; loop; {
		select {
		case now := <-t.C:
			inflight := f.requests.count()
			if inflight <= 0 {
				level.Debug(f.log).Log("msg", "inflight requests completed", "inflight", inflight, "elapsed", now.Sub(start))
				loop = false
			} else {
				level.Debug(f.log).Log("msg", "waiting for inflight requests to complete", "inflight", inflight, "elapsed", now.Sub(start))
			}
		case now := <-timeout.C:
			inflight := f.requests.count()
			level.Debug(f.log).Log("msg", "timed out waiting for inflight requests to complete", "inflight", inflight, "elapsed", now.Sub(start))
			loop = false
		}
	}

	return errors.Wrap(services.StopAndAwaitTerminated(context.Background(), f.schedulerWorkers), "failed to stop frontend scheduler workers")
}

// RoundTripGRPC round trips a proto (instead of a HTTP request).
func (f *Frontend) RoundTripGRPC(ctx context.Context, req *httpgrpc.HTTPRequest) (*httpgrpc.HTTPResponse, error) {
	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return nil, err
	}
	tenantID := tenant.JoinTenantIDs(tenantIDs)

	// Propagate trace context in gRPC too - this will be ignored if using HTTP.
	tracer, span := opentracing.GlobalTracer(), opentracing.SpanFromContext(ctx)
	if tracer != nil && span != nil {
		carrier := (*lokigrpc.HeadersCarrier)(req)
		if err := tracer.Inject(span.Context(), opentracing.HTTPHeaders, carrier); err != nil {
			return nil, err
		}
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	freq := &frontendRequest{
		queryID:      f.lastQueryID.Inc(),
		request:      req,
		tenantID:     tenantID,
		actor:        httpreq.ExtractActorPath(ctx),
		statsEnabled: stats.IsEnabled(ctx),

		cancel: cancel,

		// Buffer of 1 to ensure response or error can be written to the channel
		// even if this goroutine goes away due to client context cancellation.
		enqueue:  make(chan enqueueResult, 1),
		response: make(chan ResponseTuple, 1),
	}

	cancelCh, err := f.enqueue(ctx, freq)
	defer f.requests.delete(freq.queryID)
	if err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		if cancelCh != nil {
			select {
			case cancelCh <- freq.queryID:
				// cancellation sent.
			default:
				// failed to cancel, ignore.
				level.Warn(f.log).Log("msg", "failed to send cancellation request to scheduler, queue full")
			}
		}
		return nil, ctx.Err()

	case resp := <-freq.response:
		switch concrete := resp.Response.(type) {
		case *frontendv2pb.QueryResultRequest_HttpResponse:
			if stats.ShouldTrackHTTPGRPCResponse(concrete.HttpResponse) {
				stats := stats.FromContext(ctx)
				stats.Merge(resp.Stats) // Safe if stats is nil.
			}

			return concrete.HttpResponse, nil
		default:
			return nil, fmt.Errorf("unsupported response type for roundtrip: %T", resp.Response)
		}
	}
}

// Do implements queryrangebase.Handler analogous to RoundTripGRPC.
func (f *Frontend) Do(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return nil, err
	}
	tenantID := tenant.JoinTenantIDs(tenantIDs)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	freq := &frontendRequest{
		queryID:      f.lastQueryID.Inc(),
		tenantID:     tenantID,
		actor:        httpreq.ExtractActorPath(ctx),
		statsEnabled: stats.IsEnabled(ctx),

		cancel: cancel,

		// Buffer of 1 to ensure response or error can be written to the channel
		// even if this goroutine goes away due to client context cancellation.
		enqueue:  make(chan enqueueResult, 1),
		response: make(chan ResponseTuple, 1),
	}

	if f.cfg.Encoding == EncodingProtobuf {
		freq.queryRequest, err = f.codec.QueryRequestWrap(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("cannot wrap request: %w", err)
		}
	} else {
		httpReq, err := f.codec.EncodeRequest(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("cannot convert request to HTTP request: %w", err)
		}

		freq.request, err = httpgrpc.FromHTTPRequest(httpReq)
		if err != nil {
			return nil, fmt.Errorf("cannot convert HTTP request to gRPC request: %w", err)
		}
	}

	cancelCh, err := f.enqueue(ctx, freq)
	defer f.requests.delete(freq.queryID)
	if err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		if cancelCh != nil {
			select {
			case cancelCh <- freq.queryID:
				// cancellation sent.
			default:
				// failed to cancel, ignore.
				level.Warn(f.log).Log("msg", "failed to send cancellation request to scheduler, queue full")
			}
		}
		return nil, ctx.Err()

	case resp := <-freq.response:
		if resp.error != nil {
			return nil, resp.error
		}
		switch concrete := resp.Response.(type) {
		case *frontendv2pb.QueryResultRequest_HttpResponse:
			if stats.ShouldTrackHTTPGRPCResponse(concrete.HttpResponse) {
				stats := stats.FromContext(ctx)
				stats.Merge(resp.Stats) // Safe if stats is nil.
			}

			return f.codec.DecodeHTTPGrpcResponse(concrete.HttpResponse, req)
		case *frontendv2pb.QueryResultRequest_QueryResponse:
			if stats.ShouldTrackQueryResponse(concrete.QueryResponse.Status) {
				stats := stats.FromContext(ctx)
				stats.Merge(resp.Stats) // Safe if stats is nil.
			}

			return queryrange.QueryResponseUnwrap(concrete.QueryResponse)
		default:
			return nil, fmt.Errorf("unexpected frontend v2 response type: %T", concrete)
		}
	}
}

func (f *Frontend) enqueue(ctx context.Context, freq *frontendRequest) (chan<- uint64, error) {
	f.requests.put(freq)

	retries := f.cfg.WorkerConcurrency + 1 // To make sure we hit at least two different schedulers.

enqueueAgain:
	var cancelCh chan<- uint64
	select {
	case <-ctx.Done():
		return cancelCh, ctx.Err()

	case f.requestsCh <- freq:
		// Enqueued, let's wait for response.
		enqRes := <-freq.enqueue

		if enqRes.status == waitForResponse {
			cancelCh = enqRes.cancelCh
			break // go wait for response.
		} else if enqRes.status == failed {
			retries--
			if retries > 0 {
				goto enqueueAgain
			}
		}

		return cancelCh, httpgrpc.Errorf(http.StatusInternalServerError, "failed to enqueue request")
	}

	return cancelCh, nil
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
	if req != nil && req.tenantID == userID {
		select {
		case req.response <- ResponseTuple{qrReq, nil}:
			// Should always be possible, unless QueryResult is called multiple times with the same queryID.
		default:
			level.Warn(f.log).Log("msg", "failed to write query result to the response channel", "queryID", qrReq.QueryID, "user", userID)
		}
	}
	// TODO(chaudum): In case the the userIDs do not match, we do not send a
	// response to the req.response channel.
	// In that case, the RoundTripGRPC method waits until the request context deadline exceeds.
	// Only then the function finished and the request is removed from the
	// requests map.

	return &frontendv2pb.QueryResultResponse{}, nil
}

// CheckReady determines if the query frontend is ready.  Function parameters/return
// chosen to match the same method in the ingester
func (f *Frontend) CheckReady(_ context.Context) error {
	if s := f.State(); s != services.Running {
		return fmt.Errorf("%v", s)
	}

	workers := f.schedulerWorkers.getWorkersCount()

	// If frontend is connected to at least one scheduler, we are ready.
	if workers > 0 {
		return nil
	}

	msg := fmt.Sprintf("not ready: number of schedulers this worker is connected to is %d", workers)
	level.Info(f.log).Log("msg", msg)
	return errors.New(msg)
}

func (f *Frontend) IsProtobufEncoded() bool {
	return f.cfg.Encoding == EncodingProtobuf
}

func (f *Frontend) IsJSONEncoded() bool {
	return f.cfg.Encoding == EncodingJSON
}

const stripeSize = 1 << 6

type requestsInProgress struct {
	locks    []sync.Mutex
	requests []map[uint64]*frontendRequest
}

func newRequestsInProgress() *requestsInProgress {
	x := &requestsInProgress{
		requests: make([]map[uint64]*frontendRequest, stripeSize),
		locks:    make([]sync.Mutex, stripeSize),
	}

	for i := range x.requests {
		x.requests[i] = map[uint64]*frontendRequest{}
	}

	return x
}

func (r *requestsInProgress) count() (res int) {
	for i := range r.requests {
		r.locks[i].Lock()
		res += len(r.requests[i])
		r.locks[i].Unlock()
	}
	return
}

func (r *requestsInProgress) put(req *frontendRequest) {
	i := req.queryID & uint64(stripeSize-1)
	r.locks[i].Lock()
	r.requests[i][req.queryID] = req
	r.locks[i].Unlock()
}

func (r *requestsInProgress) delete(queryID uint64) {
	i := queryID & uint64(stripeSize-1)
	r.locks[i].Lock()
	delete(r.requests[i], queryID)
	r.locks[i].Unlock()

}

func (r *requestsInProgress) get(queryID uint64) *frontendRequest {
	i := queryID & uint64(stripeSize-1)
	r.locks[i].Lock()
	req := r.requests[i][queryID]
	r.locks[i].Unlock()
	return req
}
