package middleware

import (
	"io"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/weaveworks/common/instrument"
)

// PrometheusGRPCUnaryInstrumentation records duration of gRPC requests client side.
func PrometheusGRPCUnaryInstrumentation(metric *prometheus.HistogramVec) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, resp interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		start := time.Now()
		err := invoker(ctx, method, req, resp, cc, opts...)
		metric.WithLabelValues(method, instrument.ErrorCode(err)).Observe(time.Now().Sub(start).Seconds())
		return err
	}
}

// PrometheusGRPCStreamInstrumentation records duration of streaming gRPC requests client side.
func PrometheusGRPCStreamInstrumentation(metric *prometheus.HistogramVec) grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string,
		streamer grpc.Streamer, opts ...grpc.CallOption,
	) (grpc.ClientStream, error) {
		start := time.Now()
		stream, err := streamer(ctx, desc, cc, method, opts...)
		return &instrumentedClientStream{
			metric:       metric,
			start:        start,
			method:       method,
			ClientStream: stream,
		}, err
	}
}

type instrumentedClientStream struct {
	metric *prometheus.HistogramVec
	start  time.Time
	method string
	grpc.ClientStream
}

func (s *instrumentedClientStream) SendMsg(m interface{}) error {
	err := s.ClientStream.SendMsg(m)
	if err == nil {
		return err
	}

	if err == io.EOF {
		s.metric.WithLabelValues(s.method, instrument.ErrorCode(nil)).Observe(time.Now().Sub(s.start).Seconds())
	} else {
		s.metric.WithLabelValues(s.method, instrument.ErrorCode(err)).Observe(time.Now().Sub(s.start).Seconds())
	}

	return err
}

func (s *instrumentedClientStream) RecvMsg(m interface{}) error {
	err := s.ClientStream.RecvMsg(m)
	if err == nil {
		return err
	}

	if err == io.EOF {
		s.metric.WithLabelValues(s.method, instrument.ErrorCode(nil)).Observe(time.Now().Sub(s.start).Seconds())
	} else {
		s.metric.WithLabelValues(s.method, instrument.ErrorCode(err)).Observe(time.Now().Sub(s.start).Seconds())
	}

	return err
}

func (s *instrumentedClientStream) Header() (metadata.MD, error) {
	md, err := s.ClientStream.Header()
	if err != nil {
		s.metric.WithLabelValues(s.method, instrument.ErrorCode(err)).Observe(time.Now().Sub(s.start).Seconds())
	}
	return md, err
}
