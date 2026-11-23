// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

// Package tracing contains the definitions needed to support distributed tracing.
package tracing

import (
	"context"
)

// ProviderOptions contains the optional values when creating a Provider.
type ProviderOptions struct {
	// for future expansion
}

// NewProvider creates a new Provider with the specified values.
//   - newTracerFn is the underlying implementation for creating Tracer instances
//   - options contains optional values; pass nil to accept the default value
func NewProvider(newTracerFn func(name, version string) Tracer, options *ProviderOptions) Provider {
	return Provider{
		newTracerFn: newTracerFn,
	}
}

// Provider is the factory that creates Tracer instances.
// It defaults to a no-op provider.
type Provider struct {
	newTracerFn func(name, version string) Tracer
}

// NewTracer creates a new Tracer for the specified module name and version.
//   - module - the fully qualified name of the module
//   - version - the version of the module
func (p Provider) NewTracer(module, version string) (tracer Tracer) {
	if p.newTracerFn != nil {
		tracer = p.newTracerFn(module, version)
	}
	return
}

/////////////////////////////////////////////////////////////////////////////////////////////////////////////

// TracerOptions contains the optional values when creating a Tracer.
type TracerOptions struct {
	// SpanFromContext contains the implementation for the Tracer.SpanFromContext method.
	SpanFromContext func(context.Context) Span
}

// NewTracer creates a Tracer with the specified values.
//   - newSpanFn is the underlying implementation for creating Span instances
//   - options contains optional values; pass nil to accept the default value
func NewTracer(newSpanFn func(ctx context.Context, spanName string, options *SpanOptions) (context.Context, Span), options *TracerOptions) Tracer {
	if options == nil {
		options = &TracerOptions{}
	}
	return Tracer{
		newSpanFn:         newSpanFn,
		spanFromContextFn: options.SpanFromContext,
	}
}

// WrapTracer decorates tracer using wrap while preserving its attributes.
// The tracer passed to wrap has no attributes, so its Start method can be called
// without applying them twice. The returned tracer applies the original attributes
// before any attributes added by wrap or subsequent calls to SetAttributes.
// The original tracer is not modified. wrap must not be nil.
//
// wrap is responsible for forwarding span creation and providing span lookup
// behavior on the returned tracer using [TracerOptions.SpanFromContext]. If wrap decorates
// spans, it should also decorate spans retrieved from the underlying tracer's
// SpanFromContext method so callers cannot bypass the decorator.
// If the original tracer has no span lookup implementation, the returned tracer's
// lookup implementation is cleared so SpanFromContext continues to return a zero-value span.
//
// To preserve disabled-tracer behavior, return tracer unchanged when its Enabled
// method returns false. Constructing a tracer with a non-nil span creation function
// makes it report enabled, even if that function delegates to a disabled tracer.
func WrapTracer(tracer Tracer, wrap func(Tracer) Tracer) Tracer {
	attrs := tracer.attrs
	tracer.attrs = nil
	wrapped := wrap(tracer)
	if tracer.spanFromContextFn == nil {
		wrapped.spanFromContextFn = nil
	}
	wrapped.attrs = append(append([]Attribute(nil), attrs...), wrapped.attrs...)
	return wrapped
}

// Tracer is the factory that creates Span instances.
type Tracer struct {
	attrs             []Attribute
	newSpanFn         func(ctx context.Context, spanName string, options *SpanOptions) (context.Context, Span)
	spanFromContextFn func(ctx context.Context) Span
}

// Start creates a new span and a context.Context that contains it.
//   - ctx is the parent context for this span. If it contains a Span, the newly created span will be a child of that span, else it will be a root span
//   - spanName identifies the span within a trace, it's typically the fully qualified API name
//   - options contains optional values for the span, pass nil to accept any defaults
func (t Tracer) Start(ctx context.Context, spanName string, options *SpanOptions) (context.Context, Span) {
	if t.newSpanFn != nil {
		opts := SpanOptions{}
		if options != nil {
			opts = *options
		}
		opts.Attributes = append(opts.Attributes, t.attrs...)
		return t.newSpanFn(ctx, spanName, &opts)
	}
	return ctx, Span{}
}

// SetAttributes sets attrs to be applied to each Span. If a key from attrs
// already exists for an attribute of the Span it will be overwritten with
// the value contained in attrs.
func (t *Tracer) SetAttributes(attrs ...Attribute) {
	t.attrs = append(t.attrs, attrs...)
}

// Enabled returns true if this Tracer is capable of creating Spans.
func (t Tracer) Enabled() bool {
	return t.newSpanFn != nil
}

// SpanFromContext returns the Span associated with the current context.
// If the provided context has no Span, false is returned.
func (t Tracer) SpanFromContext(ctx context.Context) Span {
	if t.spanFromContextFn != nil {
		return t.spanFromContextFn(ctx)
	}
	return Span{}
}

// SpanOptions contains optional settings for creating a span.
type SpanOptions struct {
	// Kind indicates the kind of Span.
	Kind SpanKind

	// Attributes contains key-value pairs of attributes for the span.
	Attributes []Attribute
}

/////////////////////////////////////////////////////////////////////////////////////////////////////////////

// SpanImpl abstracts the underlying implementation for Span,
// allowing it to work with various tracing implementations.
// Any zero-values will have their default, no-op behavior.
type SpanImpl struct {
	// End contains the implementation for the Span.End method.
	End func()

	// SetAttributes contains the implementation for the Span.SetAttributes method.
	SetAttributes func(...Attribute)

	// AddEvent contains the implementation for the Span.AddEvent method.
	AddEvent func(string, ...Attribute)

	// SetStatus contains the implementation for the Span.SetStatus method.
	SetStatus func(SpanStatus, string)
}

// NewSpan creates a Span with the specified implementation.
func NewSpan(impl SpanImpl) Span {
	return Span{
		impl: impl,
	}
}

// Span is a single unit of a trace.  A trace can contain multiple spans.
// A zero-value Span provides a no-op implementation.
type Span struct {
	impl SpanImpl
}

// End terminates the span and MUST be called before the span leaves scope.
// Any further updates to the span will be ignored after End is called.
func (s Span) End() {
	if s.impl.End != nil {
		s.impl.End()
	}
}

// SetAttributes sets the specified attributes on the Span.
// Any existing attributes with the same keys will have their values overwritten.
func (s Span) SetAttributes(attrs ...Attribute) {
	if s.impl.SetAttributes != nil {
		s.impl.SetAttributes(attrs...)
	}
}

// AddEvent adds a named event with an optional set of attributes to the span.
func (s Span) AddEvent(name string, attrs ...Attribute) {
	if s.impl.AddEvent != nil {
		s.impl.AddEvent(name, attrs...)
	}
}

// SetStatus sets the status on the span along with a description.
func (s Span) SetStatus(code SpanStatus, desc string) {
	if s.impl.SetStatus != nil {
		s.impl.SetStatus(code, desc)
	}
}

/////////////////////////////////////////////////////////////////////////////////////////////////////////////

// Attribute is a key-value pair.
type Attribute struct {
	// Key is the name of the attribute.
	Key string

	// Value is the attribute's value.
	// Types that are natively supported include int64, float64, int, bool, string.
	// Any other type will be formatted per rules of fmt.Sprintf("%v").
	Value any
}
