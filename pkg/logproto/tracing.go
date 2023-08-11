// SPDX-License-Identifier: AGPL-3.0-only
// Provenance-includes-location: https://github.com/cortexproject/cortex/blob/master/pkg/cortex/tracing.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: The Cortex Authors.

package mimir

import (
	"context"
	"encoding/binary"

	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/log"
	"github.com/thanos-io/objstore/tracing"
	"github.com/uber/jaeger-client-go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
)

// ThanosTracerUnaryInterceptor injects the opentracing global tracer into the context
// in order to get it picked up by Thanos components.
func ThanosTracerUnaryInterceptor(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	return handler(tracing.ContextWithTracer(ctx, opentracing.GlobalTracer()), req)
}

// ThanosTracerStreamInterceptor injects the opentracing global tracer into the context
// in order to get it picked up by Thanos components.
func ThanosTracerStreamInterceptor(srv interface{}, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	return handler(srv, wrappedServerStream{
		ctx:          tracing.ContextWithTracer(ss.Context(), opentracing.GlobalTracer()),
		ServerStream: ss,
	})
}

type wrappedServerStream struct {
	ctx context.Context
	grpc.ServerStream
}

func (ss wrappedServerStream) Context() context.Context {
	return ss.ctx
}

type OpenTelemetryProviderBridge struct {
	tracer opentracing.Tracer
}

func NewOpenTelemetryProviderBridge(tracer opentracing.Tracer) *OpenTelemetryProviderBridge {
	return &OpenTelemetryProviderBridge{
		tracer: tracer,
	}
}

// Tracer creates an implementation of the Tracer interface.
// The instrumentationName must be the name of the library providing
// instrumentation. This name may be the same as the instrumented code
// only if that code provides built-in instrumentation. If the
// instrumentationName is empty, then a implementation defined default
// name will be used instead.
//
// This method must be concurrency safe.
func (p *OpenTelemetryProviderBridge) Tracer(instrumentationName string, opts ...trace.TracerOption) trace.Tracer {
	return NewOpenTelemetryTracerBridge(p.tracer, p)
}

type OpenTelemetryTracerBridge struct {
	tracer   opentracing.Tracer
	provider trace.TracerProvider
}

func NewOpenTelemetryTracerBridge(tracer opentracing.Tracer, provider trace.TracerProvider) *OpenTelemetryTracerBridge {
	return &OpenTelemetryTracerBridge{
		tracer:   tracer,
		provider: provider,
	}
}

// Start creates a span and a context.Context containing the newly-created span.
//
// If the context.Context provided in `ctx` contains a Span then the newly-created
// Span will be a child of that span, otherwise it will be a root span. This behavior
// can be overridden by providing `WithNewRoot()` as a SpanOption, causing the
// newly-created Span to be a root span even if `ctx` contains a Span.
//
// When creating a Span it is recommended to provide all known span attributes using
// the `WithAttributes()` SpanOption as samplers will only have access to the
// attributes provided when a Span is created.
//
// Any Span that is created MUST also be ended. This is the responsibility of the user.
// Implementations of this API may leak memory or other resources if Spans are not ended.
func (t *OpenTelemetryTracerBridge) Start(ctx context.Context, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	var mappedOptions []opentracing.StartSpanOption

	// Map supported options.
	if len(opts) > 0 {
		mappedOptions = make([]opentracing.StartSpanOption, 0, len(opts))
		cfg := trace.NewSpanStartConfig(opts...)

		if !cfg.Timestamp().IsZero() {
			mappedOptions = append(mappedOptions, opentracing.StartTime(cfg.Timestamp()))
		}
		if len(cfg.Attributes()) > 0 {
			tags := make(map[string]interface{}, len(cfg.Attributes()))

			for _, attr := range cfg.Attributes() {
				if !attr.Valid() {
					continue
				}

				tags[string(attr.Key)] = attr.Value.AsInterface()
			}

			mappedOptions = append(mappedOptions, opentracing.Tags(tags))
		}
	}

	span, ctx := opentracing.StartSpanFromContextWithTracer(ctx, t.tracer, spanName, mappedOptions...)
	return ctx, NewOpenTelemetrySpanBridge(span, t.provider)
}

type OpenTelemetrySpanBridge struct {
	span     opentracing.Span
	provider trace.TracerProvider
}

func NewOpenTelemetrySpanBridge(span opentracing.Span, provider trace.TracerProvider) *OpenTelemetrySpanBridge {
	return &OpenTelemetrySpanBridge{
		span:     span,
		provider: provider,
	}
}

