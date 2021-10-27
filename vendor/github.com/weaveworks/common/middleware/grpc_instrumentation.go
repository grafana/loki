package middleware

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	grpcUtils "github.com/weaveworks/common/grpc"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/instrument"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
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
