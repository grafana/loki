package grpcclient

import (
	otgrpc "github.com/opentracing-contrib/go-grpc"
	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"

	"github.com/grafana/dskit/middleware"
)

func Instrument(requestDuration *prometheus.HistogramVec, instrumentationLabelOptions ...middleware.InstrumentationOption) ([]grpc.UnaryClientInterceptor, []grpc.StreamClientInterceptor) {
	return []grpc.UnaryClientInterceptor{
			otgrpc.OpenTracingClientInterceptor(opentracing.GlobalTracer()),
			middleware.ClientUserHeaderInterceptor,
			middleware.UnaryClientInstrumentInterceptor(requestDuration, instrumentationLabelOptions...),
		}, []grpc.StreamClientInterceptor{
			otgrpc.OpenTracingStreamClientInterceptor(opentracing.GlobalTracer()),
			middleware.StreamClientUserHeaderInterceptor,
			middleware.StreamClientInstrumentInterceptor(requestDuration, instrumentationLabelOptions...),
		}
}
