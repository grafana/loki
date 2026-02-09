package xcap

import (
	"context"

	"go.opentelemetry.io/otel/trace"
)

// Tracer wraps a [trace.Tracer] so that every span it creates is paired
// with a [Region] for aggregated observation recording.
//
// The returned [trace.Span] from [Tracer.Start] is an [xcap.Span] whose
// End method flushes Region observations as span attributes before
// ending the underlying OTel span.
//
// All other [trace.Tracer] methods (if any are added in the future) are
// automatically delegated to the inner tracer via embedding.
//
// Tracer implements [trace.Tracer], so it can be used as a drop-in
// replacement anywhere a standard tracer is accepted.
type Tracer struct {
	trace.Tracer
}

// NewTracer creates an xcap [Tracer] that wraps the provided
// [trace.Tracer].
func NewTracer(t trace.Tracer) *Tracer {
	return &Tracer{Tracer: t}
}

// Start creates a new OTel span and a linked [Region].
//
// If a [Capture] is present in ctx, the Region is registered with it
// for summary aggregation via [SummaryLogValues]. If no Capture is
// found, a span is still created but no Region is attached (observation
// recording is a no-op).
//
// The Region is stored in the returned context and can be retrieved
// with [RegionFromContext] for recording observations.
func (t *Tracer) Start(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	ctx, inner := t.Tracer.Start(ctx, name, opts...)

	capture := CaptureFromContext(ctx)
	if capture == nil {
		// No capture — wrap the span but with a nil region.
		// Span.End() will just call inner.End() with nothing to flush.
		return ctx, &Span{Span: inner}
	}

	r := &Region{
		name:         name,
		observations: make(map[StatisticKey]*AggregatedObservation),
	}
	capture.AddRegion(r)

	ctx = ContextWithRegion(ctx, r)
	return ctx, &Span{Span: inner, region: r}
}
