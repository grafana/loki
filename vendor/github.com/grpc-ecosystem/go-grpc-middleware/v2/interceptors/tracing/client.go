// Copyright 2017 Michal Witkowski. All Rights Reserved.
// See LICENSE for licensing terms.

package tracing

import (
	"context"
	"io"
	"time"

	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	"github.com/opentracing/opentracing-go/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"

	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/util/metautils"
)

type opentracingClientReporter struct {
	typ                 interceptors.GRPCType
	svcName, methodName string

	clientSpan opentracing.Span
}

func (o *opentracingClientReporter) PostCall(err error, _ time.Duration) {
	// Finish span.
	if err != nil && err != io.EOF {
		ext.Error.Set(o.clientSpan, true)
		o.clientSpan.LogFields(log.String("event", "error"), log.String("message", err.Error()))
	}
	o.clientSpan.Finish()
}

func (o *opentracingClientReporter) PostMsgSend(interface{}, error, time.Duration) {}

func (o *opentracingClientReporter) PostMsgReceive(interface{}, error, time.Duration) {}

type opentracingClientReportable struct {
	tracer        opentracing.Tracer
	filterOutFunc FilterFunc
}

func (o *opentracingClientReportable) ClientReporter(ctx context.Context, _ interface{}, typ interceptors.GRPCType, service string, method string) (interceptors.Reporter, context.Context) {
	if o.filterOutFunc != nil && !o.filterOutFunc(ctx, method) {
		return interceptors.NoopReporter{}, ctx
	}

	newCtx, clientSpan := newClientSpanFromContext(ctx, o.tracer, interceptors.FullMethod(service, method))
	mock := &opentracingClientReporter{typ: typ, svcName: service, methodName: method, clientSpan: clientSpan}
	return mock, newCtx
}

// UnaryClientInterceptor returns a new unary client interceptor for OpenTracing.
func UnaryClientInterceptor(opts ...Option) grpc.UnaryClientInterceptor {
	o := evaluateOptions(opts)
	return interceptors.UnaryClientInterceptor(&opentracingClientReportable{tracer: o.tracer, filterOutFunc: o.filterOutFunc})
}

// StreamClientInterceptor returns a new streaming client interceptor for OpenTracing.
func StreamClientInterceptor(opts ...Option) grpc.StreamClientInterceptor {
	o := evaluateOptions(opts)
	return interceptors.StreamClientInterceptor(&opentracingClientReportable{tracer: o.tracer, filterOutFunc: o.filterOutFunc})
}

// ClientAddContextTags returns a context with specified opentracing tags, which
// are used by UnaryClientInterceptor/StreamClientInterceptor when creating a
// new span.
func ClientAddContextTags(ctx context.Context, tags opentracing.Tags) context.Context {
	return context.WithValue(ctx, clientSpanTagKey{}, tags)
}

type clientSpanTagKey struct{}

func newClientSpanFromContext(ctx context.Context, tracer opentracing.Tracer, fullMethodName string) (context.Context, opentracing.Span) {
	var parentSpanCtx opentracing.SpanContext
	if parent := opentracing.SpanFromContext(ctx); parent != nil {
		parentSpanCtx = parent.Context()
	}
	opts := []opentracing.StartSpanOption{
		opentracing.ChildOf(parentSpanCtx),
		ext.SpanKindRPCClient,
		grpcTag,
	}
	if tagx := ctx.Value(clientSpanTagKey{}); tagx != nil {
		if opt, ok := tagx.(opentracing.StartSpanOption); ok {
			opts = append(opts, opt)
		}
	}
	clientSpan := tracer.StartSpan(fullMethodName, opts...)
	// Make sure we add this to the metadata of the call, so it gets propagated:
	md := metautils.ExtractOutgoing(ctx).Clone()
	if err := tracer.Inject(clientSpan.Context(), opentracing.HTTPHeaders, metadataTextMap(md)); err != nil {
		grpclog.Infof("grpc_opentracing: failed serializing trace information: %v", err)
	}
	ctxWithMetadata := md.ToOutgoing(ctx)
	return opentracing.ContextWithSpan(ctxWithMetadata, clientSpan), clientSpan
}
