package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"runtime"

	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/middleware"
	grpc_recovery "github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/recovery"
	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/uber/jaeger-client-go"

	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

const maxStacksize = 8 * 1024

var (
	panicTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "panic_total",
		Help:      "The total number of panic triggered",
	})

	RecoveryHTTPMiddleware = middleware.Func(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			defer func() {
				if p := recover(); p != nil {
					WriteError(onPanic(req.Context(), p), w)
				}
			}()
			next.ServeHTTP(w, req)
		})
	})
	RecoveryGRPCStreamInterceptor = grpc_recovery.StreamServerInterceptor(grpc_recovery.WithRecoveryHandlerContext(onPanic))
	RecoveryGRPCUnaryInterceptor  = grpc_recovery.UnaryServerInterceptor(grpc_recovery.WithRecoveryHandlerContext(onPanic))

	RecoveryMiddleware queryrangebase.Middleware = queryrangebase.MiddlewareFunc(func(next queryrangebase.Handler) queryrangebase.Handler {
		return queryrangebase.HandlerFunc(func(ctx context.Context, req queryrangebase.Request) (res queryrangebase.Response, err error) {
			defer func() {
				if p := recover(); p != nil {
					err = onPanic(ctx, p)
				}
			}()
			res, err = next.Do(ctx, req)
			return
		})
	})
)

func onPanic(ctx context.Context, p interface{}) error {
	stack := make([]byte, maxStacksize)
	stack = stack[:runtime.Stack(stack, true)]
	traceID := jaegerTraceID(ctx)

	// keep a multiline stack
	fmt.Fprintf(os.Stderr, "panic: %v %s\n%s", p, traceID, stack)
	panicTotal.Inc()
	return httpgrpc.Errorf(http.StatusInternalServerError, "error while processing request: %v", p)
}

func jaegerTraceID(ctx context.Context) string {
	span := opentracing.SpanFromContext(ctx)
	if span == nil {
		return ""
	}

	spanContext, ok := span.Context().(jaeger.SpanContext)
	if !ok {
		return ""
	}

	return "traceID=" + spanContext.TraceID().String()
}
