package worker

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/gogo/status"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/grpcclient"
	"github.com/grafana/dskit/multierror"
	"github.com/grafana/dskit/ring/client"
	"github.com/grafana/dskit/services"
	otgrpc "github.com/opentracing-contrib/go-grpc"
	"github.com/opentracing/opentracing-go"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/middleware"
	"github.com/weaveworks/common/user"
	"go.uber.org/atomic"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/pkg/lokifrontend/frontend/v2/frontendv2pb"
	querier_stats "github.com/grafana/loki/pkg/querier/stats"
	"github.com/grafana/loki/pkg/scheduler/schedulerpb"
	httpgrpcutil "github.com/grafana/loki/pkg/util/httpgrpc"
	util_log "github.com/grafana/loki/pkg/util/log"
)

func newSchedulerProcessor(cfg Config, handler RequestHandler, log log.Logger, metrics *Metrics) (*schedulerProcessor, []services.Service) {
	p := &schedulerProcessor{
		log:            log,
		handler:        handler,
		maxMessageSize: cfg.GRPCClientConfig.MaxSendMsgSize,
		querierID:      cfg.QuerierID,
		grpcConfig:     cfg.GRPCClientConfig,
		schedulerClientFactory: func(conn *grpc.ClientConn) schedulerpb.SchedulerForQuerierClient {
			return schedulerpb.NewSchedulerForQuerierClient(conn)
		},

		metrics: metrics,
	}

	poolConfig := client.PoolConfig{
		CheckInterval:      5 * time.Second,
		HealthCheckEnabled: true,
		HealthCheckTimeout: 1 * time.Second,
	}
	p.frontendPool = client.NewPool("frontend", poolConfig, nil, p.createFrontendClient, p.metrics.frontendClientsGauge, log)
	return p, []services.Service{p.frontendPool}
}

// Handles incoming queries from query-scheduler.
type schedulerProcessor struct {
	log            log.Logger
	handler        RequestHandler
	grpcConfig     grpcclient.Config
	maxMessageSize int
	querierID      string

	schedulerClientFactory func(conn *grpc.ClientConn) schedulerpb.SchedulerForQuerierClient

	frontendPool *client.Pool
	metrics      *Metrics
}

// notifyShutdown implements processor.
func (sp *schedulerProcessor) notifyShutdown(ctx context.Context, conn *grpc.ClientConn, address string) {
	client := sp.schedulerClientFactory(conn)

	req := &schedulerpb.NotifyQuerierShutdownRequest{QuerierID: sp.querierID}
	if _, err := client.NotifyQuerierShutdown(ctx, req); err != nil {
		// Since we're shutting down there's nothing we can do except logging it.
		level.Warn(sp.log).Log("msg", "failed to notify querier shutdown to query-scheduler", "address", address, "err", err)
	}
}

func (sp *schedulerProcessor) processQueriesOnSingleStream(workerCtx context.Context, conn *grpc.ClientConn, address string) {
	schedulerClient := sp.schedulerClientFactory(conn)

	// Run the querier loop (and so all the queries) in a dedicated context that we call the "execution context".
	// The execution context is cancelled once the workerCtx is cancelled AND there's no inflight query executing.
	execCtx, execCancel, inflightQuery := newExecutionContext(workerCtx, sp.log)
	defer execCancel()

	backoff := backoff.New(execCtx, processorBackoffConfig)
	for backoff.Ongoing() {
		c, err := schedulerClient.QuerierLoop(execCtx)
		if err == nil {
			err = c.Send(&schedulerpb.QuerierToScheduler{QuerierID: sp.querierID})
		}

		if err != nil {
			level.Warn(sp.log).Log("msg", "error contacting scheduler", "err", err, "addr", address)
			backoff.Wait()
			continue
		}

		if err := sp.querierLoop(c, address, inflightQuery); err != nil {
			// Do not log an error if the query-scheduler is shutting down.
			if s, ok := status.FromError(err); !ok || !strings.Contains(s.Message(), schedulerpb.ErrSchedulerIsNotRunning.Error()) {
				level.Error(sp.log).Log("msg", "error processing requests from scheduler", "err", err, "addr", address)
			}

			backoff.Wait()
			continue
		}

		backoff.Reset()
	}
}

