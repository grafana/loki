// Copyright 2017 Michal Witkowski. All Rights Reserved.
// See LICENSE for licensing terms.

package tracing

import (
	"context"
	"time"

	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	"github.com/opentracing/opentracing-go/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"

	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/tags"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/util/metautils"
)

var (
	grpcTag = opentracing.Tag{Key: string(ext.Component), Value: "gRPC"}
)

type opentracingServerReporter struct {
	ctx             context.Context
	typ             interceptors.GRPCType
	service, method string

	serverSpan opentracing.Span
}

func (o *opentracingServerReporter) PostCall(err error, _ time.Duration) {
	// Finish span and log context information.
	tags := tags.Extract(o.ctx)
	for k, v := range tags.Values() {
		o.serverSpan.SetTag(k, v)
	}
	if err != nil {
		ext.Error.Set(o.serverSpan, true)
		o.serverSpan.LogFields(log.String("event", "error"), log.String("message", err.Error()))
	}
	o.serverSpan.Finish()
}

func (o *opentracingServerReporter) PostMsgSend(interface{}, error, time.Duration) {}

func (o *opentracingServerReporter) PostMsgReceive(interface{}, error, time.Duration) {}

type opentracingServerReportable struct {
	tracer opentracing.Tracer
	// This is only used for server. TODO: Investigate if needed in client.
	traceHeaderName string
	filterOutFunc   FilterFunc
}

func (o *opentracingServerReportable) ServerReporter(ctx context.Context, _ interface{}, typ interceptors.GRPCType, service string, method string) (interceptors.Reporter, context.Context) {
	if o.filterOutFunc != nil && !o.filterOutFunc(ctx, interceptors.FullMethod(service, method)) {
		return interceptors.NoopReporter{}, ctx
	}

	newCtx, serverSpan := newServerSpanFromInbound(ctx, o.tracer, o.traceHeaderName, interceptors.FullMethod(service, method))
	mock := &opentracingServerReporter{ctx: newCtx, typ: typ, service: service, method: method, serverSpan: serverSpan}
	return mock, newCtx
}

// UnaryServerInterceptor returns a new unary server interceptor for OpenTracing.
func UnaryServerInterceptor(opts ...Option) grpc.UnaryServerInterceptor {
	o := evaluateOptions(opts)
	return interceptors.UnaryServerInterceptor(&opentracingServerReportable{tracer: o.tracer, traceHeaderName: o.traceHeaderName, filterOutFunc: o.filterOutFunc})
}

// StreamServerInterceptor returns a new streaming server interceptor for OpenTracing.
func StreamServerInterceptor(opts ...Option) grpc.StreamServerInterceptor {
	o := evaluateOptions(opts)
	return interceptors.StreamServerInterceptor(&opentracingServerReportable{tracer: o.tracer, traceHeaderName: o.traceHeaderName, filterOutFunc: o.filterOutFunc})
}

func newServerSpanFromInbound(ctx context.Context, tracer opentracing.Tracer, traceHeaderName, fullMethodName string) (context.Context, opentracing.Span) {
	md := metautils.ExtractIncoming(ctx)
	parentSpanContext, err := tracer.Extract(opentracing.HTTPHeaders, metadataTextMap(md))
	if err != nil && err != opentracing.ErrSpanContextNotFound {
		grpclog.Infof("grpc_opentracing: failed parsing trace information: %v", err)
	}

	serverSpan := tracer.StartSpan(
		fullMethodName,
		// this is magical, it attaches the new span to the parent parentSpanContext, and creates an unparented one if empty.
		ext.RPCServerOption(parentSpanContext),
		grpcTag,
	)

	injectOpentracingIdsToTags(traceHeaderName, serverSpan, tags.Extract(ctx))
	return opentracing.ContextWithSpan(ctx, serverSpan), serverSpan
}
