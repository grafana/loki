package xcap

import (
	"context"

	"go.opentelemetry.io/otel/trace"
)

// StartSpan creates a new OTel span using the given tracer and pairs it
// with a [Region] for aggregated observation recording.
//
// The returned [trace.Span] is an [xcap.Span] whose End method flushes
// Region observations as span attributes before ending the underlying
// OTel span.
//
// If a [Capture] is present in ctx, the Region is registered with it
// for summary aggregation via [SummaryLogValues]. If no Capture is
// found, a span is still created but no Region is attached (observation
// recording is a no-op).
//
// The Region is stored in the returned context and can be retrieved
// with [RegionFromContext] for recording observations.
func StartSpan(ctx context.Context, t trace.Tracer, name string, opts ...trace.SpanStartOption) (context.Context, *Span) {
	ctx, inner := t.Start(ctx, name, opts...)
	ctx, r := StartRegion(ctx, name)
	return ctx, &Span{Span: inner, region: r}
}
