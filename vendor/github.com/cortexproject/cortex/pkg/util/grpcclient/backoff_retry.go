package grpcclient

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/cortexproject/cortex/pkg/util"
)

// NewBackoffRetry gRPC middleware.
func NewBackoffRetry(cfg util.BackoffConfig) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		backoff := util.NewBackoff(ctx, cfg)
		for backoff.Ongoing() {
			err := invoker(ctx, method, req, reply, cc, opts...)
			if err == nil {
				return nil
			}

			if status.Code(err) != codes.ResourceExhausted {
				return err
			}

			backoff.Wait()
		}
		return backoff.Err()
	}
}
