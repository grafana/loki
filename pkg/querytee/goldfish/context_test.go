package goldfish

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSamplingDecisionFromContext_NotSet(t *testing.T) {
	ctx := context.Background()
	decision, ok := SamplingDecisionFromContext(ctx)
	assert.False(t, ok)
	assert.Equal(t, SamplingDecision{}, decision)
}

func TestSamplingDecisionFromContext_Sampled(t *testing.T) {
	ctx := context.Background()
	want := SamplingDecision{Sampled: true, CorrelationID: "test-uuid-123"}
	ctx = ContextWithSamplingDecision(ctx, want)

	got, ok := SamplingDecisionFromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, want, got)
}

func TestSamplingDecisionFromContext_NotSampled(t *testing.T) {
	ctx := context.Background()
	want := SamplingDecision{Sampled: false, CorrelationID: ""}
	ctx = ContextWithSamplingDecision(ctx, want)

	got, ok := SamplingDecisionFromContext(ctx)
	assert.True(t, ok, "negative decisions should be propagated (ok=true)")
	assert.False(t, got.Sampled)
	assert.Equal(t, want, got)
}
