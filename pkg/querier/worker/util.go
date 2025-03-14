// SPDX-License-Identifier: AGPL-3.0-only

package worker

import (
	"context"
	"net/http"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/gogo/status"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/pkg/errors"
	"go.uber.org/atomic"
	"google.golang.org/grpc/codes"

	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/util/server"
)

// newExecutionContext returns a new execution context (execCtx) that wraps the input workerCtx and
// it used to run the querier's worker loop and execute queries.
// The purpose of the execution context is to gracefully shutdown queriers, waiting
// until inflight queries are terminated before the querier process exits.
//
// The caller must call execCancel() once done.
//
// How it's used:
//
// - The querier worker's loop run in a dedicated context, called the "execution context".
//
// - The execution context is canceled when the worker context gets cancelled (ie. querier is shutting down)
// and there's no inflight query execution. In case there's an inflight query, the execution context is canceled
// once the inflight query terminates and the response has been sent.
func newExecutionContext(workerCtx context.Context, logger log.Logger) (execCtx context.Context, execCancel context.CancelCauseFunc, inflightQuery *atomic.Bool) {
	execCtx, execCancel = context.WithCancelCause(context.Background())
	inflightQuery = atomic.NewBool(false)

	go func() {
		// Wait until it's safe to cancel the execution context, which is when one of the following conditions happen:
		// - The worker context has been canceled and there's no inflight query
		// - The execution context itself has been explicitly canceled
		select {
		case <-workerCtx.Done():
			level.Debug(logger).Log("msg", "querier worker context has been canceled, waiting until there's no inflight query")
			//
			// TODO：a potential race condition
			//
			// summarizing Marco's answer.
			//   Question 1：
			//     isn't there a potential race condition between testing the flag and setting it in the querier loop?
			//     It could be false here but then the next query is received.
			//   Answer 1：
			//     When the querier shutdowns it's expected to cancel the context and so the call to request,
			//         err := c.Recv() (done in schedulerProcessor.querierLoop())
			//     to return error because of the canceled context (I mean the querier context, not the query execution context).
			//
			//   Question 2：
			//     Is there a race？
			//   Answer 2：
			//     Yes, there's a race between the call to c.Recv() and the sequent call to inflightQuery.Store(true),
			//     but the time window is very short and we ignored it in Mimir (all in all we want to gracefully handle the 99.9% of cases).
			//
			//  Question Q3：
			//    I was wondering if we can end up in a state were the query is inflight but we shut down.I guess it times out.
			//  Answer 3：
			//    I think that race condition still exists (I found it very hard to guarantee to never happen) but in practice should be very unlikely.
			for inflightQuery.Load() {
				select {
				case <-execCtx.Done():
					// In the meanwhile, the execution context has been explicitly canceled, so we should just terminate.
					return
				case <-time.After(100 * time.Millisecond):
					// Going to check it again.
				}
			}

			level.Debug(logger).Log("msg", "querier worker context has been canceled and there's no inflight query, canceling the execution context too")
			execCancel(errors.New("querier worker context has been canceled and there's no inflight query"))
		case <-execCtx.Done():
			// Nothing to do. The execution context has been explicitly canceled.
		}
	}()

	return
}

// handleHTTPRequest converts the request and applies it to the handler.
func handleHTTPRequest(ctx context.Context, request *httpgrpc.HTTPRequest, handler RequestHandler, codec RequestCodec) *httpgrpc.HTTPResponse {
	req, ctx, err := codec.DecodeHTTPGrpcRequest(ctx, request)
	if err != nil {
		response, ok := httpgrpc.HTTPResponseFromError(err)
		if !ok {
			return &httpgrpc.HTTPResponse{
				Code: http.StatusInternalServerError,
				Body: []byte(err.Error()),
			}
		}
		return response
	}

	resp, err := handler.Do(ctx, req)
	if err != nil {
		code, err := server.ClientHTTPStatusAndError(err)
		return &httpgrpc.HTTPResponse{
			Code: int32(code),
			Body: []byte(err.Error()),
		}
	}

	response, err := queryrange.DefaultCodec.EncodeHTTPGrpcResponse(ctx, request, resp)
	if err != nil {
		code, err := server.ClientHTTPStatusAndError(err)
		return &httpgrpc.HTTPResponse{
			Code: int32(code),
			Body: []byte(err.Error()),
		}
	}

	return response
}

// handleQueryRequest applies unwraps a request and applies it to the handler.
func handleQueryRequest(ctx context.Context, request *queryrange.QueryRequest, handler RequestHandler, codec RequestCodec) *queryrange.QueryResponse {
	r, ctx, err := codec.QueryRequestUnwrap(ctx, request)
	if err != nil {
		return &queryrange.QueryResponse{
			Status: status.New(codes.Internal, err.Error()).Proto(),
		}
	}

	resp, err := handler.Do(ctx, r)
	if err != nil {
		if s, ok := status.FromError(err); ok {
			return &queryrange.QueryResponse{
				Status: s.Proto(),
			}
		}

		// This block covers any errors that are not gRPC errors and will include all query errors.
		// It's important to map non-retryable errors to a non 5xx status code so they will not be retried.
		return queryrange.QueryResponseWrapError(err)
	}

	response, err := queryrange.QueryResponseWrap(resp)
	if err != nil {
		return &queryrange.QueryResponse{
			Status: status.New(codes.Internal, err.Error()).Proto(),
		}
	}

	return response
}
