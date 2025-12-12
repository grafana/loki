package querytee

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/tenant"
	"github.com/grafana/dskit/user"
	"github.com/grafana/loki/v3/pkg/engine"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/lokifrontend/frontend"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/util/server"
	"github.com/grafana/loki/v3/tools/querytee/goldfish"
	"github.com/pkg/errors"
)

type SplittingHandler struct {
	codec               queryrangebase.Codec
	fanOutHandler       queryrangebase.Handler
	goldfishManager     goldfish.Manager
	logger              log.Logger
	logsQueryHandler    queryrangebase.Handler
	metricsQueryHandler queryrangebase.Handler
	defaultHandler      queryrangebase.Handler
}

func NewSplittingHandler(
	codec queryrangebase.Codec,
	fanOutHandler queryrangebase.Handler,
	goldfishManager goldfish.Manager,
	logger log.Logger,
	preferredBackend *ProxyBackend,
) (http.Handler, error) {
	if preferredBackend == nil {
		return tenantHandler(queryrange.NewSerializeHTTPHandler(fanOutHandler, codec), logger), nil
	}

	splitHandlerFactory := &splitHandlerFactory{
		codec:            codec,
		fanOutHandler:    fanOutHandler,
		goldfishManager:  goldfishManager,
		logger:           logger,
		preferredBackend: preferredBackend,
	}
	preferredRT, err := frontend.NewDownstreamRoundTripper(
		preferredBackend.endpoint.String(),
		&http.Transport{
			MaxIdleConnsPerHost: 100,
			IdleConnTimeout:     90 * time.Second,
		},
		codec,
	)
	if err != nil {
		return nil, err
	}
	metricsQueryHandler := splitHandlerFactory.createSplittingHandler(true, preferredRT)
	logsQueryHandler := splitHandlerFactory.createSplittingHandler(false, preferredRT)

	splittingHandler := &SplittingHandler{
		codec:               codec,
		fanOutHandler:       fanOutHandler,
		goldfishManager:     goldfishManager,
		logger:              logger,
		logsQueryHandler:    logsQueryHandler,
		metricsQueryHandler: metricsQueryHandler,
		defaultHandler:      preferredRT,
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
	codec            queryrangebase.Codec
	fanOutHandler    queryrangebase.Handler
	goldfishManager  goldfish.Manager
	logger           log.Logger
	preferredBackend *ProxyBackend
}

func (f *splitHandlerFactory) createSplittingHandler(forMetricQuery bool, defaultHandler queryrangebase.Handler) queryrangebase.Handler {
	if f.preferredBackend == nil {
		// No preferred backend, can't do splitting
		return f.fanOutHandler
	}

	routerConfig := queryrange.RouterConfig{
		Enabled:  true,
		Validate: engine.IsQuerySupported,
		Handler:  f.fanOutHandler, // v2Next: fan-out to all backends for goldfish
	}

	if f.goldfishManager != nil {
		routerConfig.Start = f.goldfishManager.ComparisonStartDate()
		routerConfig.Lag = f.goldfishManager.ComparisonMinAge()
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
	return queryrangebase.MergeMiddlewares(middleware...).Wrap(defaultHandler)
}

// ServeHTTP implements http.Handler interface to serve queries that can be split.
// If ComparisonMinAge is 0 (legacy mode), serve the non-splitting fan-out handler directly.
// If ComparisonMinAge > 0 (splitting mode), it wraps the fan-out handler with engineRouter
// middleware to serve split queries based on data age.
func (f *SplittingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_, ctx, err := tenant.ExtractTenantIDFromHTTPRequest(r)
	if err != nil {
		level.Warn(f.logger).Log(
			"msg", "failed to extract tenant ID",
			"err", err,
			"req", r.URL.String(),
		)
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	// The codec decode/encode cycle loses custom headers, so we preserve them for downstream
	headersCopy := r.Header.Clone()
	ctx = context.WithValue(ctx, originalHTTPHeadersKey, headersCopy)

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
	if f.goldfishManager != nil && f.goldfishManager.ComparisonMinAge() > 0 {
		resp, err = f.serveSplits(ctx, req)
	} else {
		resp, err = f.fanOutHandler.Do(ctx, req)
	}

	if err != nil {
		switch r := resp.(type) {
		case *NonDecodableResponse:
			http.Error(w, string(r.Body), r.StatusCode)
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
			return f.metricsQueryHandler.Do(ctx, req)
		default:
			return f.logsQueryHandler.Do(ctx, req)
		}
	default:
		return f.defaultHandler.Do(ctx, req)
	}
}
