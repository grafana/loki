package querylimits

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const (
	lowerQueryLimitsHeaderName        = "x-loki-query-limits"
	lowerQueryLimitsContextHeaderName = "x-loki-query-limits-context"
)

func injectIntoGRPCRequest(ctx context.Context) (context.Context, error) {
	// inject into GRPC metadata
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		md = metadata.New(map[string]string{})
	}
	md = md.Copy()

	limits := ExtractQueryLimitsFromContext(ctx)
	if limits != nil {
		headerValue, err := MarshalQueryLimits(limits)
		if err != nil {
			return nil, err
		}
		md.Set(lowerQueryLimitsHeaderName, string(headerValue))
	}

	limitsCtx := ExtractQueryLimitsContextFromContext(ctx)
	if limitsCtx != nil {
		headerValue, err := MarshalQueryLimitsContext(limitsCtx)
		if err != nil {
			return nil, err
		}
		md.Set(lowerQueryLimitsContextHeaderName, string(headerValue))
	}

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
	if ok && len(headerValues) > 0 {
		// Pick first header
		limits, err := UnmarshalQueryLimits([]byte(headerValues[0]))
		if err != nil {
			return ctx, err
		}
		ctx = InjectQueryLimitsIntoContext(ctx, *limits)
	}

	// Extract QueryLimitsContext if present
	headerContextValues, ok := md[lowerQueryLimitsContextHeaderName]
	if ok && len(headerContextValues) > 0 {
		// Pick first header
		limitsCtx, err := UnmarshalQueryLimitsContext([]byte(headerContextValues[0]))
		if err != nil {
			return ctx, err
		}
		ctx = InjectQueryLimitsContextIntoContext(ctx, *limitsCtx)
	}

	return ctx, nil
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
