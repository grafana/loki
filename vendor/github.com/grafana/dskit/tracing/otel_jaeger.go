package tracing

import (
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"

	otelpyroscope "github.com/grafana/otel-profiling-go"
	"github.com/pkg/errors"
	jaegerpropagator "go.opentelemetry.io/contrib/propagators/jaeger"
	"go.opentelemetry.io/contrib/samplers/jaegerremote"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"

	//nolint:staticcheck
	jaegerotel "go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/propagation"
)

const (
	envJaegerAgentHost                 = "JAEGER_AGENT_HOST"
	envJaegerTags                      = "JAEGER_TAGS"
	envJaegerSamplerManagerHostPort    = "JAEGER_SAMPLER_MANAGER_HOST_PORT"
	envJaegerSamplerParam              = "JAEGER_SAMPLER_PARAM"
	envJaegerEndpoint                  = "JAEGER_ENDPOINT"
	envJaegerAgentPort                 = "JAEGER_AGENT_PORT"
	envJaegerSamplerType               = "JAEGER_SAMPLER_TYPE"
	envJaegerSamplingEndpoint          = "JAEGER_SAMPLING_ENDPOINT"
	envJaegerDefaultSamplingServerPort = 5778
	envJaegerDefaultUDPSpanServerHost  = "localhost"
	envJaegerDefaultUDPSpanServerPort  = "6831"
)

// NewOTelFromJaegerEnv is a convenience function to allow OTel tracing configuration via Jaeger environment variables
//
// Tracing will be enabled if one (or more) of the following environment variables is used to configure trace reporting:
// - JAEGER_AGENT_HOST
// - JAEGER_SAMPLER_MANAGER_HOST_PORT
func NewOTelFromJaegerEnv(serviceName string) (io.Closer, error) {
	cfg, err := parseOTelConfig()
	if err != nil {
		return nil, errors.Wrap(err, "could not load jaeger tracer configuration")
	}
	if cfg.samplingServerURL == "" && cfg.agentHostPort == "" && cfg.jaegerEndpoint == "" {
		return nil, ErrBlankTraceConfiguration
	}
	return cfg.initJaegerTracerProvider(serviceName)
}

// parseJaegerTags Parse Jaeger tags from env var JAEGER_TAGS, example of TAGs format: key1=value1,key2=${value2:value3} where value2 is an env var
// and value3 is the default value, which is optional.
func parseJaegerTags(sTags string) ([]attribute.KeyValue, error) {
	pairs := strings.Split(sTags, ",")
	res := make([]attribute.KeyValue, 0, len(pairs))
	for _, p := range pairs {
		k, v, found := strings.Cut(p, "=")
		if found {
			k, v := strings.TrimSpace(k), strings.TrimSpace(v)
			if strings.HasPrefix(v, "${") && strings.HasSuffix(v, "}") {
				e, d, _ := strings.Cut(v[2:len(v)-1], ":")
				v = os.Getenv(e)
				if v == "" && d != "" {
					v = d
				}
			}
			if v == "" {
				return nil, errors.Errorf("invalid tag %q, expected key=value", p)
			}
			res = append(res, attribute.String(k, v))
		} else if p != "" {
			return nil, errors.Errorf("invalid tag %q, expected key=value", p)
		}
	}
	return res, nil
}

type otelConfig struct {
	agentHost         string
	jaegerEndpoint    string
	agentPort         string
	samplerType       string
	samplingServerURL string
	samplerParam      float64
	jaegerTags        []attribute.KeyValue
	agentHostPort     string
}

