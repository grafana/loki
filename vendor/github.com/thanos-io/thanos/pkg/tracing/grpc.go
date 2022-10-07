// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package tracing

import (
	"context"

	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware/v2"
	grpc_opentracing "github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/tracing"
	opentracing "github.com/opentracing/opentracing-go"
	"google.golang.org/grpc"
)

// UnaryClientInterceptor returns a new unary client interceptor for OpenTracing.
func UnaryClientInterceptor(tracer opentracing.Tracer) grpc.UnaryClientInterceptor {
	return grpc_opentracing.UnaryClientInterceptor(grpc_opentracing.WithTracer(tracer))
}

// StreamClientInterceptor returns a new streaming client interceptor for OpenTracing.
func StreamClientInterceptor(tracer opentracing.Tracer) grpc.StreamClientInterceptor {
	return grpc_opentracing.StreamClientInterceptor(grpc_opentracing.WithTracer(tracer))
}

// UnaryServerInterceptor returns a new unary server interceptor for OpenTracing and injects given tracer.
func UnaryServerInterceptor(tracer opentracing.Tracer) grpc.UnaryServerInterceptor {
	interceptor := grpc_opentracing.UnaryServerInterceptor(grpc_opentracing.WithTracer(tracer))
	return func(parentCtx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// Add our own tracer.
		return interceptor(ContextWithTracer(parentCtx, tracer), req, info, handler)
	}
}

// StreamServerInterceptor returns a new streaming server interceptor for OpenTracing and injects given tracer.
func StreamServerInterceptor(tracer opentracing.Tracer) grpc.StreamServerInterceptor {
	interceptor := grpc_opentracing.StreamServerInterceptor(grpc_opentracing.WithTracer(tracer))
	return func(srv interface{}, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		// Add our own tracer.
		wrappedStream := grpc_middleware.WrapServerStream(stream)
		wrappedStream.WrappedContext = ContextWithTracer(stream.Context(), tracer)

		return interceptor(srv, wrappedStream, info, handler)
	}
}
