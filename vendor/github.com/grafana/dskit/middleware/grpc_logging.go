// Provenance-includes-location: https://github.com/weaveworks/common/blob/main/middleware/grpc_logging.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: Weaveworks Ltd.

package middleware

import (
	"context"
	"errors"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	dskit_log "github.com/grafana/dskit/log"

	"google.golang.org/grpc"

	grpcUtils "github.com/grafana/dskit/grpcutil"
	"github.com/grafana/dskit/user"
)

const (
	gRPC = "gRPC"
)

// An error can implement ShouldLog() to control whether GRPCServerLog will log.
type OptionalLogging interface {
	ShouldLog(ctx context.Context, duration time.Duration) bool
}

// GRPCServerLog logs grpc requests, errors, and latency.
type GRPCServerLog struct {
	Log log.Logger
	// WithRequest will log the entire request rather than just the error
	WithRequest              bool
	DisableRequestSuccessLog bool
}

// UnaryServerInterceptor returns an interceptor that logs gRPC requests
func (s GRPCServerLog) UnaryServerInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	begin := time.Now()
	resp, err := handler(ctx, req)
	if err == nil && s.DisableRequestSuccessLog {
		return resp, nil
	}
	var optional OptionalLogging
	if errors.As(err, &optional) && !optional.ShouldLog(ctx, time.Since(begin)) {
		return resp, err
	}

	entry := log.With(user.LogWith(ctx, s.Log), "method", info.FullMethod, "duration", time.Since(begin))
	if err != nil {
		if s.WithRequest {
			entry = log.With(entry, "request", req)
		}
		if grpcUtils.IsCanceled(err) {
			level.Debug(entry).Log("msg", gRPC, "err", err)
		} else {
			level.Warn(entry).Log("msg", gRPC, "err", err)
		}
	} else {
		level.Debug(entry).Log("msg", dskit_log.LazySprintf("%s (success)", gRPC))
	}
	return resp, err
}

// StreamServerInterceptor returns an interceptor that logs gRPC requests
func (s GRPCServerLog) StreamServerInterceptor(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	begin := time.Now()
	err := handler(srv, ss)
	if err == nil && s.DisableRequestSuccessLog {
		return nil
	}

	entry := log.With(user.LogWith(ss.Context(), s.Log), "method", info.FullMethod, "duration", time.Since(begin))
	if err != nil {
		if grpcUtils.IsCanceled(err) {
			level.Debug(entry).Log("msg", gRPC, "err", err)
		} else {
			level.Warn(entry).Log("msg", gRPC, "err", err)
		}
	} else {
		level.Debug(entry).Log("msg", dskit_log.LazySprintf("%s (success)", gRPC))
	}
	return err
}
