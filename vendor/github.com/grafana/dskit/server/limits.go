package server

import (
	"context"
	"strings"

	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/tap"
)

type GrpcInflightMethodLimiter interface {
	// RPCCallStarting is called before request has been read into memory.
	// All that's known about the request at this point is grpc method name.
	// Returned error should be convertible to gRPC Status via status.FromError,
	// otherwise gRPC-server implementation-specific error will be returned to the client (codes.PermissionDenied in grpc@v1.55.0).
	RPCCallStarting(methodName string) error
	RPCCallFinished(methodName string)
}

// Custom type to hide it from other packages.
type grpcLimitCheckContextKey int

// Presence of this key in the context indicates that inflight request counter was increased for this request, and needs to be decreased when request ends.
const (
	requestFullMethod grpcLimitCheckContextKey = 1
)

func newGrpcInflightLimitCheck(methodLimiter GrpcInflightMethodLimiter) *grpcInflightLimitCheck {
	return &grpcInflightLimitCheck{
		methodLimiter: methodLimiter,
	}
}

// grpcInflightLimitCheck implements gRPC TapHandle and gRPC stats.Handler.
// grpcInflightLimitCheck can track inflight requests, and reject requests before even reading them into memory.
type grpcInflightLimitCheck struct {
	methodLimiter GrpcInflightMethodLimiter
}

// TapHandle is called after receiving grpc request and headers, but before reading any request data yet.
// If we reject request here, it won't be counted towards any metrics (eg. in middleware.grpcStatsHandler).
// If we accept request (not return error), eventually HandleRPC with stats.End notification will be called.
func (g *grpcInflightLimitCheck) TapHandle(ctx context.Context, info *tap.Info) (context.Context, error) {
	if !isMethodNameValid(info.FullMethodName) {
		// If method name is not valid, we let the request continue, but not call method limiter.
		// Otherwise, we would not be able to call method limiter again when the call finishes, because in this case grpc server will not call stat handler.
		return ctx, nil
	}

	if err := g.methodLimiter.RPCCallStarting(info.FullMethodName); err != nil {
		return ctx, err
	}

	ctx = context.WithValue(ctx, requestFullMethod, info.FullMethodName)
	return ctx, nil
}

func (g *grpcInflightLimitCheck) TagRPC(ctx context.Context, _ *stats.RPCTagInfo) context.Context {
	return ctx
}

func (g *grpcInflightLimitCheck) HandleRPC(ctx context.Context, rpcStats stats.RPCStats) {
	// when request ends, and we started "inflight" request tracking for it, finish it.
	if _, ok := rpcStats.(*stats.End); !ok {
		return
	}

	if name, ok := ctx.Value(requestFullMethod).(string); ok {
		g.methodLimiter.RPCCallFinished(name)
	}
}

func (g *grpcInflightLimitCheck) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context {
	return ctx
}

func (g *grpcInflightLimitCheck) HandleConn(_ context.Context, _ stats.ConnStats) {
	// Not interested.
}

// This function mimics the check in grpc library, server.go, handleStream method. handleStream method can stop processing early,
// without calling stat handler if the method name is invalid.
func isMethodNameValid(method string) bool {
	if method != "" && method[0] == '/' {
		method = method[1:]
	}
	pos := strings.LastIndex(method, "/")
	return pos >= 0
}
