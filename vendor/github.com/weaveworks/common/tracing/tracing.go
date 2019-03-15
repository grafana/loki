package tracing

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"

	jaegercfg "github.com/uber/jaeger-client-go/config"
	jaegerprom "github.com/uber/jaeger-lib/metrics/prometheus"
)

// installJaeger registers Jaeger as the OpenTracing implementation.
func installJaeger(serviceName string, cfg *jaegercfg.Configuration) io.Closer {
	metricsFactory := jaegerprom.New()
	closer, err := cfg.InitGlobalTracer(serviceName, jaegercfg.Metrics(metricsFactory))
	if err != nil {
		fmt.Printf("Could not initialize jaeger tracer: %s\n", err.Error())
		os.Exit(1)
	}
	return closer
}

// NewFromEnv is a convenience function to allow tracing configuration
// via environment variables
//
// Tracing will be enabled if one (or more) of the following environment variables is used to configure trace reporting:
// - JAEGER_AGENT_HOST
// - JAEGER_SAMPLER_MANAGER_HOST_PORT
func NewFromEnv(serviceName string) io.Closer {
	cfg, err := jaegercfg.FromEnv()
	if err != nil {
		fmt.Printf("Could not load jaeger tracer configuration: %s\n", err.Error())
		os.Exit(1)
	}

	if cfg.Sampler.SamplingServerURL == "" && cfg.Reporter.LocalAgentHostPort == "" {
		fmt.Printf("Jaeger tracer disabled: No trace report agent or config server specified\n")
		return ioutil.NopCloser(nil)
	}

	return installJaeger(serviceName, cfg)
}
