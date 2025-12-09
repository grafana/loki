package querytee

import (
	"net/http"
	"time"

	"github.com/go-kit/log"

	"github.com/grafana/loki/v3/pkg/engine"
	"github.com/grafana/loki/v3/pkg/lokifrontend/frontend"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/tools/querytee/comparator"
	"github.com/grafana/loki/v3/tools/querytee/goldfish"
)

// HandlerFactory creates the appropriate handler based on configuration.
type HandlerFactory struct {
	backends           []*ProxyBackend
	codec              queryrangebase.Codec
	goldfishManager    *goldfish.Manager
	instrumentCompares bool
	enableRace         bool
	logger             log.Logger
	metrics            *ProxyMetrics
}

// HandlerFactoryConfig holds configuration for creating a HandlerFactory.
type HandlerFactoryConfig struct {
	Backends           []*ProxyBackend
	Codec              queryrangebase.Codec
	GoldfishManager    *goldfish.Manager
	InstrumentCompares bool
	EnableRace         bool
	Logger             log.Logger
	Metrics            *ProxyMetrics
}

// NewHandlerFactory creates a new HandlerFactory.
func NewHandlerFactory(cfg HandlerFactoryConfig) *HandlerFactory {
	return &HandlerFactory{
		backends:           cfg.Backends,
		codec:              cfg.Codec,
		goldfishManager:    cfg.GoldfishManager,
		instrumentCompares: cfg.InstrumentCompares,
		enableRace:         cfg.EnableRace,
		logger:             cfg.Logger,
		metrics:            cfg.Metrics,
	}
}

// CreateHandler creates the appropriate handler based on configuration.
// If ComparisonMinAge is 0 (legacy mode), it returns a FanOutHandler directly.
// If ComparisonMinAge > 0 (splitting mode), it wraps the FanOutHandler with engineRouter
// middleware to split queries based on data age.
func (f *HandlerFactory) CreateHandler(routeName string, comp comparator.ResponsesComparator, forMetricQuery bool) queryrangebase.Handler {
	// Create the fan-out handler that sends requests to all backends
	fanOutHandler := NewFanOutHandler(FanOutHandlerConfig{
		Backends:           f.backends,
		Codec:              f.codec,
		Comparator:         comp,
		GoldfishManager:    f.goldfishManager,
		InstrumentCompares: f.instrumentCompares,
		EnableRace:         f.enableRace,
		Logger:             f.logger,
		Metrics:            f.metrics,
		RouteName:          routeName,
	})

	if f.goldfishManager == nil || f.goldfishManager.ComparisonMinAge() == 0 {
		return fanOutHandler
	}

	return f.createSplittingHandler(fanOutHandler, forMetricQuery)
}

// createSplittingHandler creates a handler that splits queries based on data age.
func (f *HandlerFactory) createSplittingHandler(fanOutHandler *FanOutHandler, forMetricQuery bool) queryrangebase.Handler {
	var preferredBackend *ProxyBackend
	for _, b := range f.backends {
		if b.preferred {
			preferredBackend = b
			break
		}
	}

	if preferredBackend == nil {
		// No preferred backend, can't do splitting - fall back to fan-out
		return fanOutHandler
	}

	// Create downstream round tripper for recent queries (preferred backend only)
	preferredRT, err := frontend.NewDownstreamRoundTripper(
		preferredBackend.endpoint.String(),
		//TODO(twhitney): do we have this config already somewhere?
		&http.Transport{
			MaxIdleConnsPerHost: 100,
			IdleConnTimeout:     90 * time.Second,
		},
		f.codec,
	)
	if err != nil {
		// Fall back to fan-out handler if we can't create the downstream RT
		return fanOutHandler
	}

	routerConfig := queryrange.RouterConfig{
		Enabled:  true,
		Start:    f.goldfishManager.ComparisonStartDate(),
		Lag:      f.goldfishManager.ComparisonMinAge(),
		Validate: engine.IsQuerySupported,
		Handler:  fanOutHandler, // v2Next: fan-out to all backends for goldfish
	}

	middleware := []queryrangebase.Middleware{}
	if forMetricQuery {
		middleware = append(middleware, queryrangebase.StepAlignMiddleware)
	}

	// Create the engine router engineRouterMiddleware
	engineRouterMiddleware := queryrange.NewEngineRouterMiddleware(
		routerConfig,
		nil, // no v1 chain middleware
		f.codec,
		forMetricQuery,
		f.logger,
	)
	middleware = append(middleware, engineRouterMiddleware)

	// Wrap the preferred backend handler (v1Next) with the router middleware
	return queryrangebase.MergeMiddlewares(middleware...).Wrap(preferredRT)
}