// process loops processing requests on an established stream.
func (sp *schedulerProcessor) querierLoop(c schedulerpb.SchedulerForQuerier_QuerierLoopClient, address string, inflightQuery *atomic.Bool) error {
	// Build a child context so we can cancel a query when the stream is closed.
	ctx, cancel := context.WithCancel(c.Context())
	defer cancel()

	for {
		request, err := c.Recv()
		if err != nil {
			return err
		}

		inflightQuery.Store(true)

		// Handle the request on a "background" goroutine, so we go back to
		// blocking on c.Recv().  This allows us to detect the stream closing
		// and cancel the query.  We don't actually handle queries in parallel
		// here, as we're running in lock step with the server - each Recv is
		// paired with a Send.
		go func() {
			defer inflightQuery.Store(false)

			// We need to inject user into context for sending response back.
			ctx := user.InjectOrgID(ctx, request.UserID)

			sp.metrics.inflightRequests.Inc()
			tracer := opentracing.GlobalTracer()
			// Ignore errors here. If we cannot get parent span, we just don't create new one.
			parentSpanContext, err := httpgrpcutil.GetParentSpanForRequest(tracer, request.HttpRequest)
			if err != nil {
				level.Error(util_log.Logger).Log("msg", "couldn't get parent span from request", "err", err)
			}
			if parentSpanContext != nil {
				queueSpan, spanCtx := opentracing.StartSpanFromContextWithTracer(ctx, tracer, "querier_processor_runRequest", opentracing.ChildOf(parentSpanContext))
				defer queueSpan.Finish()

				ctx = spanCtx
			}
			logger := util_log.WithContext(ctx, sp.log)

			sp.runRequest(ctx, logger, request.QueryID, request.FrontendAddress, request.StatsEnabled, request.HttpRequest)
			sp.metrics.inflightRequests.Dec()
			// Report back to scheduler that processing of the query has finished.
			if err := c.Send(&schedulerpb.QuerierToScheduler{}); err != nil {
				level.Error(logger).Log("msg", "error notifying scheduler about finished query", "err", err, "addr", address)
			}
		}()
	}
}

func (sp *schedulerProcessor) runRequest(ctx context.Context, logger log.Logger, queryID uint64, frontendAddress string, statsEnabled bool, request *httpgrpc.HTTPRequest) {
	var stats *querier_stats.Stats
	if statsEnabled {
		stats, ctx = querier_stats.ContextWithEmptyStats(ctx)
	}

	response, err := sp.handler.Handle(ctx, request)
	if err != nil {
		var ok bool
		response, ok = httpgrpc.HTTPResponseFromError(err)
		if !ok {
			response = &httpgrpc.HTTPResponse{
				Code: http.StatusInternalServerError,
				Body: []byte(err.Error()),
			}
		}
	}

	logger = log.With(logger, "frontend", frontendAddress)

	// Ensure responses that are too big are not retried.
	if len(response.Body) >= sp.maxMessageSize {
		level.Error(logger).Log("msg", "response larger than max message size", "size", len(response.Body), "maxMessageSize", sp.maxMessageSize)

		errMsg := fmt.Sprintf("response larger than the max message size (%d vs %d)", len(response.Body), sp.maxMessageSize)
		response = &httpgrpc.HTTPResponse{
			Code: http.StatusRequestEntityTooLarge,
			Body: []byte(errMsg),
		}
	}

	runPoolWithBackoff(
		ctx,
		logger,
		sp.frontendPool,
		frontendAddress,
		func(c client.PoolClient) error {
			// Response is empty and uninteresting.
			_, err = c.(frontendv2pb.FrontendForQuerierClient).QueryResult(ctx, &frontendv2pb.QueryResultRequest{
				QueryID:      queryID,
				HttpResponse: response,
				Stats:        stats,
			})
			if err != nil {
				level.Error(logger).Log("msg", "error notifying frontend about finished query", "err", err)
			}
			return err
		},
	)
}

var defaultBackoff = backoff.Config{
	MinBackoff: 100 * time.Millisecond,
	MaxBackoff: 10 * time.Second,
	MaxRetries: 5,
}

func runPoolWithBackoff(
	ctx context.Context,
	logger log.Logger,
	pool *client.Pool,
	addr string,
	f func(client.PoolClient) error,
) {
	var (
		backoff = backoff.New(ctx, defaultBackoff)
		errs    = multierror.New()
	)

	for backoff.Ongoing() {
		c, err := pool.GetClientFor(addr)
		if err != nil {
			level.Error(logger).Log("msg", "error acquiring client", "err", err)
			errs.Add(err)
			pool.RemoveClientFor(addr)
			backoff.Wait()
			continue
		}

		if err = f(c); err != nil {
			errs.Add(err)

			// copied from dskit. I'm assuming we need an org_id here.
			hCtx := user.InjectOrgID(ctx, "0")
			_, err := c.Check(hCtx, &grpc_health_v1.HealthCheckRequest{})

			// If health check fails, remove client from pool.
			if err != nil {
				level.Error(logger).Log("msg", "error health checking", "err", err)
				pool.RemoveClientFor(addr)
			}

			backoff.Wait()
			continue
		}
		return
	}
}

func (sp *schedulerProcessor) createFrontendClient(addr string) (client.PoolClient, error) {
	opts, err := sp.grpcConfig.DialOption([]grpc.UnaryClientInterceptor{
		otgrpc.OpenTracingClientInterceptor(opentracing.GlobalTracer()),
		middleware.ClientUserHeaderInterceptor,
		middleware.UnaryClientInstrumentInterceptor(sp.metrics.frontendClientRequestDuration),
	}, nil)
	if err != nil {
		return nil, err
	}

	conn, err := grpc.Dial(addr, opts...)
	if err != nil {
		return nil, err
	}

	return &frontendClient{
		FrontendForQuerierClient: frontendv2pb.NewFrontendForQuerierClient(conn),
		HealthClient:             grpc_health_v1.NewHealthClient(conn),
		conn:                     conn,
	}, nil
}

type frontendClient struct {
	frontendv2pb.FrontendForQuerierClient
	grpc_health_v1.HealthClient
	conn *grpc.ClientConn
}

func (fc *frontendClient) Close() error {
	return fc.conn.Close()
}
