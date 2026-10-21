package querytee

import (
	"net/http"

	"github.com/go-kit/log"

	"github.com/grafana/dskit/middleware"

	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/querytee/comparator"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
	"github.com/grafana/loki/v3/pkg/util/server"
)

// HandlerFactory creates the query handler of a route.
type HandlerFactory struct {
	backends                      []*ProxyBackend
	codec                         queryrangebase.Codec
	instrumentCompares            bool
	logger                        log.Logger
	metrics                       *ProxyMetrics
	addRoutingDecisionsToWarnings bool
}

// HandlerFactoryConfig holds configuration for creating a HandlerFactory.
type HandlerFactoryConfig struct {
	Backends                      []*ProxyBackend
	Codec                         queryrangebase.Codec
	InstrumentCompares            bool
	Logger                        log.Logger
	Metrics                       *ProxyMetrics
	AddRoutingDecisionsToWarnings bool
}

// NewHandlerFactory creates a new HandlerFactory.
func NewHandlerFactory(cfg HandlerFactoryConfig) *HandlerFactory {
	return &HandlerFactory{
		backends:                      cfg.Backends,
		codec:                         cfg.Codec,
		instrumentCompares:            cfg.InstrumentCompares,
		logger:                        cfg.Logger,
		metrics:                       cfg.Metrics,
		addRoutingDecisionsToWarnings: cfg.AddRoutingDecisionsToWarnings,
	}
}

// CreateHandler returns the query handler of a route. It sends each request to every backend,
// except the backends whose filter does not match the request URL.
func (f *HandlerFactory) CreateHandler(routeName string, comp comparator.ResponsesComparator) http.Handler {
	fanOutHandler := NewFanOutHandler(FanOutHandlerConfig{
		Backends:                      f.backends,
		Codec:                         f.codec,
		Comparator:                    comp,
		InstrumentCompares:            f.instrumentCompares,
		Logger:                        f.logger,
		Metrics:                       f.metrics,
		RouteName:                     routeName,
		AddRoutingDecisionsToWarnings: f.addRoutingDecisionsToWarnings,
	})

	httpMiddlewares := []middleware.Interface{
		// Propagate all headers except those that affect response encoding or security.
		httpreq.PropagateAllHeadersMiddleware(httpreq.AuthorizationHeader, httpreq.AcceptHeader, httpreq.AcceptEncodingHeader),
		httpreq.ExtractQueryTagsMiddleware(),
		server.NewPrepopulateMiddleware(),
	}

	return middleware.Merge(httpMiddlewares...).Wrap(NewQueryHandler(f.codec, fanOutHandler, f.logger))
}
