package middleware

import (
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	"github.com/weaveworks/common/user"
)

// ClientUserHeaderInterceptor propagates the user ID from the context to gRPC metadata, which eventually ends up as a HTTP2 header.
func ClientUserHeaderInterceptor(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	ctx, err := user.InjectIntoGRPCRequest(ctx)
	if err != nil {
		return err
	}

	return invoker(ctx, method, req, reply, cc, opts...)
}

// ServerUserHeaderInterceptor propagates the user ID from the gRPC metadata back to our context.
func ServerUserHeaderInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	_, ctx, err := user.ExtractFromGRPCRequest(ctx)
	if err != nil {
		return nil, err
	}

	return handler(ctx, req)
}
