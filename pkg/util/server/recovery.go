package server

import (
	"fmt"
	"net/http"
	"os"
	"runtime"

	grpc_recovery "github.com/grpc-ecosystem/go-grpc-middleware/recovery"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/middleware"
)

const maxStacksize = 8 * 1024

var (
	panicTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "loki",
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
)

func onPanic(p interface{}) error {
	stack := make([]byte, maxStacksize)
	stack = stack[:runtime.Stack(stack, true)]
	// keep a multiline stack
	fmt.Fprintf(os.Stderr, "panic: %v\n%s", p, stack)
	panicTotal.Inc()
	return httpgrpc.Errorf(http.StatusInternalServerError, "error while processing request: %v", p)
}
