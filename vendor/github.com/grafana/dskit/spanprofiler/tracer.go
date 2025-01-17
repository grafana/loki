package spanprofiler

import (
	"context"
	"unsafe"

	"github.com/opentracing/opentracing-go"
	"github.com/uber/jaeger-client-go"
)

const (
	profileIDTagKey = "pyroscope.profile.id"

	spanIDLabelName   = "span_id"
	spanNameLabelName = "span_name"
)

type tracer struct{ opentracing.Tracer }

// NewTracer creates a new opentracing.Tracer with the span profiler integrated.
//
// For efficiency, the tracer selectively records profiles for _root_ spans
// — the initial _local_ span in a process — since a trace may encompass
// thousands of spans. All stack trace samples accumulated during the execution
// of their child spans contribute to the root span's profile. In practical
// terms, this signifies that, for instance, an HTTP request results in a
// singular profile, irrespective of the numerous spans within the trace. It's
// important to note that these profiles don't extend beyond the boundaries of
// a single process.
//
// The limitation of this approach is that only spans created within the same
// goroutine, or its children, as the parent are taken into account.
// Consequently, in scenarios involving asynchronous execution, where the parent
// span context is passed to another goroutine, explicit profiling becomes
// necessary using `spanprofiler.StartSpanFromContext`.
func NewTracer(tr opentracing.Tracer) opentracing.Tracer { return &tracer{tr} }

func (t *tracer) StartSpan(operationName string, opts ...opentracing.StartSpanOption) opentracing.Span {
	span := t.Tracer.StartSpan(operationName, opts...)
	spanCtx, ok := span.Context().(jaeger.SpanContext)
	if !ok {
		return span
	}
	if !spanCtx.IsSampled() {
		return span
	}
	// pprof labels are attached only once, at the span root level.
	if !isRootSpan(opts...) {
		return span
	}
	// The pprof label API assumes that pairs of labels are passed through the
	// context. Unfortunately, the opentracing Tracer API doesn't match this
	// concept: this makes it impossible to save an existing pprof context and
	// all the original pprof labels associated with the goroutine.
	ctx := context.Background()
	return wrapJaegerSpanWithGoroutineLabels(ctx, span, operationName, sampledSpanID(spanCtx))
}

// isRootSpan reports whether the span is a root span.
//
// There are only two valid cases: if the span is the first span in the trace,
// or is the first _local_ span in the trace.
//
// An exception is made for FollowsFrom reference: spans without an explicit
// parent are considered as root ones.
func isRootSpan(opts ...opentracing.StartSpanOption) bool {
	parent, ok := parentSpanContextFromRef(opts...)
	return !ok || isRemoteSpan(parent)
}

// parentSpanContextFromRef returns the first parent reference.
func parentSpanContextFromRef(options ...opentracing.StartSpanOption) (sc jaeger.SpanContext, ok bool) {
	var sso opentracing.StartSpanOptions
	for _, option := range options {
		option.Apply(&sso)
	}
	for _, ref := range sso.References {
		if ref.Type == opentracing.ChildOfRef && ref.ReferencedContext != nil {
			sc, ok = ref.ReferencedContext.(jaeger.SpanContext)
			return sc, ok
		}
	}
	return sc, ok
}

// isRemoteSpan reports whether the span context represents a remote parent.
//
// NOTE(kolesnikovae): this is ugly, but the only reliable method I found.
// The opentracing-go package and Jaeger client are not meant to change as
// both are deprecated.
func isRemoteSpan(c jaeger.SpanContext) bool {
	jaegerCtx := *(*jaegerSpanCtx)(unsafe.Pointer(&c))
	return jaegerCtx.remote
}

// jaegerSpanCtx represents memory layout of the jaeger.SpanContext type.
type jaegerSpanCtx struct {
	traceID  [16]byte   // TraceID
	spanID   [8]byte    // SpanID
	parentID [8]byte    // SpanID
	baggage  uintptr    // map[string]string
	debugID  [2]uintptr // string

	// samplingState is a pointer to a struct that has "localRootSpan" member,
	// which we could probably use: that would allow omitting quite expensive
	// parentSpanContextFromRef call. However, interpreting the pointer and
	// the complex struct memory layout is more complicated and dangerous.
	samplingState uintptr

	// remote indicates that span context represents a remote parent
	remote bool
}
