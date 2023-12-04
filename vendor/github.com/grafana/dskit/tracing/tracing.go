// Provenance-includes-location: https://github.com/weaveworks/common/blob/main/tracing/tracing.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: Weaveworks Ltd.

package tracing

import (
	"context"
	"io"

	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	jaeger "github.com/uber/jaeger-client-go"
	jaegercfg "github.com/uber/jaeger-client-go/config"
	jaegerprom "github.com/uber/jaeger-lib/metrics/prometheus"
)

// ErrInvalidConfiguration is an error to notify client to provide valid trace report agent or config server
var (
	ErrBlankTraceConfiguration = errors.New("no trace report agent, config server, or collector endpoint specified")
)

// installJaeger registers Jaeger as the OpenTracing implementation.
func installJaeger(serviceName string, cfg *jaegercfg.Configuration, options ...jaegercfg.Option) (io.Closer, error) {
	metricsFactory := jaegerprom.New()

	// put the metricsFactory earlier so provided options can override it
	opts := append([]jaegercfg.Option{jaegercfg.Metrics(metricsFactory)}, options...)

	closer, err := cfg.InitGlobalTracer(serviceName, opts...)
	if err != nil {
		return nil, errors.Wrap(err, "could not initialize jaeger tracer")
	}
	return closer, nil
}

// NewFromEnv is a convenience function to allow tracing configuration
// via environment variables
//
// Tracing will be enabled if one (or more) of the following environment variables is used to configure trace reporting:
// - JAEGER_AGENT_HOST
// - JAEGER_SAMPLER_MANAGER_HOST_PORT
func NewFromEnv(serviceName string, options ...jaegercfg.Option) (io.Closer, error) {
	cfg, err := jaegercfg.FromEnv()
	if err != nil {
		return nil, errors.Wrap(err, "could not load jaeger tracer configuration")
	}

	if cfg.Sampler.SamplingServerURL == "" && cfg.Reporter.LocalAgentHostPort == "" && cfg.Reporter.CollectorEndpoint == "" {
		return nil, ErrBlankTraceConfiguration
	}

	return installJaeger(serviceName, cfg, options...)
}

// ExtractTraceID extracts the trace id, if any from the context.
func ExtractTraceID(ctx context.Context) (string, bool) {
	sp := opentracing.SpanFromContext(ctx)
	if sp == nil {
		return "", false
	}
	sctx, ok := sp.Context().(jaeger.SpanContext)
	if !ok {
		return "", false
	}

	return sctx.TraceID().String(), true
}

// ExtractSampledTraceID works like ExtractTraceID but the returned bool is only
// true if the returned trace id is sampled.
func ExtractSampledTraceID(ctx context.Context) (string, bool) {
	sp := opentracing.SpanFromContext(ctx)
	if sp == nil {
		return "", false
	}
	sctx, ok := sp.Context().(jaeger.SpanContext)
	if !ok {
		return "", false
	}

	return sctx.TraceID().String(), sctx.IsSampled()
}
