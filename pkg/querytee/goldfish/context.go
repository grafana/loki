package goldfish

import "context"

const (
	// GoldfishCorrelationIDHeader is the HTTP header name for the goldfish correlation ID.
	GoldfishCorrelationIDHeader = "X-Loki-Goldfish-ID"
	// GoldfishCorrelationIDQueryTagKey is the query-tag key used to include Goldfish correlation ID.
	GoldfishCorrelationIDQueryTagKey = "goldfish_correlation_id"
)

// SamplingDecision represents an upstream goldfish sampling decision.
// Both positive and negative decisions are stored so downstream handlers
// do not re-evaluate sampling independently.
type SamplingDecision struct {
	Sampled       bool
	CorrelationID string // non-empty only when Sampled is true
}

type contextKey int

const samplingDecisionKey contextKey = iota

// ContextWithSamplingDecision stores a SamplingDecision in the context.
func ContextWithSamplingDecision(ctx context.Context, decision SamplingDecision) context.Context {
	return context.WithValue(ctx, samplingDecisionKey, decision)
}

// SamplingDecisionFromContext retrieves the SamplingDecision from context.
// Returns the decision and true if found, or a zero-value decision and false if not set.
func SamplingDecisionFromContext(ctx context.Context) (SamplingDecision, bool) {
	decision, ok := ctx.Value(samplingDecisionKey).(SamplingDecision)
	return decision, ok
}
