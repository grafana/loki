package tracing

import (
	"strings"

	tracesdk "go.opentelemetry.io/otel/sdk/trace"
)

// GCSSpanFilter is a sampler that drops all spans from the GCS client library.
// This prevents the GCS client from creating millions of spans in high-throughput
// production environments, as the client creates one span per request with no
// built-in way to disable tracing.
type GCSSpanFilter struct {
	inner tracesdk.Sampler
}

// NewGCSSpanFilter creates a new GCS span filter that wraps the provided inner sampler.
func NewGCSSpanFilter(inner tracesdk.Sampler) *GCSSpanFilter {
	return &GCSSpanFilter{
		inner: inner,
	}
}

// ShouldSample implements tracesdk.Sampler interface.
// It drops all spans from the GCS client and delegates to the inner sampler for all other spans.
func (g *GCSSpanFilter) ShouldSample(parameters tracesdk.SamplingParameters) tracesdk.SamplingResult {
	if strings.HasPrefix(parameters.Name, "cloud.google.com/go/storage") {
		return tracesdk.SamplingResult{Decision: tracesdk.Drop}
	}

	return g.inner.ShouldSample(parameters)
}

// Description implements tracesdk.Sampler interface.
func (g *GCSSpanFilter) Description() string {
	return "GCSSpanFilter - drops all spans from cloud.google.com/go/storage"
}
