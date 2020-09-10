package api

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/opentracing-contrib/go-stdlib/nethttp"
	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/gorilla/mux"
	"github.com/prometheus/common/route"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/storage"
	v1 "github.com/prometheus/prometheus/web/api/v1"
	"github.com/weaveworks/common/middleware"
	"github.com/weaveworks/common/server"

	"github.com/cortexproject/cortex/pkg/alertmanager"
	"github.com/cortexproject/cortex/pkg/chunk/purger"
	"github.com/cortexproject/cortex/pkg/compactor"
	"github.com/cortexproject/cortex/pkg/distributor"
	"github.com/cortexproject/cortex/pkg/ingester"
	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/querier"
	"github.com/cortexproject/cortex/pkg/querier/frontend"
	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/ruler"
	"github.com/cortexproject/cortex/pkg/storegateway"
	"github.com/cortexproject/cortex/pkg/storegateway/storegatewaypb"
	"github.com/cortexproject/cortex/pkg/util/push"
)

type Config struct {
	AlertmanagerHTTPPrefix string `yaml:"alertmanager_http_prefix"`
	PrometheusHTTPPrefix   string `yaml:"prometheus_http_prefix"`

	// The following configs are injected by the upstream caller.
	ServerPrefix       string               `yaml:"-"`
	LegacyHTTPPrefix   string               `yaml:"-"`
	HTTPAuthMiddleware middleware.Interface `yaml:"-"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", f)
}

// RegisterFlagsWithPrefix adds the flags required to config this to the given FlagSet with the set prefix.
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&cfg.AlertmanagerHTTPPrefix, prefix+"http.alertmanager-http-prefix", "/alertmanager", "HTTP URL path under which the Alertmanager ui and api will be served.")
	f.StringVar(&cfg.PrometheusHTTPPrefix, prefix+"http.prometheus-http-prefix", "/prometheus", "HTTP URL path under which the Prometheus api will be served.")
}

type API struct {
	cfg            Config
	authMiddleware middleware.Interface
	server         *server.Server
	logger         log.Logger
	sourceIPs      *middleware.SourceIPExtractor
}

func New(cfg Config, serverCfg server.Config, s *server.Server, logger log.Logger) (*API, error) {
	// Ensure the encoded path is used. Required for the rules API
	s.HTTP.UseEncodedPath()

	var sourceIPs *middleware.SourceIPExtractor
	if serverCfg.LogSourceIPs {
		var err error
		sourceIPs, err = middleware.NewSourceIPs(serverCfg.LogSourceIPsHeader, serverCfg.LogSourceIPsRegex)
		if err != nil {
			// This should have already been caught in the Server creation
			return nil, err
		}
	}

	api := &API{
		cfg:            cfg,
		authMiddleware: cfg.HTTPAuthMiddleware,
		server:         s,
		logger:         logger,
		sourceIPs:      sourceIPs,
	}

	// If no authentication middleware is present in the config, use the default authentication middleware.
	if cfg.HTTPAuthMiddleware == nil {
		api.authMiddleware = middleware.AuthenticateUser
	}

	return api, nil
}

func (a *API) RegisterRoute(path string, handler http.Handler, auth bool, methods ...string) {
	a.registerRouteWithRouter(a.server.HTTP, path, handler, auth, methods...)
}

func (a *API) registerRouteWithRouter(router *mux.Router, path string, handler http.Handler, auth bool, methods ...string) {
	level.Debug(a.logger).Log("msg", "api: registering route", "methods", strings.Join(methods, ","), "path", path, "auth", auth)
	if auth {
		handler = a.authMiddleware.Wrap(handler)
	}
	if len(methods) == 0 {
		router.Path(path).Handler(handler)
		return
	}
	router.Path(path).Methods(methods...).Handler(handler)
}

func (a *API) RegisterRoutesWithPrefix(prefix string, handler http.Handler, auth bool, methods ...string) {
	level.Debug(a.logger).Log("msg", "api: registering route", "methods", strings.Join(methods, ","), "prefix", prefix, "auth", auth)
	if auth {
		handler = a.authMiddleware.Wrap(handler)
	}
	if len(methods) == 0 {
		a.server.HTTP.PathPrefix(prefix).Handler(handler)
		return
	}
	a.server.HTTP.PathPrefix(prefix).Methods(methods...).Handler(handler)
}

// Latest Prometheus requires r.RemoteAddr to be set to addr:port, otherwise it reject the request.
// Requests to Querier sometimes doesn't have that (if they are fetched from Query-Frontend).
// Prometheus uses this when logging queries to QueryLogger, but Cortex doesn't call engine.SetQueryLogger to set one.
//
// Can be removed when (if) https://github.com/prometheus/prometheus/pull/6840 is merged.
func fakeRemoteAddr(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.RemoteAddr == "" {
			r.RemoteAddr = "127.0.0.1:8888"
		}
		handler.ServeHTTP(w, r)
	})
}

// RegisterAlertmanager registers endpoints associated with the alertmanager. It will only
// serve endpoints using the legacy http-prefix if it is not run as a single binary.
func (a *API) RegisterAlertmanager(am *alertmanager.MultitenantAlertmanager, target, apiEnabled bool) {
	// Ensure this route is registered before the prefixed AM route
	a.RegisterRoute("/multitenant_alertmanager/status", am.GetStatusHandler(), false)

	// UI components lead to a large number of routes to support, utilize a path prefix instead
	a.RegisterRoutesWithPrefix(a.cfg.AlertmanagerHTTPPrefix, am, true)
	level.Debug(a.logger).Log("msg", "api: registering alertmanager", "path_prefix", a.cfg.AlertmanagerHTTPPrefix)

	// If the target is Alertmanager, enable the legacy behaviour. Otherwise only enable
	// the component routed API.
	if target {
		a.RegisterRoute("/status", am.GetStatusHandler(), false)
		a.RegisterRoutesWithPrefix(a.cfg.LegacyHTTPPrefix, am, true)
	}

	// MultiTenant Alertmanager Experimental API routes
	if apiEnabled {
		a.RegisterRoute("/api/v1/alerts", http.HandlerFunc(am.GetUserConfig), true, "GET")
		a.RegisterRoute("/api/v1/alerts", http.HandlerFunc(am.SetUserConfig), true, "POST")
		a.RegisterRoute("/api/v1/alerts", http.HandlerFunc(am.DeleteUserConfig), true, "DELETE")
	}
}

// RegisterAPI registers the standard endpoints associated with a running Cortex.
func (a *API) RegisterAPI(cfg interface{}) {
	a.RegisterRoute("/config", configHandler(cfg), false)
	a.RegisterRoute("/", http.HandlerFunc(indexHandler), false)
}

// RegisterDistributor registers the endpoints associated with the distributor.
func (a *API) RegisterDistributor(d *distributor.Distributor, pushConfig distributor.Config) {
	a.RegisterRoute("/api/v1/push", push.Handler(pushConfig, a.sourceIPs, d.Push), true)
	a.RegisterRoute("/distributor/all_user_stats", http.HandlerFunc(d.AllUserStatsHandler), false)
	a.RegisterRoute("/distributor/ha_tracker", d.HATracker, false)

	// Legacy Routes
	a.RegisterRoute(a.cfg.LegacyHTTPPrefix+"/push", push.Handler(pushConfig, a.sourceIPs, d.Push), true)
	a.RegisterRoute("/all_user_stats", http.HandlerFunc(d.AllUserStatsHandler), false)
	a.RegisterRoute("/ha-tracker", d.HATracker, false)
}

// RegisterIngester registers the ingesters HTTP and GRPC service
func (a *API) RegisterIngester(i *ingester.Ingester, pushConfig distributor.Config) {
	client.RegisterIngesterServer(a.server.GRPC, i)

	a.RegisterRoute("/ingester/flush", http.HandlerFunc(i.FlushHandler), false)
	a.RegisterRoute("/ingester/shutdown", http.HandlerFunc(i.ShutdownHandler), false)
	a.RegisterRoute("/ingester/push", push.Handler(pushConfig, a.sourceIPs, i.Push), true) // For testing and debugging.

	// Legacy Routes
	a.RegisterRoute("/flush", http.HandlerFunc(i.FlushHandler), false)
	a.RegisterRoute("/shutdown", http.HandlerFunc(i.ShutdownHandler), false)
	a.RegisterRoute("/push", push.Handler(pushConfig, a.sourceIPs, i.Push), true) // For testing and debugging.
}

// RegisterPurger registers the endpoints associated with the Purger/DeleteStore. They do not exactly
// match the Prometheus API but mirror it closely enough to justify their routing under the Prometheus
// component/
func (a *API) RegisterPurger(store *purger.DeleteStore, deleteRequestCancelPeriod time.Duration) {
	deleteRequestHandler := purger.NewDeleteRequestHandler(store, deleteRequestCancelPeriod, prometheus.DefaultRegisterer)

	a.RegisterRoute(a.cfg.PrometheusHTTPPrefix+"/api/v1/admin/tsdb/delete_series", http.HandlerFunc(deleteRequestHandler.AddDeleteRequestHandler), true, "PUT", "POST")
	a.RegisterRoute(a.cfg.PrometheusHTTPPrefix+"/api/v1/admin/tsdb/delete_series", http.HandlerFunc(deleteRequestHandler.GetAllDeleteRequestsHandler), true, "GET")
	a.RegisterRoute(a.cfg.PrometheusHTTPPrefix+"/api/v1/admin/tsdb/cancel_delete_request", http.HandlerFunc(deleteRequestHandler.CancelDeleteRequestHandler), true, "PUT", "POST")

	// Legacy Routes
	a.RegisterRoute(a.cfg.LegacyHTTPPrefix+"/api/v1/admin/tsdb/delete_series", http.HandlerFunc(deleteRequestHandler.AddDeleteRequestHandler), true, "PUT", "POST")
	a.RegisterRoute(a.cfg.LegacyHTTPPrefix+"/api/v1/admin/tsdb/delete_series", http.HandlerFunc(deleteRequestHandler.GetAllDeleteRequestsHandler), true, "GET")
	a.RegisterRoute(a.cfg.LegacyHTTPPrefix+"/api/v1/admin/tsdb/cancel_delete_request", http.HandlerFunc(deleteRequestHandler.CancelDeleteRequestHandler), true, "PUT", "POST")
}

// RegisterRuler registers routes associated with the Ruler service. If the
// API is not enabled only the ring route is registered.
func (a *API) RegisterRuler(r *ruler.Ruler, apiEnabled bool) {
	a.RegisterRoute("/ruler/ring", r, false)

	// Legacy Ring Route
	a.RegisterRoute("/ruler_ring", r, false)

	if apiEnabled {
		// Prometheus Rule API Routes
		a.RegisterRoute(a.cfg.PrometheusHTTPPrefix+"/api/v1/rules", http.HandlerFunc(r.PrometheusRules), true, "GET")
		a.RegisterRoute(a.cfg.PrometheusHTTPPrefix+"/api/v1/alerts", http.HandlerFunc(r.PrometheusAlerts), true, "GET")

		ruler.RegisterRulerServer(a.server.GRPC, r)

		// Ruler API Routes
		a.RegisterRoute("/api/v1/rules", http.HandlerFunc(r.ListRules), true, "GET")
		a.RegisterRoute("/api/v1/rules/{namespace}", http.HandlerFunc(r.ListRules), true, "GET")
		a.RegisterRoute("/api/v1/rules/{namespace}/{groupName}", http.HandlerFunc(r.GetRuleGroup), true, "GET")
		a.RegisterRoute("/api/v1/rules/{namespace}", http.HandlerFunc(r.CreateRuleGroup), true, "POST")
		a.RegisterRoute("/api/v1/rules/{namespace}/{groupName}", http.HandlerFunc(r.DeleteRuleGroup), true, "DELETE")

		// Legacy Prometheus Rule API Routes
		a.RegisterRoute(a.cfg.LegacyHTTPPrefix+"/api/v1/rules", http.HandlerFunc(r.PrometheusRules), true, "GET")
		a.RegisterRoute(a.cfg.LegacyHTTPPrefix+"/api/v1/alerts", http.HandlerFunc(r.PrometheusAlerts), true, "GET")

		// Legacy Ruler API Routes
		a.RegisterRoute(a.cfg.LegacyHTTPPrefix+"/rules", http.HandlerFunc(r.ListRules), true, "GET")
		a.RegisterRoute(a.cfg.LegacyHTTPPrefix+"/rules/{namespace}", http.HandlerFunc(r.ListRules), true, "GET")
		a.RegisterRoute(a.cfg.LegacyHTTPPrefix+"/rules/{namespace}/{groupName}", http.HandlerFunc(r.GetRuleGroup), true, "GET")
		a.RegisterRoute(a.cfg.LegacyHTTPPrefix+"/rules/{namespace}", http.HandlerFunc(r.CreateRuleGroup), true, "POST")
		a.RegisterRoute(a.cfg.LegacyHTTPPrefix+"/rules/{namespace}/{groupName}", http.HandlerFunc(r.DeleteRuleGroup), true, "DELETE")
	}
}

// // RegisterRing registers the ring UI page associated with the distributor for writes.
func (a *API) RegisterRing(r *ring.Ring) {
	a.RegisterRoute("/ingester/ring", r, false)

	// Legacy Route
	a.RegisterRoute("/ring", r, false)
}

// RegisterStoreGateway registers the ring UI page associated with the store-gateway.
func (a *API) RegisterStoreGateway(s *storegateway.StoreGateway) {
	storegatewaypb.RegisterStoreGatewayServer(a.server.GRPC, s)

	a.RegisterRoute("/store-gateway/ring", http.HandlerFunc(s.RingHandler), false)
}

// RegisterCompactor registers the ring UI page associated with the compactor.
func (a *API) RegisterCompactor(c *compactor.Compactor) {
	a.RegisterRoute("/compactor/ring", http.HandlerFunc(c.RingHandler), false)
}

// RegisterQuerier registers the Prometheus routes supported by the
// Cortex querier service. Currently this can not be registered simultaneously
// with the QueryFrontend.
func (a *API) RegisterQuerier(
	queryable storage.SampleAndChunkQueryable,
	engine *promql.Engine,
	distributor *distributor.Distributor,
	registerRoutesExternally bool,
	tombstonesLoader *purger.TombstonesLoader,
	querierRequestDuration *prometheus.HistogramVec,
	receivedMessageSize *prometheus.HistogramVec,
	sentMessageSize *prometheus.HistogramVec,
	inflightRequests *prometheus.GaugeVec,
) http.Handler {
	api := v1.NewAPI(
		engine,
		errorTranslateQueryable{queryable}, // Translate errors to errors expected by API.
		func(context.Context) v1.TargetRetriever { return &querier.DummyTargetRetriever{} },
		func(context.Context) v1.AlertmanagerRetriever { return &querier.DummyAlertmanagerRetriever{} },
		func() config.Config { return config.Config{} },
		map[string]string{}, // TODO: include configuration flags
		v1.GlobalURLOptions{},
		func(f http.HandlerFunc) http.HandlerFunc { return f },
		nil,   // Only needed for admin APIs.
		"",    // This is for snapshots, which is disabled when admin APIs are disabled. Hence empty.
		false, // Disable admin APIs.
		a.logger,
		func(context.Context) v1.RulesRetriever { return &querier.DummyRulesRetriever{} },
		0, 0, 0, // Remote read samples and concurrency limit.
		regexp.MustCompile(".*"),
		func() (v1.RuntimeInfo, error) { return v1.RuntimeInfo{}, errors.New("not implemented") },
		&v1.PrometheusVersion{},
	)

	// these routes are always registered to the default server
	a.RegisterRoute("/api/v1/user_stats", http.HandlerFunc(distributor.UserStatsHandler), true)
	a.RegisterRoute("/api/v1/chunks", querier.ChunksHandler(queryable), true)

	a.RegisterRoute(a.cfg.LegacyHTTPPrefix+"/user_stats", http.HandlerFunc(distributor.UserStatsHandler), true)
	a.RegisterRoute(a.cfg.LegacyHTTPPrefix+"/chunks", querier.ChunksHandler(queryable), true)

	// these routes are either registered the default server OR to an internal mux.  The internal mux is
	// for use in a single binary mode when both the query frontend and the querier would attempt to claim these routes
	// TODO:  Add support to expose querier paths with a configurable prefix in single binary mode.
	router := mux.NewRouter()
	if registerRoutesExternally {
		router = a.server.HTTP
	}

	// Use a separate metric for the querier in order to differentiate requests from the query-frontend when
	// running Cortex as a single binary.
	inst := middleware.Instrument{
		RouteMatcher:     router,
		Duration:         querierRequestDuration,
		RequestBodySize:  receivedMessageSize,
		ResponseBodySize: sentMessageSize,
		InflightRequests: inflightRequests,
	}

	promRouter := route.New().WithPrefix(a.cfg.ServerPrefix + a.cfg.PrometheusHTTPPrefix + "/api/v1")
	api.Register(promRouter)
	cacheGenHeaderMiddleware := getHTTPCacheGenNumberHeaderSetterMiddleware(tombstonesLoader)
	promHandler := fakeRemoteAddr(inst.Wrap(cacheGenHeaderMiddleware.Wrap(promRouter)))

	a.registerRouteWithRouter(router, a.cfg.PrometheusHTTPPrefix+"/api/v1/read", querier.RemoteReadHandler(queryable), true, "POST")
	a.registerRouteWithRouter(router, a.cfg.PrometheusHTTPPrefix+"/api/v1/query", promHandler, true, "GET", "POST")
	a.registerRouteWithRouter(router, a.cfg.PrometheusHTTPPrefix+"/api/v1/query_range", promHandler, true, "GET", "POST")
	a.registerRouteWithRouter(router, a.cfg.PrometheusHTTPPrefix+"/api/v1/labels", promHandler, true, "GET", "POST")
	a.registerRouteWithRouter(router, a.cfg.PrometheusHTTPPrefix+"/api/v1/label/{name}/values", promHandler, true, "GET")
	a.registerRouteWithRouter(router, a.cfg.PrometheusHTTPPrefix+"/api/v1/series", promHandler, true, "GET", "POST", "DELETE")
	//TODO(gotjosh): This custom handler is temporary until we're able to vendor the changes in:
	// https://github.com/prometheus/prometheus/pull/7125/files
	a.registerRouteWithRouter(router, a.cfg.PrometheusHTTPPrefix+"/api/v1/metadata", querier.MetadataHandler(distributor), true, "GET")

	legacyPromRouter := route.New().WithPrefix(a.cfg.ServerPrefix + a.cfg.LegacyHTTPPrefix + "/api/v1")
	api.Register(legacyPromRouter)
	legacyPromHandler := fakeRemoteAddr(inst.Wrap(cacheGenHeaderMiddleware.Wrap(legacyPromRouter)))

	a.registerRouteWithRouter(router, a.cfg.LegacyHTTPPrefix+"/api/v1/read", querier.RemoteReadHandler(queryable), true, "POST")
	a.registerRouteWithRouter(router, a.cfg.LegacyHTTPPrefix+"/api/v1/query", legacyPromHandler, true, "GET", "POST")
	a.registerRouteWithRouter(router, a.cfg.LegacyHTTPPrefix+"/api/v1/query_range", legacyPromHandler, true, "GET", "POST")
	a.registerRouteWithRouter(router, a.cfg.LegacyHTTPPrefix+"/api/v1/labels", legacyPromHandler, true, "GET", "POST")
	a.registerRouteWithRouter(router, a.cfg.LegacyHTTPPrefix+"/api/v1/label/{name}/values", legacyPromHandler, true, "GET")
	a.registerRouteWithRouter(router, a.cfg.LegacyHTTPPrefix+"/api/v1/series", legacyPromHandler, true, "GET", "POST", "DELETE")
	//TODO(gotjosh): This custom handler is temporary until we're able to vendor the changes in:
	// https://github.com/prometheus/prometheus/pull/7125/files
	a.registerRouteWithRouter(router, a.cfg.LegacyHTTPPrefix+"/api/v1/metadata", querier.MetadataHandler(distributor), true, "GET")

	// if we have externally registered routes then we need to return the server handler
	// so that we continue to use all standard middleware
	if registerRoutesExternally {
		return a.server.HTTPServer.Handler
	}

	// Since we have a new router and the request will not go trough the default server
	// HTTP middleware stack, we need to add a middleware to extract the trace context
	// from the HTTP headers and inject it into the Go context.
	return nethttp.MiddlewareFunc(opentracing.GlobalTracer(), router.ServeHTTP, nethttp.OperationNameFunc(func(r *http.Request) string {
		return "internalQuerier"
	}))
}

// registerQueryAPI registers the Prometheus routes supported by the
// Cortex querier service. Currently this can not be registered simultaneously
// with the Querier.
func (a *API) registerQueryAPI(handler http.Handler) {
	a.RegisterRoute(a.cfg.PrometheusHTTPPrefix+"/api/v1/read", handler, true, "POST")
	a.RegisterRoute(a.cfg.PrometheusHTTPPrefix+"/api/v1/query", handler, true, "GET", "POST")
	a.RegisterRoute(a.cfg.PrometheusHTTPPrefix+"/api/v1/query_range", handler, true, "GET", "POST")
	a.RegisterRoute(a.cfg.PrometheusHTTPPrefix+"/api/v1/labels", handler, true, "GET", "POST")
	a.RegisterRoute(a.cfg.PrometheusHTTPPrefix+"/api/v1/label/{name}/values", handler, true, "GET")
	a.RegisterRoute(a.cfg.PrometheusHTTPPrefix+"/api/v1/series", handler, true, "GET", "POST", "DELETE")
	a.RegisterRoute(a.cfg.PrometheusHTTPPrefix+"/api/v1/metadata", handler, true, "GET")

	// Register Legacy Routers
	a.RegisterRoute(a.cfg.LegacyHTTPPrefix+"/api/v1/read", handler, true, "POST")
	a.RegisterRoute(a.cfg.LegacyHTTPPrefix+"/api/v1/query", handler, true, "GET", "POST")
	a.RegisterRoute(a.cfg.LegacyHTTPPrefix+"/api/v1/query_range", handler, true, "GET", "POST")
	a.RegisterRoute(a.cfg.LegacyHTTPPrefix+"/api/v1/labels", handler, true, "GET", "POST")
	a.RegisterRoute(a.cfg.LegacyHTTPPrefix+"/api/v1/label/{name}/values", handler, true, "GET")
	a.RegisterRoute(a.cfg.LegacyHTTPPrefix+"/api/v1/series", handler, true, "GET", "POST", "DELETE")
	a.RegisterRoute(a.cfg.LegacyHTTPPrefix+"/api/v1/metadata", handler, true, "GET")
}

// RegisterQueryFrontend registers the Prometheus routes supported by the
// Cortex querier service. Currently this can not be registered simultaneously
// with the Querier.
func (a *API) RegisterQueryFrontend(f *frontend.Frontend) {
	frontend.RegisterFrontendServer(a.server.GRPC, f)
	a.registerQueryAPI(f.Handler())
}

// RegisterServiceMapHandler registers the Cortex structs service handler
// TODO: Refactor this code to be accomplished using the services.ServiceManager
// or a future module manager #2291
func (a *API) RegisterServiceMapHandler(handler http.Handler) {
	a.RegisterRoute("/services", handler, false)
}
