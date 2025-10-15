package aws

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/grafana/dskit/backoff"
	attribute "go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Map Cortex Backoff into AWS Retryer interface
type retryer struct {
	aws.Retryer
	*backoff.Backoff
	maxRetries int
	context.Context
}

func newRetryer(ctx context.Context, cfg backoff.Config) *retryer {
	return &retryer{
		retry.AddWithMaxBackoffDelay(retry.AddWithMaxAttempts(retry.NewStandard(), cfg.MaxRetries), cfg.MaxBackoff),
		backoff.New(ctx, cfg),
		cfg.MaxRetries, ctx}
}

// MaxAttempts is the number of times a request may be retried before
// failing.
func (r *retryer) MaxAttempts() int {
	return r.maxRetries
}

// RetryRules return the retry delay that should be used by the SDK before
// making another request attempt for the failed request.
func (r *retryer) RetryDelay(_ int, _ error) (time.Duration, error) {
	duration := r.NextDelay()
	trace.SpanFromContext(r.Context).SetAttributes(attribute.Int("retry", r.NumRetries()))
	return duration, nil
}
