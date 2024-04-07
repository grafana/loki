package v1

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/services"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/v3/pkg/lokifrontend/frontend/v1/frontendv1pb"
	"github.com/grafana/loki/v3/pkg/querier/stats"
	"github.com/grafana/loki/v3/pkg/queue"
	"github.com/grafana/loki/v3/pkg/scheduler/limits"
	"github.com/grafana/loki/v3/pkg/util"
	lokigrpc "github.com/grafana/loki/v3/pkg/util/httpgrpc"
)

var errTooManyRequest = httpgrpc.Errorf(http.StatusTooManyRequests, "too many outstanding requests")

// Config for a Frontend.
type Config struct {
	MaxOutstandingPerTenant int           `yaml:"max_outstanding_per_tenant"`
	QuerierForgetDelay      time.Duration `yaml:"querier_forget_delay"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.IntVar(&cfg.MaxOutstandingPerTenant, "querier.max-outstanding-requests-per-tenant", 2048, "Maximum number of outstanding requests per tenant per frontend; requests beyond this error with HTTP 429.")
	f.DurationVar(&cfg.QuerierForgetDelay, "query-frontend.querier-forget-delay", 0, "In the event a tenant is repeatedly sending queries that lead the querier to crash or be killed due to an out-of-memory error, the crashed querier will be disconnected from the query frontend and a new querier will be immediately assigned to the tenant’s shard. This invalidates the assumption that shuffle sharding can be used to reduce the impact on tenants. This option mitigates the impact by configuring a delay between when a querier disconnects because of a crash and when the crashed querier is actually removed from the tenant's shard.")
}

type Limits interface {
	// Returns max queriers to use per tenant, or 0 if shuffle sharding is disabled.
	MaxQueriersPerUser(user string) uint

	// MaxQueryCapacity returns how much of the available query capacity can be used by this user.
	MaxQueryCapacity(user string) float64
}

// Frontend queues HTTP requests, dispatches them to backends, and handles retries
// for requests which failed.
type Frontend struct {
	services.Service

	cfg    Config
	log    log.Logger
	limits Limits

	requestQueue *queue.RequestQueue
	activeUsers  *util.ActiveUsersCleanupService

	// Subservices manager.
	subservices        *services.Manager
	subservicesWatcher *services.FailureWatcher

	// qeueue metrics
	queueMetrics *queue.Metrics

	// frontend metrics
	numClients    prometheus.GaugeFunc
	queueDuration prometheus.Histogram
}

type request struct {
	enqueueTime time.Time
	queueSpan   opentracing.Span
	originalCtx context.Context

	request  *httpgrpc.HTTPRequest
	err      chan error
	response chan *httpgrpc.HTTPResponse
}

// New creates a new frontend. Frontend implements service, and must be started and stopped.
func New(cfg Config, frontendLimits Limits, log log.Logger, registerer prometheus.Registerer, metricsNamespace string) (*Frontend, error) {
	queueMetrics := queue.NewMetrics(registerer, metricsNamespace, "query_frontend")
	f := &Frontend{
		cfg:          cfg,
		log:          log,
		limits:       frontendLimits,
		queueMetrics: queueMetrics,
		queueDuration: promauto.With(registerer).NewHistogram(prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Name:      "query_frontend_queue_duration_seconds",
			Help:      "Time spend by requests queued.",
			Buckets:   prometheus.DefBuckets,
		}),
	}

	f.requestQueue = queue.NewRequestQueue(cfg.MaxOutstandingPerTenant, cfg.QuerierForgetDelay, limits.NewQueueLimits(frontendLimits), queueMetrics)
	f.activeUsers = util.NewActiveUsersCleanupWithDefaultValues(f.cleanupInactiveUserMetrics)

	var err error
	f.subservices, err = services.NewManager(f.requestQueue, f.activeUsers)
	if err != nil {
		return nil, err
	}

	f.numClients = promauto.With(registerer).NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Name:      "query_frontend_connected_clients",
		Help:      "Number of worker clients currently connected to the frontend.",
	}, f.requestQueue.GetConnectedConsumersMetric)

	f.Service = services.NewBasicService(f.starting, f.running, f.stopping)
	return f, nil
}

func (f *Frontend) starting(ctx context.Context) error {
	f.subservicesWatcher = services.NewFailureWatcher()
	f.subservicesWatcher.WatchManager(f.subservices)

	if err := services.StartManagerAndAwaitHealthy(ctx, f.subservices); err != nil {
		return errors.Wrap(err, "unable to start frontend subservices")
	}

	return nil
}

func (f *Frontend) running(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-f.subservicesWatcher.Chan():
			return errors.Wrap(err, "frontend subservice failed")
		}
	}
}

func (f *Frontend) stopping(_ error) error {
	// This will also stop the requests queue, which stop accepting new requests and errors out any pending requests.
	return services.StopManagerAndAwaitStopped(context.Background(), f.subservices)
}

func (f *Frontend) cleanupInactiveUserMetrics(user string) {
	f.queueMetrics.Cleanup(user)
}

// RoundTripGRPC round trips a proto (instead of a HTTP request).
func (f *Frontend) RoundTripGRPC(ctx context.Context, req *httpgrpc.HTTPRequest) (*httpgrpc.HTTPResponse, error) {
	// Propagate trace context in gRPC too - this will be ignored if using HTTP.
	tracer, span := opentracing.GlobalTracer(), opentracing.SpanFromContext(ctx)
	if tracer != nil && span != nil {
		carrier := (*lokigrpc.HeadersCarrier)(req)
		err := tracer.Inject(span.Context(), opentracing.HTTPHeaders, carrier)
		if err != nil {
			return nil, err
		}
	}

	request := request{
		request:     req,
		originalCtx: ctx,

		// Buffer of 1 to ensure response can be written by the server side
		// of the Process stream, even if this goroutine goes away due to
		// client context cancellation.
		err:      make(chan error, 1),
		response: make(chan *httpgrpc.HTTPResponse, 1),
	}

	if err := f.queueRequest(ctx, &request); err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()

	case resp := <-request.response:
		return resp, nil

	case err := <-request.err:
		return nil, err
	}
}

// Process allows backends to pull requests from the frontend.
func (f *Frontend) Process(server frontendv1pb.Frontend_ProcessServer) error {
	querierID, err := getQuerierID(server)
	if err != nil {
		return err
	}

	f.requestQueue.RegisterConsumerConnection(querierID)
	defer f.requestQueue.UnregisterConsumerConnection(querierID)

	lastIndex := queue.StartIndex

	for {
		reqWrapper, idx, err := f.requestQueue.Dequeue(server.Context(), lastIndex, querierID)
		if err != nil {
			return err
		}
		lastIndex = idx

		req := reqWrapper.(*request)

		f.queueDuration.Observe(time.Since(req.enqueueTime).Seconds())
		req.queueSpan.Finish()

		/*
		  We want to dequeue the next unexpired request from the chosen tenant queue.
		  The chance of choosing a particular tenant for dequeueing is (1/active_tenants).
		  This is problematic under load, especially with other middleware enabled such as
		  querier.split-by-interval, where one request may fan out into many.
		  If expired requests aren't exhausted before checking another tenant, it would take
		  n_active_tenants * n_expired_requests_at_front_of_queue requests being processed
		  before an active request was handled for the tenant in question.
		  If this tenant meanwhile continued to queue requests,
		  it's possible that it's own queue would perpetually contain only expired requests.
		*/
		if req.originalCtx.Err() != nil {
			lastIndex = lastIndex.ReuseLastIndex()
			continue
		}

		// Handle the stream sending & receiving on a goroutine so we can
		// monitoring the contexts in a select and cancel things appropriately.
		resps := make(chan *frontendv1pb.ClientToFrontend, 1)
		errs := make(chan error, 1)
		go func() {
			err = server.Send(&frontendv1pb.FrontendToClient{
				Type:         frontendv1pb.HTTP_REQUEST,
				HttpRequest:  req.request,
				StatsEnabled: stats.IsEnabled(req.originalCtx),
			})
			if err != nil {
				errs <- err
				return
			}

			resp, err := server.Recv()
			if err != nil {
				errs <- err
				return
			}

			resps <- resp
		}()

		select {
		// If the upstream request is cancelled, we need to cancel the
		// downstream req.  Only way we can do that is to close the stream.
		// The worker client is expecting this semantics.
		case <-req.originalCtx.Done():
			return req.originalCtx.Err()

		// Is there was an error handling this request due to network IO,
		// then error out this upstream request _and_ stream.
		case err := <-errs:
			req.err <- err
			return err

		// Happy path: merge the stats and propagate the response.
		case resp := <-resps:
			if stats.ShouldTrackHTTPGRPCResponse(resp.HttpResponse) {
				stats := stats.FromContext(req.originalCtx)
				stats.Merge(resp.Stats) // Safe if stats is nil.
			}

			req.response <- resp.HttpResponse
		}
	}
}

func (f *Frontend) NotifyClientShutdown(_ context.Context, req *frontendv1pb.NotifyClientShutdownRequest) (*frontendv1pb.NotifyClientShutdownResponse, error) {
	level.Info(f.log).Log("msg", "received shutdown notification from querier", "querier", req.GetClientID())
	f.requestQueue.NotifyConsumerShutdown(req.GetClientID())

	return &frontendv1pb.NotifyClientShutdownResponse{}, nil
}

func getQuerierID(server frontendv1pb.Frontend_ProcessServer) (string, error) {
	err := server.Send(&frontendv1pb.FrontendToClient{
		Type: frontendv1pb.GET_ID,
		// Old queriers don't support GET_ID, and will try to use the request.
		// To avoid confusing them, include dummy request.
		HttpRequest: &httpgrpc.HTTPRequest{
			Method: "GET",
			Url:    "/invalid_request_sent_by_frontend",
		},
	})
	if err != nil {
		return "", err
	}

	resp, err := server.Recv()

	// Old queriers will return empty string, which is fine. All old queriers will be
	// treated as single querier with lot of connections.
	// (Note: if resp is nil, GetClientID() returns "")
	return resp.GetClientID(), err
}

func (f *Frontend) queueRequest(ctx context.Context, req *request) error {
	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return err
	}

	now := time.Now()
	req.enqueueTime = now
	req.queueSpan, _ = opentracing.StartSpanFromContext(ctx, "queued")

	joinedTenantID := tenant.JoinTenantIDs(tenantIDs)
	f.activeUsers.UpdateUserTimestamp(joinedTenantID, now)

	err = f.requestQueue.Enqueue(joinedTenantID, nil, req, nil)
	if err == queue.ErrTooManyRequests {
		return errTooManyRequest
	}
	return err
}

// CheckReady determines if the query frontend is ready.  Function parameters/return
// chosen to match the same method in the ingester
func (f *Frontend) CheckReady(_ context.Context) error {
	// if we have more than one querier connected we will consider ourselves ready
	connectedClients := f.requestQueue.GetConnectedConsumersMetric()
	if connectedClients > 0 {
		return nil
	}

	msg := fmt.Sprintf("not ready: number of queriers connected to query-frontend is %d", int64(connectedClients))
	level.Info(f.log).Log("msg", msg)
	return errors.New(msg)
}
