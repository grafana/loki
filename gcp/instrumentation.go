package gcp

import (
	"github.com/grpc-ecosystem/go-grpc-middleware"
	otgrpc "github.com/opentracing-contrib/go-grpc"
	"github.com/opentracing/opentracing-go"
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

func instrumentation() []option.ClientOption {
	return []option.ClientOption{
		option.WithGRPCDialOption(
			grpc.WithUnaryInterceptor(grpc_middleware.ChainUnaryClient(
				otgrpc.OpenTracingClientInterceptor(opentracing.GlobalTracer()),
				middleware.PrometheusGRPCUnaryInstrumentation(bigtableRequestDuration),
			)),
		),
		option.WithGRPCDialOption(
			grpc.WithStreamInterceptor(grpc_middleware.ChainStreamClient(
				otgrpc.OpenTracingStreamClientInterceptor(opentracing.GlobalTracer()),
				middleware.PrometheusGRPCStreamInstrumentation(bigtableRequestDuration),
			)),
		),
	}
}
