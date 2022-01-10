package middleware

import (
	"context"
	"io"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	grpcUtils "github.com/weaveworks/common/grpc"
	"github.com/weaveworks/common/httpgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// PrometheusGRPCUnaryInstrumentation records duration of gRPC requests client side.
func PrometheusGRPCUnaryInstrumentation(metric *prometheus.HistogramVec) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, resp interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		start := time.Now()
		err := invoker(ctx, method, req, resp, cc, opts...)
		metric.WithLabelValues(method, errorCode(err)).Observe(time.Since(start).Seconds())
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
		s.metric.WithLabelValues(s.method, errorCode(nil)).Observe(time.Since(s.start).Seconds())
	} else {
		s.metric.WithLabelValues(s.method, errorCode(err)).Observe(time.Since(s.start).Seconds())
	}

	return err
}

func (s *instrumentedClientStream) RecvMsg(m interface{}) error {
	err := s.ClientStream.RecvMsg(m)
	if err == nil {
		return err
	}

	if err == io.EOF {
		s.metric.WithLabelValues(s.method, errorCode(nil)).Observe(time.Since(s.start).Seconds())
	} else {
		s.metric.WithLabelValues(s.method, errorCode(err)).Observe(time.Since(s.start).Seconds())
	}

	return err
}

func (s *instrumentedClientStream) Header() (metadata.MD, error) {
	md, err := s.ClientStream.Header()
	if err != nil {
		s.metric.WithLabelValues(s.method, errorCode(err)).Observe(time.Since(s.start).Seconds())
	}
	return md, err
}

func errorCode(err error) string {
	respStatus := "2xx"
	if err != nil {
		if errResp, ok := httpgrpc.HTTPResponseFromError(err); ok {
			statusFamily := int(errResp.Code / 100)
			respStatus = strconv.Itoa(statusFamily) + "xx"
		} else if grpcUtils.IsCanceled(err) {
			respStatus = "cancel"
		} else {
			respStatus = "error"
		}
	}

	return respStatus
}
