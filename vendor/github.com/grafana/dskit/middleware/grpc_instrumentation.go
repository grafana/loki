// Provenance-includes-location: https://github.com/weaveworks/common/blob/main/middleware/grpc_instrumentation.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: Weaveworks Ltd.

package middleware

import (
	"context"
	"io"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	grpcUtils "github.com/grafana/dskit/grpcutil"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/instrument"
)

func observe(ctx context.Context, hist *prometheus.HistogramVec, method string, err error, duration time.Duration) {
	respStatus := "success"
	if err != nil {
		if errResp, ok := httpgrpc.HTTPResponseFromError(err); ok {
			respStatus = strconv.Itoa(int(errResp.Code))
		} else if grpcUtils.IsCanceled(err) {
			respStatus = "cancel"
		} else {
			respStatus = "error"
		}
	}
	instrument.ObserveWithExemplar(ctx, hist.WithLabelValues(gRPC, method, respStatus, "false"), duration.Seconds())
}

// UnaryServerInstrumentInterceptor instruments gRPC requests for errors and latency.
func UnaryServerInstrumentInterceptor(hist *prometheus.HistogramVec) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		begin := time.Now()
		resp, err := handler(ctx, req)
		observe(ctx, hist, info.FullMethod, err, time.Since(begin))
		return resp, err
	}
}

// StreamServerInstrumentInterceptor instruments gRPC requests for errors and latency.
func StreamServerInstrumentInterceptor(hist *prometheus.HistogramVec) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		begin := time.Now()
		err := handler(srv, ss)
		observe(ss.Context(), hist, info.FullMethod, err, time.Since(begin))
		return err
	}
}

// UnaryClientInstrumentInterceptor records duration of gRPC requests client side.
func UnaryClientInstrumentInterceptor(metric *prometheus.HistogramVec) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, resp interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		start := time.Now()
		err := invoker(ctx, method, req, resp, cc, opts...)
		metric.WithLabelValues(method, errorCode(err)).Observe(time.Since(start).Seconds())
		return err
	}
}

// StreamClientInstrumentInterceptor records duration of streaming gRPC requests client side.
func StreamClientInstrumentInterceptor(metric *prometheus.HistogramVec) grpc.StreamClientInterceptor {
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
		return nil
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
		return nil
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

// errorCode converts an error into an error code string.
func errorCode(err error) string {
	if err == nil {
		return "2xx"
	}

	if errResp, ok := httpgrpc.HTTPResponseFromError(err); ok {
		statusFamily := int(errResp.Code / 100)
		return strconv.Itoa(statusFamily) + "xx"
	} else if grpcUtils.IsCanceled(err) {
		return "cancel"
	} else {
		return "error"
	}
}
