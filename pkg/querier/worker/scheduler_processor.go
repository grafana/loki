package worker

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/grpcclient"
	"github.com/grafana/dskit/ring/client"
	"github.com/grafana/dskit/services"
	otgrpc "github.com/opentracing-contrib/go-grpc"
	"github.com/opentracing/opentracing-go"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/middleware"
	"github.com/weaveworks/common/user"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/pkg/lokifrontend/frontend/v2/frontendv2pb"
	querier_stats "github.com/grafana/loki/pkg/querier/stats"
	"github.com/grafana/loki/pkg/scheduler/schedulerpb"
	util_log "github.com/grafana/loki/pkg/util/log"
)

func newSchedulerProcessor(cfg Config, handler http.Handler, log log.Logger, metrics *Metrics) (*schedulerProcessor, []services.Service) {
	p := &schedulerProcessor{
		log:            log,
		handler:        handler,
		maxMessageSize: cfg.GRPCClientConfig.MaxSendMsgSize,
		querierID:      cfg.QuerierID,
		grpcConfig:     cfg.GRPCClientConfig,

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
	handler        http.Handler 
	grpcConfig     grpcclient.Config
	maxMessageSize int
	querierID      string

	frontendPool *client.Pool
	metrics      *Metrics
}

// notifyShutdown implements processor.
func (sp *schedulerProcessor) notifyShutdown(ctx context.Context, conn *grpc.ClientConn, address string) {
	client := schedulerpb.NewSchedulerForQuerierClient(conn)

	req := &schedulerpb.NotifyQuerierShutdownRequest{QuerierID: sp.querierID}
	if _, err := client.NotifyQuerierShutdown(ctx, req); err != nil {
		// Since we're shutting down there's nothing we can do except logging it.
		level.Warn(sp.log).Log("msg", "failed to notify querier shutdown to query-scheduler", "address", address, "err", err)
	}
}

func (sp *schedulerProcessor) processQueriesOnSingleStream(ctx context.Context, conn *grpc.ClientConn, address string) {
	schedulerClient := schedulerpb.NewSchedulerForQuerierClient(conn)

	backoff := backoff.New(ctx, processorBackoffConfig)
	for backoff.Ongoing() {
		c, err := schedulerClient.QuerierLoop(ctx)
		if err == nil {
			err = c.Send(&schedulerpb.QuerierToScheduler{QuerierID: sp.querierID})
		}

		if err != nil {
			level.Error(sp.log).Log("msg", "error contacting scheduler", "err", err, "addr", address)
			backoff.Wait()
			continue
		}

		if err := sp.querierLoop(c, address); err != nil {
			// E.Welch I don't know how to do this any better but context cancelations seem common,
			// likely because of an underlying connection being close,
			// they are noisy and I don't think they communicate anything useful.
			if !strings.Contains(err.Error(), "context canceled") {
				level.Error(sp.log).Log("msg", "error processing requests from scheduler", "err", err, "addr", address)
			}
			backoff.Wait()
			continue
		}

		backoff.Reset()
	}
}

// process loops processing requests on an established stream.
func (sp *schedulerProcessor) querierLoop(c schedulerpb.SchedulerForQuerier_QuerierLoopClient, address string) error {
	// Build a child context so we can cancel a query when the stream is closed.
	ctx, cancel := context.WithCancel(c.Context())
	defer cancel()

	for {
		request, err := c.Recv()
		if err != nil {
			return err
		}

		// Handle the request on a "background" goroutine, so we go back to
		// blocking on c.Recv().  This allows us to detect the stream closing
		// and cancel the query.  We don't actually handle queries in parallel
		// here, as we're running in lock step with the server - each Recv is
		// paired with a Send.
		go func() {
			// We need to inject user into context for sending response back.
			var (
				ctx    = user.InjectOrgID(ctx, request.UserID)
				logger = util_log.WithContext(ctx, sp.log)
			)

			sp.metrics.inflightRequests.Inc()

			sp.runRequest(ctx, logger, request.QueryID, request.FrontendAddress, request.StatsEnabled, request.HttpRequest)

			sp.metrics.inflightRequests.Dec()

			// Report back to scheduler that processing of the query has finished.
			if err := c.Send(&schedulerpb.QuerierToScheduler{}); err != nil {
				level.Error(logger).Log("msg", "error notifying scheduler about finished query", "err", err, "addr", address)
			}
		}()
	}
}

type nopCloser struct {
	*bytes.Buffer
}

func (nopCloser) Close() error { return nil }

// BytesBuffer returns the underlaying `bytes.buffer` used to build this io.ReadCloser.
func (n nopCloser) BytesBuffer() *bytes.Buffer { return n.Buffer }

func toHeader(hs []*httpgrpc.Header, header http.Header) {
	for _, h := range hs {
		header[h.Key] = h.Values
	}
}

func fromHeader(hs http.Header) []*httpgrpc.Header {
	result := make([]*httpgrpc.Header, 0, len(hs))
	for k, vs := range hs {
		result = append(result, &httpgrpc.Header{
			Key:    k,
			Values: vs,
		})
	}
	return result
}

type responseWriter struct {
	stats     *querier_stats.Stats
	queryID   uint64
	stream    frontendv2pb.FrontendForQuerier_QueryResultChunkedClient
	recorder  *httptest.ResponseRecorder
	body      *bytes.Buffer // TODO: should use buffered writer instead.
	sentFirst bool
}

func newReponseWriter(stream frontendv2pb.FrontendForQuerier_QueryResultChunkedClient, stats *querier_stats.Stats, queryID uint64) *responseWriter {
	return &responseWriter{
		stats: stats,
		queryID: queryID,
		stream: stream,
		recorder: httptest.NewRecorder(),
		body: new(bytes.Buffer),
		sentFirst: false,
	}
}

func(r *responseWriter) Header() http.Header {
	return r.recorder.Header()
}

func(r *responseWriter) Write(buf []byte) (int, error) {
	if !r.sentFirst {
		return r.recorder.Write(buf)
	}

	return r.body.Write(buf)
}

func(r *responseWriter) WriteHeader(statusCode int) {
	if !r.sentFirst {
		r.recorder.WriteHeader(statusCode)
	}
}

func(r *responseWriter) Flush() {
	if !r.sentFirst {
		resp := &httpgrpc.HTTPResponse{
			Code:    int32(r.recorder.Code),
			Headers: fromHeader(r.recorder.Header()),
			Body:    r.recorder.Body.Bytes(),
		}
		msg := &frontendv2pb.QueryResultRequestChunked{
			Type: &frontendv2pb.QueryResultRequestChunked_First{
				First: &frontendv2pb.QueryResultRequest{
					QueryID:      r.queryID,
					HttpResponse: resp,
					Stats:        r.stats,
				},
			},
		}
		r.stream.Send(msg)
		r.sentFirst = true
	} else {
		msg := &frontendv2pb.QueryResultRequestChunked{
			Type: &frontendv2pb.QueryResultRequestChunked_Continuation{
				Continuation: r.body.Bytes(),	
			},
		}
		r.stream.Send(msg)
		r.body.Reset() // TODO: not sure this is save here.
	}
}

func (sp *schedulerProcessor) runRequest(ctx context.Context, logger log.Logger, queryID uint64, frontendAddress string, statsEnabled bool, r *httpgrpc.HTTPRequest) {
	var stats *querier_stats.Stats
	if statsEnabled {
		stats, ctx = querier_stats.ContextWithEmptyStats(ctx)
	}
	c, err := sp.frontendPool.GetClientFor(frontendAddress)
	if err != nil {
		level.Error(logger).Log("msg", "error notifying frontend about finished query", "err", err, "frontend", frontendAddress)
		return
	}

	stream, err := c.(frontendv2pb.FrontendForQuerierClient).QueryResultChunked(ctx)
	if err != nil {
		level.Error(logger).Log("msg", "error notifying frontend about finished query", "err", err, "frontend", frontendAddress)
		return
	}

	req, err := http.NewRequest(r.Method, r.Url, nopCloser{Buffer: bytes.NewBuffer(r.Body)})
	if err != nil {
		level.Error(logger).Log("msg", "error notifying frontend about finished query", "err", err, "frontend", frontendAddress)
		return 
	}

	toHeader(r.Headers, req.Header)
	req = req.WithContext(ctx)
	req.RequestURI = r.Url
	req.ContentLength = int64(len(r.Body))

	writer := newReponseWriter(stream, stats, queryID)

	sp.handler.ServeHTTP(writer, req)
	writer.Flush()

	_, err = stream.CloseAndRecv()
	//httpgrpc_server.NewServer(externalHandler)
	/*response, err := sp.handler.Handle(ctx, request)
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
	*/

	/*
	c, err := sp.frontendPool.GetClientFor(frontendAddress)
	if err == nil {
		stream, err := c.(frontendv2pb.FrontendForQuerierClient).QueryResultChunked(ctx)
		if err != nil {
			level.Error(logger).Log("msg", "error notifying frontend about finished query", "err", err, "frontend", frontendAddress)
		} else {
			msg := &frontendv2pb.QueryResultRequestChunked{
				Type: &frontendv2pb.QueryResultRequestChunked_First{
					First: &frontendv2pb.QueryResultRequest{
						QueryID:      queryID,
						HttpResponse: response,
						Stats:        stats,
					},
				},
			}
			err = stream.Send(msg)
			if err != nil {
				level.Error(logger).Log("msg", "error notifying frontend about finished query", "err", err, "frontend", frontendAddress)
			}
			_, err = stream.CloseAndRecv()
		}
	}
	if err != nil {
		level.Error(logger).Log("msg", "error notifying frontend about finished query", "err", err, "frontend", frontendAddress)
	}
	*/
}

func (sp *schedulerProcessor) createFrontendClient(addr string) (client.PoolClient, error) {
	opts, err := sp.grpcConfig.DialOption([]grpc.UnaryClientInterceptor{
		otgrpc.OpenTracingClientInterceptor(opentracing.GlobalTracer()),
		middleware.ClientUserHeaderInterceptor,
		middleware.UnaryClientInstrumentInterceptor(sp.metrics.frontendClientRequestDuration),
	}, []grpc.StreamClientInterceptor{
		otgrpc.OpenTracingStreamClientInterceptor(opentracing.GlobalTracer()),
		middleware.StreamClientUserHeaderInterceptor,
		middleware.StreamClientInstrumentInterceptor(sp.metrics.frontendClientRequestDuration),
	})
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