// End completes the Span. The Span is considered complete and ready to be
// delivered through the rest of the telemetry pipeline after this method
// is called. Therefore, updates to the Span are not allowed after this
// method has been called.
func (s *OpenTelemetrySpanBridge) End(options ...trace.SpanEndOption) {
	if len(options) == 0 {
		s.span.Finish()
		return
	}

	cfg := trace.NewSpanEndConfig(options...)
	s.span.FinishWithOptions(opentracing.FinishOptions{
		FinishTime: cfg.Timestamp(),
	})
}

// AddEvent adds an event with the provided name and options.
func (s *OpenTelemetrySpanBridge) AddEvent(name string, options ...trace.EventOption) {
	cfg := trace.NewEventConfig(options...)
	s.addEvent(name, cfg.Attributes())
}

func (s *OpenTelemetrySpanBridge) addEvent(name string, attributes []attribute.KeyValue) {
	s.logFieldWithAttributes(log.Event(name), attributes)
}

// IsRecording returns the recording state of the Span. It will return
// true if the Span is active and events can be recorded.
func (s *OpenTelemetrySpanBridge) IsRecording() bool {
	return true
}

// RecordError will record err as an exception span event for this span. An
// additional call to SetStatus is required if the Status of the Span should
// be set to Error, as this method does not change the Span status. If this
// span is not being recorded or err is nil then this method does nothing.
func (s *OpenTelemetrySpanBridge) RecordError(err error, options ...trace.EventOption) {
	cfg := trace.NewEventConfig(options...)
	s.recordError(err, cfg.Attributes())
}

func (s *OpenTelemetrySpanBridge) recordError(err error, attributes []attribute.KeyValue) {
	s.logFieldWithAttributes(log.Error(err), attributes)
}

// SpanContext returns the SpanContext of the Span. The returned SpanContext
// is usable even after the End method has been called for the Span.
func (s *OpenTelemetrySpanBridge) SpanContext() trace.SpanContext {
	// We only support Jaeger span context.
	sctx, ok := s.span.Context().(jaeger.SpanContext)
	if !ok {
		return trace.SpanContext{}
	}

	var flags trace.TraceFlags
	flags = flags.WithSampled(sctx.IsSampled())

	return trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    jaegerToOpenTelemetryTraceID(sctx.TraceID()),
		SpanID:     jaegerToOpenTelemetrySpanID(sctx.SpanID()),
		TraceFlags: flags,

		// Unsupported because we can't read it from the Jaeger span context.
		Remote: false,
	})
}

// SetStatus sets the status of the Span in the form of a code and a
// description, overriding previous values set. The description is only
// included in a status when the code is for an error.
func (s *OpenTelemetrySpanBridge) SetStatus(code codes.Code, description string) {
	// We use a log instead of setting tags to have it more prominent in the tracing UI.
	s.span.LogFields(log.Uint32("code", uint32(code)), log.String("description", description))
}

// SetName sets the Span name.
func (s *OpenTelemetrySpanBridge) SetName(name string) {
	s.span.SetOperationName(name)
}

// SetAttributes sets kv as attributes of the Span. If a key from kv
// already exists for an attribute of the Span it will be overwritten with
// the value contained in kv.
func (s *OpenTelemetrySpanBridge) SetAttributes(kv ...attribute.KeyValue) {
	for _, attr := range kv {
		if !attr.Valid() {
			continue
		}

		s.span.SetTag(string(attr.Key), attr.Value.AsInterface())
	}
}

// TracerProvider returns a TracerProvider that can be used to generate
// additional Spans on the same telemetry pipeline as the current Span.
func (s *OpenTelemetrySpanBridge) TracerProvider() trace.TracerProvider {
	return s.provider
}

func (s *OpenTelemetrySpanBridge) logFieldWithAttributes(field log.Field, attributes []attribute.KeyValue) {
	if len(attributes) == 0 {
		s.span.LogFields(field)
		return
	}

	fields := make([]log.Field, 0, 1+len(attributes))
	fields = append(fields, field)

	for _, attr := range attributes {
		if attr.Valid() {
			fields = append(fields, log.Object(string(attr.Key), attr.Value.AsInterface()))
		}
	}

	s.span.LogFields(fields...)
}

func jaegerToOpenTelemetryTraceID(input jaeger.TraceID) trace.TraceID {
	var traceID trace.TraceID
	binary.BigEndian.PutUint64(traceID[0:8], input.High)
	binary.BigEndian.PutUint64(traceID[8:16], input.Low)
	return traceID
}

func jaegerToOpenTelemetrySpanID(input jaeger.SpanID) trace.SpanID {
	var spanID trace.SpanID
	binary.BigEndian.PutUint64(spanID[0:8], uint64(input))
	return spanID
}
