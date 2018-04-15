package middleware

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/weaveworks/common/httpgrpc"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

// ServerInstrumentInterceptor instruments gRPC requests for errors and latency.
func ServerInstrumentInterceptor(hist *prometheus.HistogramVec) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		begin := time.Now()
		resp, err := handler(ctx, req)
		duration := time.Since(begin).Seconds()
		respStatus := "success"
		if err != nil {
			if errResp, ok := httpgrpc.HTTPResponseFromError(err); ok {
				respStatus = strconv.Itoa(int(errResp.Code))
			} else {
				respStatus = "error"
			}
		}
		hist.WithLabelValues(gRPC, info.FullMethod, respStatus, "false").Observe(duration)
		return resp, err
	}
}
