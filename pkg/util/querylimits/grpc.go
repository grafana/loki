package querylimits

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const (
	lowerQueryLimitsHeaderName = "x-loki-query-limits"
)

func injectIntoGRPCRequest(ctx context.Context) (context.Context, error) {
	limits := ExtractQueryLimitsContext(ctx)
	fmt.Printf("extract limits grpc: %v", limits)
	if limits == nil {
		return ctx, nil
	}
	// inject into GRPC metadata
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		md = metadata.New(map[string]string{})
	}
	md = md.Copy()
	headerValue, err := MarshalQueryLimits(limits)
	if err != nil {
		return nil, err
	}
	md.Set(lowerQueryLimitsHeaderName, string(headerValue))
	newCtx := metadata.NewOutgoingContext(ctx, md)

	return newCtx, nil
}

func ClientQueryLimitsInterceptor(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	ctx, err := injectIntoGRPCRequest(ctx)
	if err != nil {
		return err
	}

	return invoker(ctx, method, req, reply, cc, opts...)
}

func StreamClientQueryLimitsInterceptor(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	ctx, err := injectIntoGRPCRequest(ctx)
	if err != nil {
		return nil, err
	}

	return streamer(ctx, desc, cc, method, opts...)
}

func extractFromGRPCRequest(ctx context.Context) (context.Context, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		// No metadata, just return as is
		return ctx, nil
	}

	headerValues, ok := md[lowerQueryLimitsHeaderName]
	if !ok {
		// No QueryLimits header in metadata, just return context
		return ctx, nil
	}

	if len(headerValues) == 0 {
		return ctx, nil
	}

	// Pick first header
	limits, err := UnmarshalQueryLimits([]byte(headerValues[0]))
	if err != nil {
		return ctx, err
	}
	return InjectQueryLimitsContext(ctx, *limits), nil
}

func ServerQueryLimitsInterceptor(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	ctx, err := extractFromGRPCRequest(ctx)
	if err != nil {
		return nil, err
	}

	return handler(ctx, req)
}

func StreamServerQueryLimitsInterceptor(srv interface{}, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	ctx, err := extractFromGRPCRequest(ss.Context())
	if err != nil {
		return err
	}

	return handler(srv, serverStream{
		ctx:          ctx,
		ServerStream: ss,
	})
}

type serverStream struct {
	ctx context.Context
	grpc.ServerStream
}

func (ss serverStream) Context() context.Context {
	return ss.ctx
}