// parseOTelConfig facilitates initialization that is compatible with Jaeger's InitGlobalTracer method.
func parseOTelConfig() (otelConfig, error) {
	cfg := otelConfig{}
	var err error

	// Parse reporting agent configuration
	if e := os.Getenv(envJaegerEndpoint); e != "" {
		u, err := url.ParseRequestURI(e)
		if err != nil {
			return cfg, errors.Wrapf(err, "cannot parse env var %s=%s", envJaegerEndpoint, e)
		}
		cfg.jaegerEndpoint = u.String()
	} else {
		useEnv := false
		host := envJaegerDefaultUDPSpanServerHost
		if e := os.Getenv(envJaegerAgentHost); e != "" {
			host = e
			useEnv = true
		}

		port := envJaegerDefaultUDPSpanServerPort
		if e := os.Getenv(envJaegerAgentPort); e != "" {
			port = e
			useEnv = true
		}

		if useEnv || cfg.agentHostPort == "" {
			cfg.agentHost = host
			cfg.agentPort = port
			cfg.agentHostPort = net.JoinHostPort(host, port)
		}
	}

	// Then parse the sampler Configuration
	if e := os.Getenv(envJaegerSamplerType); e != "" {
		cfg.samplerType = e
	}

	if e := os.Getenv(envJaegerSamplerParam); e != "" {
		if value, err := strconv.ParseFloat(e, 64); err == nil {
			cfg.samplerParam = value
		} else {
			return cfg, errors.Wrapf(err, "cannot parse env var %s=%s", envJaegerSamplerParam, e)
		}
	}

	if e := os.Getenv(envJaegerSamplingEndpoint); e != "" {
		cfg.samplingServerURL = e
	} else if e := os.Getenv(envJaegerSamplerManagerHostPort); e != "" {
		cfg.samplingServerURL = e
	} else if e := os.Getenv(envJaegerAgentHost); e != "" {
		// Fallback if we know the agent host - try the sampling endpoint there
		cfg.samplingServerURL = fmt.Sprintf("http://%s:%d/sampling", e, envJaegerDefaultSamplingServerPort)
	}

	// When sampling server URL is set, we use the remote sampler
	if cfg.samplingServerURL != "" && cfg.samplerType == "" {
		cfg.samplerType = "remote"
	}

	// Parse tags
	cfg.jaegerTags, err = parseJaegerTags(os.Getenv(envJaegerTags))
	if err != nil {
		return cfg, errors.Wrapf(err, "could not parse %s", envJaegerTags)
	}
	return cfg, nil
}

// initJaegerTracerProvider initializes a new Jaeger Tracer Provider.
func (cfg otelConfig) initJaegerTracerProvider(serviceName string) (io.Closer, error) {
	// Read environment variables to configure Jaeger
	var ep jaegerotel.EndpointOption
	// Create the jaeger exporter: address can be either agent address (host:port) or collector Endpoint.
	if cfg.agentHostPort != "" {
		ep = jaegerotel.WithAgentEndpoint(
			jaegerotel.WithAgentHost(cfg.agentHost),
			jaegerotel.WithAgentPort(cfg.agentPort))
	} else {
		ep = jaegerotel.WithCollectorEndpoint(
			jaegerotel.WithEndpoint(cfg.jaegerEndpoint))
	}
	exp, err := jaegerotel.New(ep)

	if err != nil {
		return nil, err
	}

	// Configure sampling strategy
	sampler := tracesdk.AlwaysSample()
	if cfg.samplerType == "const" {
		if cfg.samplerParam == 0 {
			sampler = tracesdk.NeverSample()
		}
	} else if cfg.samplerType == "probabilistic" {
		tracesdk.TraceIDRatioBased(cfg.samplerParam)
	} else if cfg.samplerType == "remote" {
		sampler = jaegerremote.New(serviceName, jaegerremote.WithSamplingServerURL(cfg.samplingServerURL),
			jaegerremote.WithInitialSampler(tracesdk.TraceIDRatioBased(cfg.samplerParam)))
	} else if cfg.samplerType != "" {
		return nil, errors.Errorf("unknown sampler type %q", cfg.samplerType)
	}
	customAttrs := cfg.jaegerTags
	customAttrs = append(customAttrs,
		attribute.String("samplerType", cfg.samplerType),
		attribute.Float64("samplerParam", cfg.samplerParam),
		attribute.String("samplingServerURL", cfg.samplingServerURL),
	)
	res, err := NewResource(serviceName, customAttrs)
	if err != nil {
		return nil, err
	}

	tp := tracesdk.NewTracerProvider(
		tracesdk.WithBatcher(exp),
		tracesdk.WithResource(res),
		tracesdk.WithSampler(sampler),
	)

	// Set TracerProvider with pyroscope profiling.
	otel.SetTracerProvider(otelpyroscope.NewTracerProvider(tp))

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator([]propagation.TextMapPropagator{
		// w3c Propagator is the default propagator for opentelemetry
		propagation.TraceContext{}, propagation.Baggage{},
		// jaeger Propagator is for opentracing backwards compatibility
		jaegerpropagator.Jaeger{},
	}...))
	return &Closer{tp}, nil
}
