package v1

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/user"

	"github.com/cortexproject/cortex/pkg/frontend/v1/frontendv1pb"
	"github.com/cortexproject/cortex/pkg/scheduler/queue"
	"github.com/cortexproject/cortex/pkg/util/grpcutil"
)

var (
	errTooManyRequest = httpgrpc.Errorf(http.StatusTooManyRequests, "too many outstanding requests")
)

// Config for a Frontend.
type Config struct {
	MaxOutstandingPerTenant int `yaml:"max_outstanding_per_tenant"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.IntVar(&cfg.MaxOutstandingPerTenant, "querier.max-outstanding-requests-per-tenant", 100, "Maximum number of outstanding requests per tenant per frontend; requests beyond this error with HTTP 429.")
}

type Limits interface {
	// Returns max queriers to use per tenant, or 0 if shuffle sharding is disabled.
	MaxQueriersPerUser(user string) int
}

// Frontend queues HTTP requests, dispatches them to backends, and handles retries
// for requests which failed.
type Frontend struct {
	cfg    Config
	log    log.Logger
	limits Limits

	requestQueue *queue.RequestQueue

	// Metrics.
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

// New creates a new frontend.
func New(cfg Config, limits Limits, log log.Logger, registerer prometheus.Registerer) (*Frontend, error) {
	queueLength := promauto.With(registerer).NewGaugeVec(prometheus.GaugeOpts{
		Name: "cortex_query_frontend_queue_length",
		Help: "Number of queries in the queue.",
	}, []string{"user"})

	f := &Frontend{
		cfg:          cfg,
		log:          log,
		limits:       limits,
		requestQueue: queue.NewRequestQueue(cfg.MaxOutstandingPerTenant, queueLength),
		queueDuration: promauto.With(registerer).NewHistogram(prometheus.HistogramOpts{
			Name:    "cortex_query_frontend_queue_duration_seconds",
			Help:    "Time spend by requests queued.",
			Buckets: prometheus.DefBuckets,
		}),
	}

	f.numClients = promauto.With(registerer).NewGaugeFunc(prometheus.GaugeOpts{
		Name: "cortex_query_frontend_connected_clients",
		Help: "Number of worker clients currently connected to the frontend.",
	}, f.requestQueue.GetConnectedQuerierWorkersMetric)

	return f, nil
}

// Close stops new requests and errors out any pending requests.
func (f *Frontend) Close() {
	f.requestQueue.Stop()
}

// RoundTripGRPC round trips a proto (instead of a HTTP request).
func (f *Frontend) RoundTripGRPC(ctx context.Context, req *httpgrpc.HTTPRequest) (*httpgrpc.HTTPResponse, error) {
	// Propagate trace context in gRPC too - this will be ignored if using HTTP.
	tracer, span := opentracing.GlobalTracer(), opentracing.SpanFromContext(ctx)
	if tracer != nil && span != nil {
		carrier := (*grpcutil.HttpgrpcHeadersCarrier)(req)
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

	f.requestQueue.RegisterQuerierConnection(querierID)
	defer f.requestQueue.UnregisterQuerierConnection(querierID)

	// If the downstream request(from querier -> frontend) is cancelled,
	// we need to ping the condition variable to unblock getNextRequestForQuerier.
	// Ideally we'd have ctx aware condition variables...
	go func() {
		<-server.Context().Done()
		f.requestQueue.QuerierDisconnecting()
	}()

	lastUserIndex := queue.FirstUser()

	for {
		reqWrapper, idx, err := f.requestQueue.GetNextRequestForQuerier(server.Context(), lastUserIndex, querierID)
		if err != nil {
			return err
		}
		lastUserIndex = idx

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
			lastUserIndex = lastUserIndex.ReuseLastUser()
			continue
		}

		// Handle the stream sending & receiving on a goroutine so we can
		// monitoring the contexts in a select and cancel things appropriately.
		resps := make(chan *httpgrpc.HTTPResponse, 1)
		errs := make(chan error, 1)
		go func() {
			err = server.Send(&frontendv1pb.FrontendToClient{
				Type:        frontendv1pb.HTTP_REQUEST,
				HttpRequest: req.request,
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

			resps <- resp.HttpResponse
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

		// Happy path: propagate the response.
		case resp := <-resps:
			req.response <- resp
		}
	}
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
	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return err
	}

	req.enqueueTime = time.Now()
	req.queueSpan, _ = opentracing.StartSpanFromContext(ctx, "queued")

	maxQueriers := f.limits.MaxQueriersPerUser(userID)

	err = f.requestQueue.EnqueueRequest(userID, req, maxQueriers, nil)
	if err == queue.ErrTooManyRequests {
		return errTooManyRequest
	}
	return err
}

// CheckReady determines if the query frontend is ready.  Function parameters/return
// chosen to match the same method in the ingester
func (f *Frontend) CheckReady(_ context.Context) error {
	// if we have more than one querier connected we will consider ourselves ready
	connectedClients := f.requestQueue.GetConnectedQuerierWorkersMetric()
	if connectedClients > 0 {
		return nil
	}

	msg := fmt.Sprintf("not ready: number of queriers connected to query-frontend is %d", int64(connectedClients))
	level.Info(f.log).Log("msg", msg)
	return errors.New(msg)
}
