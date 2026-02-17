package xcap

import (
	"go.opentelemetry.io/otel/trace"
)

// Span wraps a [trace.Span] and an optional [Region].
//
// All [trace.Span] methods are automatically delegated to the inner
// span via embedding. Only [End] is overridden to flush the Region's
// aggregated observations as span attributes before ending the inner
// span.
type Span struct {
	trace.Span
	region *Region
}

var _ trace.Span = (*Span)(nil)

// End flushes aggregated observations from the linked Region as span
// attributes, then ends the underlying OTel span.
//
// If no Region is linked (nil), or the Region has already been ended,
// End simply ends the inner span.
func (s *Span) End(options ...trace.SpanEndOption) {
	if s.region != nil {
		s.region.flushToSpan(s.Span)
	}
	s.Span.End(options...)
}

// Region returns the linked Region, or nil if no Region is attached.
func (s *Span) Region() *Region {
	if s == nil {
		return nil
	}

	return s.region
}

// Record records the given observation into the linked Region.
func (s *Span) Record(observation Observation) {
	if s == nil {
		return
	}

	s.region.Record(observation)
}
