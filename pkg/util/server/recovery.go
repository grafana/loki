package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"runtime"

	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/middleware"
	grpc_recovery "github.com/grpc-ecosystem/go-grpc-middleware/recovery"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

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
					WriteError(onPanic(p), w)
				}
			}()
			next.ServeHTTP(w, req)
		})
	})
	RecoveryGRPCStreamInterceptor = grpc_recovery.StreamServerInterceptor(grpc_recovery.WithRecoveryHandler(onPanic))
	RecoveryGRPCUnaryInterceptor  = grpc_recovery.UnaryServerInterceptor(grpc_recovery.WithRecoveryHandler(onPanic))

	RecoveryMiddleware queryrangebase.Middleware = queryrangebase.MiddlewareFunc(func(next queryrangebase.Handler) queryrangebase.Handler {
		return queryrangebase.HandlerFunc(func(ctx context.Context, req queryrangebase.Request) (res queryrangebase.Response, err error) {
			defer func() {
				if p := recover(); p != nil {
					err = onPanic(p)
				}
			}()
			res, err = next.Do(ctx, req)
			return
		})
	})
)

func onPanic(p interface{}) error {
	stack := make([]byte, maxStacksize)
	stack = stack[:runtime.Stack(stack, true)]
	// keep a multiline stack
	fmt.Fprintf(os.Stderr, "panic: %v\n%s", p, stack)
	panicTotal.Inc()
	return httpgrpc.Errorf(http.StatusInternalServerError, "error while processing request: %v", p)
}
