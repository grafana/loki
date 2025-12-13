package querytee

import (
	"net/http"

	"github.com/go-kit/log"

	"github.com/grafana/dskit/middleware"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
	"github.com/grafana/loki/v3/pkg/util/server"
	"github.com/grafana/loki/v3/tools/querytee/comparator"
	"github.com/grafana/loki/v3/tools/querytee/goldfish"
)

// HandlerFactory creates the appropriate handler based on configuration.
type HandlerFactory struct {
	backends           []*ProxyBackend
	codec              queryrangebase.Codec
	goldfishManager    goldfish.Manager
	instrumentCompares bool
	enableRace         bool
	logger             log.Logger
	metrics            *ProxyMetrics
}

// HandlerFactoryConfig holds configuration for creating a HandlerFactory.
type HandlerFactoryConfig struct {
	Backends           []*ProxyBackend
	Codec              queryrangebase.Codec
	GoldfishManager    goldfish.Manager
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

func (f *HandlerFactory) CreateHandler(routeName string, comp comparator.ResponsesComparator) (http.Handler, error) {
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

	var preferredBackend *ProxyBackend
	for _, b := range f.backends {
		if b.preferred {
			preferredBackend = b
			break
		}
	}

	splittingHandler, err := NewSplittingHandler(
		f.codec,
		fanOutHandler,
		f.goldfishManager,
		f.logger,
		preferredBackend,
	)

	if err != nil {
		return nil, err
	}

	httpMiddlewares := []middleware.Interface{
		httpreq.ExtractQueryTagsMiddleware(),
		httpreq.PropagateHeadersMiddleware(httpreq.LokiActorPathHeader, httpreq.LokiEncodingFlagsHeader, httpreq.LokiDisablePipelineWrappersHeader),
		server.NewPrepopulateMiddleware(),
	}

	return middleware.Merge(httpMiddlewares...).Wrap(splittingHandler), nil
}
