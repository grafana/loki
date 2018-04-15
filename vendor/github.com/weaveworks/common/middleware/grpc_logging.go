package middleware

import (
	"time"

	log "github.com/sirupsen/logrus"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	"github.com/weaveworks/common/logging"
)

const gRPC = "gRPC"

// GRPCServerLog logs grpc requests, errors, and latency.
type GRPCServerLog struct {
	// WithRequest will log the entire request rather than just the error
	WithRequest bool
}

// UnaryServerInterceptor returns an interceptor that logs gRPC requests
func (s GRPCServerLog) UnaryServerInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	begin := time.Now()
	resp, err := handler(ctx, req)
	entry := logging.With(ctx).WithFields(log.Fields{"method": info.FullMethod, "duration": time.Since(begin)})
	if err != nil {
		if s.WithRequest {
			entry = entry.WithField("request", req)
		}
		entry.WithError(err).Warn(gRPC)
	} else {
		entry.Debugf("%s (success)", gRPC)
	}
	return resp, err
}
