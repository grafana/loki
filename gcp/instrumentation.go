package gcp

import (
	otgrpc "github.com/opentracing-contrib/go-grpc"
	opentracing "github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/api/option"
	"google.golang.org/grpc"

	"github.com/cortexproject/cortex/pkg/util/middleware"
)

var bigtableRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Namespace: "cortex",
	Name:      "bigtable_request_duration_seconds",
	Help:      "Time spent doing Bigtable requests.",

	// Bigtable latency seems to range from a few ms to a few hundred ms and is
	// important.  So use 6 buckets from 1ms to 1s.
	Buckets: prometheus.ExponentialBuckets(0.001, 4, 6),
}, []string{"operation", "status_code"})

func instrumentation() ([]grpc.UnaryClientInterceptor, []grpc.StreamClientInterceptor) {
	return []grpc.UnaryClientInterceptor{
			otgrpc.OpenTracingClientInterceptor(opentracing.GlobalTracer()),
			middleware.PrometheusGRPCUnaryInstrumentation(bigtableRequestDuration),
		},
		[]grpc.StreamClientInterceptor{
			otgrpc.OpenTracingStreamClientInterceptor(opentracing.GlobalTracer()),
			middleware.PrometheusGRPCStreamInstrumentation(bigtableRequestDuration),
		}
}

func toBigtableOpts(opts []grpc.DialOption) []option.ClientOption {
	result := make([]option.ClientOption, 0, len(opts))
	for _, opt := range opts {
		result = append(result, option.WithGRPCDialOption(opt))
	}
	return result
}
