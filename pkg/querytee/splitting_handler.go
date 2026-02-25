package querytee

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/tenant"
	"github.com/grafana/dskit/user"
	"github.com/pkg/errors"

	"github.com/grafana/loki/v3/pkg/engine"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/lokifrontend/frontend"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/querytee/goldfish"
	"github.com/grafana/loki/v3/pkg/util/server"
)

// SplittingHandlerConfig holds configuration for creating a SplittingHandler.
type SplittingHandlerConfig struct {
	Codec                         queryrangebase.Codec
	FanOutHandler                 queryrangebase.Handler
	GoldfishManager               goldfish.Manager
	V1Backend                     *ProxyBackend
	SkipFanoutWhenNotSampling     bool
	RoutingMode                   RoutingMode
	SplitStart                    time.Time
	SplitLag                      time.Duration
	SplitRetentionDays            int64
	AddRoutingDecisionsToWarnings bool
}

type SplittingHandler struct {
	codec                         queryrangebase.Codec
	fanOutHandler                 queryrangebase.Handler
	goldfishManager               goldfish.Manager
	skipFanoutWhenNotSampling     bool
	routingMode                   RoutingMode
	splitLag                      time.Duration
	logger                        log.Logger
	logsQueryHandler              queryrangebase.Handler
	metricsQueryHandler           queryrangebase.Handler
	v1Handler                     queryrangebase.Handler
	addRoutingDecisionsToWarnings bool
}

// isMultiTenant returns true if the request contains multiple tenant IDs.
func isMultiTenant(tenants []string) bool {
	return len(tenants) > 1
}

func NewSplittingHandler(cfg SplittingHandlerConfig, logger log.Logger) (http.Handler, error) {
	if cfg.V1Backend == nil {
		return tenantHandler(queryrange.NewSerializeHTTPHandler(cfg.FanOutHandler, cfg.Codec), logger), nil
	}

	splitHandlerFactory := &splitHandlerFactory{
		codec:              cfg.Codec,
		fanOutHandler:      cfg.FanOutHandler,
		logger:             logger,
		splitStart:         cfg.SplitStart,
		splitLag:           cfg.SplitLag,
		splitRetentionDays: cfg.SplitRetentionDays,
	}
	v1RoundTrip, err := frontend.NewDownstreamRoundTripper(
		cfg.V1Backend.endpoint.String(),
		&http.Transport{
			MaxIdleConnsPerHost: 100,
			IdleConnTimeout:     90 * time.Second,
		},
		cfg.Codec,
	)
	if err != nil {
		return nil, err
	}
	metricsQueryHandler := splitHandlerFactory.createSplittingHandler(true, v1RoundTrip)
	logsQueryHandler := splitHandlerFactory.createSplittingHandler(false, v1RoundTrip)

	splittingHandler := &SplittingHandler{
		codec:                         cfg.Codec,
		fanOutHandler:                 cfg.FanOutHandler,
		goldfishManager:               cfg.GoldfishManager,
		logger:                        logger,
		logsQueryHandler:              logsQueryHandler,
		metricsQueryHandler:           metricsQueryHandler,
		v1Handler:                     v1RoundTrip,
		skipFanoutWhenNotSampling:     cfg.SkipFanoutWhenNotSampling,
		routingMode:                   cfg.RoutingMode,
		splitLag:                      cfg.SplitLag,
		addRoutingDecisionsToWarnings: cfg.AddRoutingDecisionsToWarnings,
	}

	return tenantHandler(splittingHandler, logger), nil
}

