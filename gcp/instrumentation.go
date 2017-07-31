package gcp

import (
	"io"
	"time"

	"github.com/grpc-ecosystem/grpc-opentracing/go/otgrpc"
	"github.com/mwitkow/go-grpc-middleware"
	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/weaveworks/common/instrument"
	"golang.org/x/net/context"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
)

var bigtableRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
	Namespace: "cortex",
	Name:      "bigtable_request_duration_seconds",
	Help:      "Time spent doing Bigtable requests.",

	// Bigtable latency seems to range from a few ms to a few sec and is
	// important.  So use 8 buckets from 128us to 2s.
	Buckets: prometheus.ExponentialBuckets(0.000128, 4, 8),
}, []string{"operation", "status_code"})

func init() {
	prometheus.MustRegister(bigtableRequestDuration)
}

func instrumentation() []option.ClientOption {
	return []option.ClientOption{
		option.WithGRPCDialOption(
			grpc.WithUnaryInterceptor(grpc_middleware.ChainUnaryClient(
				otgrpc.OpenTracingClientInterceptor(opentracing.GlobalTracer()),
				grpcUnaryInstrumentation,
			)),
		),
		option.WithGRPCDialOption(
			grpc.WithStreamInterceptor(grpcStreamInstrumentation),
		),
	}
}

func grpcUnaryInstrumentation(
	ctx context.Context, method string, req, resp interface{},
	cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption,
) error {
	start := time.Now()
	err := invoker(ctx, method, req, resp, cc, opts...)
	bigtableRequestDuration.WithLabelValues(method, instrument.ErrorCode(err)).Observe(time.Now().Sub(start).Seconds())
	return err
}

func grpcStreamInstrumentation(
	ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string,
	streamer grpc.Streamer, opts ...grpc.CallOption,
) (grpc.ClientStream, error) {
	start := time.Now()
	stream, err := streamer(ctx, desc, cc, method, opts...)
	return &instrumentedClientStream{
		start:        start,
		method:       method,
		ClientStream: stream,
	}, err
}

type instrumentedClientStream struct {
	start  time.Time
	method string
	grpc.ClientStream
}

func (s *instrumentedClientStream) RecvMsg(m interface{}) error {
	err := s.ClientStream.RecvMsg(m)
	if err == nil {
		return err
	}

	if err == io.EOF {
		bigtableRequestDuration.WithLabelValues(s.method, instrument.ErrorCode(nil)).Observe(time.Now().Sub(s.start).Seconds())
	} else {
		bigtableRequestDuration.WithLabelValues(s.method, instrument.ErrorCode(err)).Observe(time.Now().Sub(s.start).Seconds())
	}

	return err
}
