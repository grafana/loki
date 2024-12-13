package spanprofiler

import (
	"context"
	"runtime/pprof"

	"github.com/opentracing/opentracing-go"
	"github.com/uber/jaeger-client-go"
)

// StartSpanFromContext starts and returns a Span with `operationName`, using
// any Span found within `ctx` as a ChildOfRef. If no such parent could be
// found, StartSpanFromContext creates a root (parentless) Span.
//
// The call sets `operationName` as `span_name` pprof label, and the new span
// identifier as `span_id` pprof label, if the trace is sampled.
//
// The second return value is a context.Context object built around the
// returned Span.
//
// Example usage:
//
//	SomeFunction(ctx context.Context, ...) {
//	    sp, ctx := opentracing.StartSpanFromContext(ctx, "SomeFunction")
//	    defer sp.Finish()
//	    ...
//	}
func StartSpanFromContext(ctx context.Context, operationName string, opts ...opentracing.StartSpanOption) (opentracing.Span, context.Context) {
	return StartSpanFromContextWithTracer(ctx, opentracing.GlobalTracer(), operationName, opts...)
}

// StartSpanFromContextWithTracer starts and returns a span with `operationName`
// using  a span found within the context as a ChildOfRef. If that doesn't exist
// it creates a root span. It also returns a context.Context object built
// around the returned span.
//
// The call sets `operationName` as `span_name` pprof label, and the new span
// identifier as `span_id` pprof label, if the trace is sampled.
//
// It's behavior is identical to StartSpanFromContext except that it takes an explicit
// tracer as opposed to using the global tracer.
func StartSpanFromContextWithTracer(ctx context.Context, tracer opentracing.Tracer, operationName string, opts ...opentracing.StartSpanOption) (opentracing.Span, context.Context) {
	span, ctx := opentracing.StartSpanFromContextWithTracer(ctx, tracer, operationName, opts...)
	spanCtx, ok := span.Context().(jaeger.SpanContext)
	if ok {
		span = wrapJaegerSpanWithGoroutineLabels(ctx, span, operationName, sampledSpanID(spanCtx))
	}
	return span, ctx
}

func wrapJaegerSpanWithGoroutineLabels(
	parentCtx context.Context,
	span opentracing.Span,
	operationName string,
	spanID string,
) *spanWrapper {
	// Note that pprof labels are propagated through the goroutine's local
	// storage and are always copied to child goroutines. This way, stack
	// trace samples collected during execution of child spans will be taken
	// into account at the root.
	var ctx context.Context
	if spanID != "" {
		ctx = pprof.WithLabels(parentCtx, pprof.Labels(
			spanNameLabelName, operationName,
			spanIDLabelName, spanID))
	} else {
		// Even if the trace has not been sampled, we still need to keep track
		// of samples that belong to the span (all spans with the given name).
		ctx = pprof.WithLabels(parentCtx, pprof.Labels(
			spanNameLabelName, operationName))
	}
	// Goroutine labels should be set as early as possible,
	// in order to capture the overhead of the function call.
	pprof.SetGoroutineLabels(ctx)
	// We create a span wrapper to ensure we remove the newly attached pprof
	// labels when span finishes. The need of this wrapper is questioned:
	// as we do not have the original context, we could leave the goroutine
	// labels – normally, span is finished at the very end of the goroutine's
	// lifetime, so no significant side effects should take place.
	w := spanWrapper{
		parentPprofCtx:  parentCtx,
		currentPprofCtx: ctx,
	}
	w.Span = span.SetTag(profileIDTagKey, spanID)
	return &w
}

type spanWrapper struct {
	parentPprofCtx  context.Context
	currentPprofCtx context.Context
	opentracing.Span
}

func (s *spanWrapper) Finish() {
	s.Span.Finish()
	pprof.SetGoroutineLabels(s.parentPprofCtx)
	s.currentPprofCtx = s.parentPprofCtx
}

// sampledSpanID returns the span ID, if the span is sampled,
// otherwise an empty string is returned.
func sampledSpanID(spanCtx jaeger.SpanContext) string {
	if spanCtx.IsSampled() {
		return spanCtx.SpanID().String()
	}
	return ""
}
