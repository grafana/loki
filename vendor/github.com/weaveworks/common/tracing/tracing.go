package tracing

import (
	"io"

	"github.com/pkg/errors"
	jaegercfg "github.com/uber/jaeger-client-go/config"
	jaegerprom "github.com/uber/jaeger-lib/metrics/prometheus"
)

// ErrInvalidConfiguration is an error to notify client to provide valid trace report agent or config server
var (
	ErrBlankTraceConfiguration = errors.New("no trace report agent or config server specified")
)

// installJaeger registers Jaeger as the OpenTracing implementation.
func installJaeger(serviceName string, cfg *jaegercfg.Configuration) (io.Closer, error) {
	metricsFactory := jaegerprom.New()
	closer, err := cfg.InitGlobalTracer(serviceName, jaegercfg.Metrics(metricsFactory))
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
func NewFromEnv(serviceName string) (io.Closer, error) {
	cfg, err := jaegercfg.FromEnv()
	if err != nil {
		return nil, errors.Wrap(err, "could not load jaeger tracer configuration")
	}

	if cfg.Sampler.SamplingServerURL == "" && cfg.Reporter.LocalAgentHostPort == "" {
		return nil, ErrBlankTraceConfiguration
	}

	return installJaeger(serviceName, cfg)
}
