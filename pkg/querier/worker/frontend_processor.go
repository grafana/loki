package worker

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/httpgrpc"
	httpgrpc_server "github.com/grafana/dskit/httpgrpc/server"
	"github.com/grafana/dskit/user"
	"google.golang.org/grpc"

	"github.com/grafana/loki/pkg/lokifrontend/frontend/transport"
	"github.com/grafana/loki/pkg/lokifrontend/frontend/v1/frontendv1pb"
	"github.com/grafana/loki/pkg/querier/queryrange"
	querier_stats "github.com/grafana/loki/pkg/querier/stats"
)

var (
	processorBackoffConfig = backoff.Config{
		MinBackoff: 500 * time.Millisecond,
		MaxBackoff: 5 * time.Second,
	}
)

func newFrontendProcessor(cfg Config, handler RequestHandler, log log.Logger) processor {
	return &frontendProcessor{
		log:            log,
		handler:        handler,
		maxMessageSize: cfg.GRPCClientConfig.MaxSendMsgSize,
		querierID:      cfg.QuerierID,
	}
}

// Handles incoming queries from frontend. This is used if there's no query-scheduler between the frontend and querier.
// This should be used by Frontend V1.
type frontendProcessor struct {
	handler        RequestHandler
	maxMessageSize int
	querierID      string

	log log.Logger
}

// notifyShutdown implements processor.
func (fp *frontendProcessor) notifyShutdown(ctx context.Context, conn *grpc.ClientConn, address string) {
	client := frontendv1pb.NewFrontendClient(conn)

	req := &frontendv1pb.NotifyClientShutdownRequest{ClientID: fp.querierID}
	if _, err := client.NotifyClientShutdown(ctx, req); err != nil {
		// Since we're shutting down there's nothing we can do except logging it.
		level.Warn(fp.log).Log("msg", "failed to notify querier shutdown to query-frontend", "address", address, "err", err)
	}
}

// runOne loops, trying to establish a stream to the frontend to begin request processing.
func (fp *frontendProcessor) processQueriesOnSingleStream(ctx context.Context, conn *grpc.ClientConn, address string) {
	client := frontendv1pb.NewFrontendClient(conn)

	backoff := backoff.New(ctx, processorBackoffConfig)
	for backoff.Ongoing() {
		c, err := client.Process(ctx)
		if err != nil {
			level.Error(fp.log).Log("msg", "error contacting frontend", "address", address, "err", err)
			backoff.Wait()
			continue
		}

		if err := fp.process(c); err != nil {
			level.Error(fp.log).Log("msg", "error processing requests", "address", address, "err", err)
			backoff.Wait()
			continue
		}

		backoff.Reset()
	}
}

// process loops processing requests on an established stream.
func (fp *frontendProcessor) process(c frontendv1pb.Frontend_ProcessClient) error {
	// Build a child context so we can cancel a query when the stream is closed.
	ctx, cancel := context.WithCancel(c.Context())
	defer cancel()

	for {
		request, err := c.Recv()
		if err != nil {
			return err
		}

		switch request.Type {
		case frontendv1pb.HTTP_REQUEST:
			// Handle the request on a "background" goroutine, so we go back to
			// blocking on c.Recv().  This allows us to detect the stream closing
			// and cancel the query.  We don't actually handle queries in parallel
			// here, as we're running in lock step with the server - each Recv is
			// paired with a Send.
			go fp.runRequest(ctx, request.HttpRequest, request.StatsEnabled, func(response *httpgrpc.HTTPResponse, stats *querier_stats.Stats) error {
				return c.Send(&frontendv1pb.ClientToFrontend{
					HttpResponse: response,
					Stats:        stats,
				})
			})

		case frontendv1pb.GET_ID:
			err := c.Send(&frontendv1pb.ClientToFrontend{ClientID: fp.querierID})
			if err != nil {
				return err
			}

		default:
			return fmt.Errorf("unknown request type: %v", request.Type)
		}
	}
}

func (fp *frontendProcessor) runRequest(ctx context.Context, request *httpgrpc.HTTPRequest, statsEnabled bool, sendResponse func(response *httpgrpc.HTTPResponse, stats *querier_stats.Stats) error) {
	var stats *querier_stats.Stats
	if statsEnabled {
		stats, ctx = querier_stats.ContextWithEmptyStats(ctx)
	}

	// TODO: handler error
	httpReq, _ := httpgrpc_server.ToHTTP(ctx, request)

	_, ctx, _ = user.ExtractOrgIDFromHTTPRequest(httpReq)

	req, _ := queryrange.DefaultCodec.DecodeRequest(ctx, httpReq, nil)

	// TODO return error code
	response, err := fp.handler.Do(ctx, req)

	var httpResp *http.Response
	var grpcResp *httpgrpc.HTTPResponse
	if err == nil {
		httpResp, err = queryrange.DefaultCodec.EncodeResponse(ctx, httpReq, response)
		if err == nil {
			grpcResp, err = transport.HTTPtoHttpgrpcResponse(httpResp)
		}
	}

	if err != nil {
		var ok bool
		grpcResp, ok = httpgrpc.HTTPResponseFromError(err)
		if !ok {
			grpcResp = &httpgrpc.HTTPResponse{
				Code: http.StatusInternalServerError,
				Body: []byte(err.Error()),
			}
		}
	}

	// Ensure responses that are too big are not retried.
	// TODO
	/*
		if len(response.Body) >= fp.maxMessageSize {
			errMsg := fmt.Sprintf("response larger than the max (%d vs %d)", len(response.Body), fp.maxMessageSize)
			response = &httpgrpc.HTTPResponse{
				Code: http.StatusRequestEntityTooLarge,
				Body: []byte(errMsg),
			}
			level.Error(fp.log).Log("msg", "error processing query", "err", errMsg)
		}
	*/

	if err := sendResponse(grpcResp, stats); err != nil {
		level.Error(fp.log).Log("msg", "error processing requests", "err", err)
	}
}
