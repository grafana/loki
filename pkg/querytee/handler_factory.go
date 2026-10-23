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
	backends                 []*ProxyBackend
	codec                    queryrangebase.Codec
	reportMissingSeries      bool
	logger                   log.Logger
	metrics                  *ProxyMetrics
	secondaryBackendStrategy SelectionStrategy
}

// HandlerFactoryConfig holds configuration for creating a HandlerFactory.
type HandlerFactoryConfig struct {
	Backends            []*ProxyBackend
	Codec               queryrangebase.Codec
	ReportMissingSeries bool
	Logger              log.Logger
	Metrics             *ProxyMetrics

	// SecondaryBackendStrategy picks the requests that the secondary backend receives. Without a
	// strategy, the secondary backend receives every request.
	SecondaryBackendStrategy SelectionStrategy
}

// NewHandlerFactory creates a new HandlerFactory.
func NewHandlerFactory(cfg HandlerFactoryConfig) *HandlerFactory {
	return &HandlerFactory{
		backends:                 cfg.Backends,
		codec:                    cfg.Codec,
		reportMissingSeries:      cfg.ReportMissingSeries,
		logger:                   cfg.Logger,
		metrics:                  cfg.Metrics,
		secondaryBackendStrategy: cfg.SecondaryBackendStrategy,
	}
}

// CreateHandler returns the query handler of a route. It sends each request to every backend, except
// that the strategy decides whether the secondary backend receives it. Without a secondary backend
// or a strategy, it sends every request to every backend.
func (f *HandlerFactory) CreateHandler(routeName string, comp comparator.ResponsesComparator) http.Handler {
	handler := NewFanOutHandler(FanOutHandlerConfig{
		Backends:                 f.backends,
		Codec:                    f.codec,
		Comparator:               comp,
		ReportMissingSeries:      f.reportMissingSeries,
		Logger:                   f.logger,
		Metrics:                  f.metrics,
		RouteName:                routeName,
		SecondaryBackendStrategy: f.secondaryBackendStrategy,
	})

	httpMiddlewares := []middleware.Interface{
		// Propagate all headers except those that affect response encoding or security.
		httpreq.PropagateAllHeadersMiddleware(httpreq.AuthorizationHeader, httpreq.AcceptHeader, httpreq.AcceptEncodingHeader),
		httpreq.ExtractQueryTagsMiddleware(),
		server.NewPrepopulateMiddleware(),
	}

	return middleware.Merge(httpMiddlewares...).Wrap(NewQueryHandler(f.codec, handler, f.logger))
}
