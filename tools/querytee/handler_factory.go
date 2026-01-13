package querytee

import (
	"net/http"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"

	"github.com/grafana/dskit/middleware"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
	"github.com/grafana/loki/v3/pkg/util/server"
	"github.com/grafana/loki/v3/tools/querytee/comparator"
	"github.com/grafana/loki/v3/tools/querytee/goldfish"
)

// HandlerFactory creates the appropriate handler based on configuration.
type HandlerFactory struct {
	backends                  []*ProxyBackend
	codec                     queryrangebase.Codec
	goldfishManager           goldfish.Manager
	instrumentCompares        bool
	routingMode               RoutingMode
	logger                    log.Logger
	metrics                   *ProxyMetrics
	raceTolerance             time.Duration
	skipFanOutWhenNotSampling bool
	splitStart                time.Time
	splitLag                  time.Duration
}

// HandlerFactoryConfig holds configuration for creating a HandlerFactory.
type HandlerFactoryConfig struct {
	Backends                  []*ProxyBackend
	Codec                     queryrangebase.Codec
	GoldfishManager           goldfish.Manager
	InstrumentCompares        bool
	RoutingMode               RoutingMode
	Logger                    log.Logger
	Metrics                   *ProxyMetrics
	RaceTolerance             time.Duration
	SkipFanOutWhenNotSampling bool
	SplitStart                flagext.Time
	SplitLag                  time.Duration
}

// NewHandlerFactory creates a new HandlerFactory.
func NewHandlerFactory(cfg HandlerFactoryConfig) *HandlerFactory {
	return &HandlerFactory{
		backends:                  cfg.Backends,
		codec:                     cfg.Codec,
		goldfishManager:           cfg.GoldfishManager,
		instrumentCompares:        cfg.InstrumentCompares,
		routingMode:               cfg.RoutingMode,
		logger:                    cfg.Logger,
		metrics:                   cfg.Metrics,
		raceTolerance:             cfg.RaceTolerance,
		skipFanOutWhenNotSampling: cfg.SkipFanOutWhenNotSampling,
		splitStart:                time.Time(cfg.SplitStart),
		splitLag:                  cfg.SplitLag,
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
		RoutingMode:        f.routingMode,
		Logger:             f.logger,
		Metrics:            f.metrics,
		RouteName:          routeName,
		RaceTolerance:      f.raceTolerance,
	})

	var v1Backend *ProxyBackend
	for _, b := range f.backends {
		if b.v1Preferred {
			v1Backend = b
			break
		}
	}

	splittingHandler, err := NewSplittingHandler(SplittingHandlerConfig{
		Codec:                     f.codec,
		FanOutHandler:             fanOutHandler,
		GoldfishManager:           f.goldfishManager,
		V1Backend:                 v1Backend,
		SkipFanoutWhenNotSampling: f.skipFanOutWhenNotSampling,
		RoutingMode:               f.routingMode,
		SplitStart:                f.splitStart,
		SplitLag:                  f.splitLag,
	}, f.logger)

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