func tenantHandler(next http.Handler, logger log.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, ctx, err := user.ExtractOrgIDFromHTTPRequest(r)
		if err != nil {
			level.Warn(logger).Log(
				"msg", "failed to extract tenant ID",
				"err", err,
				"req", r.URL.String(),
			)
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type splitHandlerFactory struct {
	codec              queryrangebase.Codec
	fanOutHandler      queryrangebase.Handler
	logger             log.Logger
	splitStart         time.Time
	splitLag           time.Duration
	splitRetentionDays int64
}

func (f *splitHandlerFactory) createSplittingHandler(forMetricQuery bool, v1Handler queryrangebase.Handler) queryrangebase.Handler {
	v2Cfg := engine.Config{
		StorageLag:           f.splitLag,
		StorageStartDate:     flagext.Time(f.splitStart),
		StorageRetentionDays: f.splitRetentionDays,
	}

	routerConfig := queryrange.RouterConfig{
		Enabled:  true,
		Validate: engine.IsQuerySupported,
		Handler:  f.fanOutHandler, // v2Next: fan-out to all backends for comparison
		V2Range:  v2Cfg.ValidQueryRange,
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

	// Wrap the default backend handler (v1Next) with the router middleware
	return queryrangebase.MergeMiddlewares(middleware...).Wrap(v1Handler)
}

// ServeHTTP implements http.Handler interface to serve queries that can be split.
//
// Routing behavior depends on the routing mode:
//   - v2-preferred/race: Always split when splitLag > 0 (sampling only affects goldfish comparison).
//   - v1-preferred: Skip fanout when not sampling, only split for goldfish comparison.
func (f *SplittingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_, ctx, err := user.ExtractOrgIDFromHTTPRequest(r)
	if err != nil {
		level.Warn(f.logger).Log(
			"msg", "failed to extract tenant ID",
			"err", err,
			"req", r.URL.String(),
		)
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	req, err := f.codec.DecodeRequest(ctx, r, nil)
	if err != nil {
		query := r.Form.Get("query")
		level.Warn(f.logger).Log(
			"msg", "failed to decode request",
			"query", query,
			"req", r.URL.String(),
			"err", err,
		)
		server.WriteError(err, w)
		return
	}

	var resp queryrangebase.Response
	tenants, err := tenant.TenantIDs(ctx)
	shouldSample := false
	if err != nil {
		level.Warn(f.logger).Log("msg", "failed to extract tenant IDs, will skip sampling evaluation", "err", err)
	} else {
		shouldSample = f.shouldSample(tenants, r)
	}

	// Multi-tenant queries must always go to v1 only (v2 doesn't support them)
	if isMultiTenant(tenants) {
		level.Info(f.logger).Log("msg", "multi-tenant query detected, routing to v1 only", "tenants", len(tenants))
		resp, err = f.v1Handler.Do(ctx, req)
		if resp != nil && f.addRoutingDecisionsToWarnings {
			addWarningToResponse(resp, "multi-tenant query routed to v1 backend only (v2 does not support multi-tenant)")
		}
		f.writeResponse(ctx, r, w, resp, err)
		return
	}

	// Routing decision logic:
	// - v2-preferred/race: Always split when splitLag > 0 (sampling only affects goldfish comparison)
	// - v1-preferred: Skip fanout when not sampling, only split for goldfish comparison
	useDefault := f.skipFanoutWhenNotSampling && !shouldSample && f.routingMode == RoutingModeV1Preferred
	splittingEnabled := f.splitLag > 0
	level.Debug(f.logger).Log("msg", "routing decision", "useDefault", useDefault, "splittingEnabled", splittingEnabled)

	if useDefault {
		// Not sampling and v1-preferred: go directly to v1 backend
		resp, err = f.v1Handler.Do(ctx, req)
		if resp != nil && f.addRoutingDecisionsToWarnings {
			addWarningToResponse(resp, "query was not split and was routed to v1 backend only")
		}
	} else if splittingEnabled {
		// Splitting is enabled: use engine router to split queries by time range between v1 and v2 backends
		resp, err = f.serveSplits(ctx, req)
	} else {
		// No splitting configured: fan out without splits to all backends
		resp, err = f.fanOutHandler.Do(ctx, req)
		if resp != nil && f.addRoutingDecisionsToWarnings {
			addWarningToResponse(resp, "query was not split, but still routed to all backends")
		}
	}

	f.writeResponse(ctx, r, w, resp, err)
}

// writeResponse handles encoding and writing the response back to the HTTP response writer.
func (f *SplittingHandler) writeResponse(ctx context.Context, r *http.Request, w http.ResponseWriter, resp queryrangebase.Response, err error) {
	if err != nil {
		switch typedResp := resp.(type) {
		case *NonDecodableResponse:
			http.Error(w, string(typedResp.Body), typedResp.StatusCode)
		default:
			level.Warn(f.logger).Log("msg", "handler failed", "err", err)
			server.WriteError(err, w)
		}
		return
	}

	// Encode the response back to HTTP
	httpResp, err := f.codec.EncodeResponse(ctx, r, resp)
	if err != nil {
		level.Warn(f.logger).Log("msg", "failed to encode response", "err", err)
		server.WriteError(err, w)
		return
	}
	defer httpResp.Body.Close()

	// Copy response headers
	for key, values := range httpResp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Write status code
	w.WriteHeader(httpResp.StatusCode)

	// Copy response body
	if _, err := io.Copy(w, httpResp.Body); err != nil {
		level.Warn(f.logger).Log("msg", "unable to write response body", "err", err)
	}
}

// shouldSample determines if a query should be sampled for goldfish comparison.
func (f *SplittingHandler) shouldSample(tenants []string, httpReq *http.Request) bool {
	if f.goldfishManager == nil {
		return false
	}

	for _, tenant := range tenants {
		if f.goldfishManager.ShouldSample(tenant) {
			level.Debug(f.logger).Log(
				"msg", "Goldfish sampling decision",
				"tenant", tenant,
				"sampled", true,
				"path", httpReq.URL.Path)
			return true
		}
	}

	return false
}

func (f *SplittingHandler) serveSplits(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
	switch op := req.(type) {
	case *queryrange.LokiRequest:
		if op.Plan == nil {
			err := errors.New("query plan is empty")
			query := req.GetQuery()
			level.Warn(f.logger).Log("msg", "query plan is empty", "query", query, "err", err)
			return nil, err
		}

		switch op.Plan.AST.(type) {
		case syntax.VariantsExpr, syntax.SampleExpr:
			level.Info(f.logger).Log("msg", "serving metric split", "query", req.GetQuery())
			resp, err := f.metricsQueryHandler.Do(ctx, req)
			if resp != nil && f.addRoutingDecisionsToWarnings {
				addWarningToResponse(resp, "metrics query was split between v1 and v2 backends")
			}
			return resp, err
		default:
			level.Info(f.logger).Log("msg", "serving logs split", "query", req.GetQuery())
			resp, err := f.logsQueryHandler.Do(ctx, req)
			if resp != nil && f.addRoutingDecisionsToWarnings {
				addWarningToResponse(resp, "logs query was split between v1 and v2 backends")
			}
			return resp, err
		}
	default:
		level.Info(f.logger).Log("msg", "not splitting unsupported request", "query", req.GetQuery())
		resp, err := f.v1Handler.Do(ctx, req)
		if resp != nil && f.addRoutingDecisionsToWarnings {
			addWarningToResponse(resp, "unsupported query was not split and sent to v1 backend only")
		}
		return resp, err
	}
}

// addWarningToResponse adds a warning message to the response based on its type.
func addWarningToResponse(resp queryrangebase.Response, warning string) {
	switch r := resp.(type) {
	case *queryrange.LokiResponse:
		r.Warnings = append(r.Warnings, warning)
	case *queryrange.LokiPromResponse:
		if r.Response != nil {
			r.Response.Warnings = append(r.Response.Warnings, warning)
		}
	}
}
