package otelpyroscope

import (
	"context"
	"runtime/pprof"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

const (
	spanIDLabelName   = "span_id"
	spanNameLabelName = "span_name"
	traceIDLabelName  = "trace_id"
)

var profileIDSpanAttributeKey = attribute.Key("pyroscope.profile.id")

// tracerProvider satisfies open telemetry TracerProvider interface.
type tracerProvider struct {
	noop.TracerProvider
	tp     trace.TracerProvider
	config config
}

type config struct {
	spanNameScope Scope
	spanIDScope   Scope
}

type Option func(*tracerProvider)

// NewTracerProvider returns a TracerProvider that annotates pprof samples
// with span_id and trace_id labels for trace↔profile correlation.
func NewTracerProvider(tp trace.TracerProvider, options ...Option) trace.TracerProvider {
	p := tracerProvider{
		tp: tp,
		config: config{
			spanNameScope: ScopeRootSpan,
			spanIDScope:   ScopeRootSpan,
		},
	}
	for _, o := range options {
		o(&p)
	}
	return &p
}

func (w *tracerProvider) Tracer(name string, opts ...trace.TracerOption) trace.Tracer {
	return &profileTracer{p: w, tr: w.tp.Tracer(name, opts...)}
}

type profileTracer struct {
	noop.Tracer
	p  *tracerProvider
	tr trace.Tracer
}

func (w *profileTracer) Start(ctx context.Context, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	ctx, span := w.tr.Start(ctx, spanName, opts...)
	spanCtx := span.SpanContext()
	addSpanIDLabel := w.p.config.spanIDScope != ScopeNone && spanCtx.IsSampled()
	addSpanNameLabel := w.p.config.spanNameScope != ScopeNone && spanName != ""
	addTraceIDLabel := spanCtx.IsSampled()
	if !(addSpanIDLabel || addSpanNameLabel || addTraceIDLabel) {
		return ctx, span
	}

	spanID := spanCtx.SpanID().String()
	s := spanWrapper{
		Span: span,
		ctx:  ctx,
		p:    w.p,
	}

	rs, ok := rootSpanFromContext(ctx)
	isLocalTraceEntry := !ok
	if isLocalTraceEntry {
		// This is the first local span.
		rs.id = spanID
		rs.name = spanName
		ctx = withRootSpan(ctx, rs)
	}

	// We mark spans with "pyroscope.profile.id" attribute,
	// only if they _can_ have profiles. Presence of the attribute
	// does not indicate the fact that we actually have collected
	// any samples for the span.
	if (w.p.config.spanIDScope == ScopeRootSpan && spanID == rs.id) ||
		w.p.config.spanIDScope == ScopeAllSpans {
		span.SetAttributes(profileIDSpanAttributeKey.String(spanID))
	}
	labels := make([]string, 0, 6)
	if addSpanNameLabel {
		if w.p.config.spanNameScope == ScopeRootSpan {
			spanName = rs.name
		}
		labels = append(labels, spanNameLabelName, spanName)
	}
	if addSpanIDLabel {
		if w.p.config.spanIDScope == ScopeRootSpan {
			spanID = rs.id
		}
		labels = append(labels, spanIDLabelName, spanID)
	}
	// Set on the local root only; descendants inherit via pprof label merge.
	if addTraceIDLabel && isLocalTraceEntry {
		labels = append(labels, traceIDLabelName, spanCtx.TraceID().String())
	}

	ctx = pprof.WithLabels(ctx, pprof.Labels(labels...))
	pprof.SetGoroutineLabels(ctx)
	return ctx, &s
}

type spanWrapper struct {
	trace.Span
	ctx context.Context
	p   *tracerProvider
}

func (s spanWrapper) End(options ...trace.SpanEndOption) {
	s.Span.End(options...)
	pprof.SetGoroutineLabels(s.ctx)
}

type rootSpanCtxKey struct{}

type rootSpan struct {
	id   string
	name string
}

func withRootSpan(ctx context.Context, s rootSpan) context.Context {
	return context.WithValue(ctx, rootSpanCtxKey{}, s)
}

func rootSpanFromContext(ctx context.Context) (rootSpan, bool) {
	s, ok := ctx.Value(rootSpanCtxKey{}).(rootSpan)
	return s, ok
}

// WithSpanNameLabelScope specifies whether the current span name should be
// added to the profile labels. If the name is dynamic, i.e. includes
// span-specific identifiers, such as URL or SQL query, this may significantly
// deteriorate performance.
//
// By default, only the local root span name is recorded. Samples collected
// during the child span execution will be included into the root span profile.
func WithSpanNameLabelScope(s Scope) Option {
	return func(tp *tracerProvider) {
		tp.config.spanNameScope = s
	}
}

// WithSpanIDLabelScope specifies whether the current span ID should be added to
// the profile labels.
//
// By default, only the local root span ID is recorded. Samples collected
// during the child span execution will be included into the root span profile.
func WithSpanIDLabelScope(s Scope) Option {
	return func(tp *tracerProvider) {
		tp.config.spanIDScope = s
	}
}

type Scope uint

const (
	ScopeNone Scope = iota
	ScopeRootSpan
	ScopeAllSpans
)
