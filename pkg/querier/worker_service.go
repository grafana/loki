package querier

import (
	"fmt"
	"net/http"

	querier_worker "github.com/cortexproject/cortex/pkg/querier/worker"
	util_log "github.com/cortexproject/cortex/pkg/util/log"
	"github.com/go-kit/log/level"
	"github.com/gorilla/mux"
	"github.com/grafana/dskit/services"
	"github.com/opentracing-contrib/go-stdlib/nethttp"
	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus"
	httpgrpc_server "github.com/weaveworks/common/httpgrpc/server"
	"github.com/weaveworks/common/middleware"

	serverutil "github.com/grafana/loki/pkg/util/server"
)

type WorkerServiceConfig struct {
	AllEnabled            bool
	GrpcListenPort        int
	QuerierMaxConcurrent  int
	QuerierWorkerConfig   *querier_worker.Config
	QueryFrontendEnabled  bool
	QuerySchedulerEnabled bool
}

// InitWorkerService takes a config object, a map of routes to handlers, an external http router and external
// http handler, and an auth middleware wrapper. This function creates an internal HTTP router that responds to all
// the provided query routes/handlers. This router can either be registered with the external Loki HTTP server, or
// be used internally by a querier worker so that it does not conflict with the routes registered by the Query Frontend module.
//
// 1. Query-Frontend Enabled: If Loki has an All or QueryFrontend target, the internal
//    HTTP router is wrapped with Tenant ID parsing middleware and passed to the frontend
//    worker.
//
// 2. Querier Standalone: The querier will register the internal HTTP router with the external
//    HTTP router for the Prometheus API routes. Then the external HTTP server will be passed
//    as a http.Handler to the frontend worker.
//
func InitWorkerService(
	cfg WorkerServiceConfig,
	queryRoutesToHandlers map[string]http.Handler,
	externalRouter *mux.Router,
	externalHandler http.Handler,
	authMiddleware middleware.Interface,
) (serve services.Service, err error) {

	internalRouter := mux.NewRouter()
	for route, handler := range queryRoutesToHandlers {
		internalRouter.Handle(route, handler)
	}

	// If the querier is running standalone without the query-frontend or query-scheduler, we must register the internal
	// HTTP handler externally (as it's the only handler that needs to register on querier routes) and provide the
	// external Loki Server HTTP handler to the frontend worker to ensure requests it processes use the default
	// middleware instrumentation.
	if querierRunningStandalone(cfg) {

		// First, register the internal querier handler with the external HTTP server
		routes := make([]string, len(queryRoutesToHandlers))
		var idx = 0
		for route := range queryRoutesToHandlers {
			routes[idx] = route
			idx++
		}

		registerRoutesExternally(routes, externalRouter, internalRouter, authMiddleware)

		//If no frontend or scheduler address has been configured, then there is no place for the
		//querier worker to request work from, so no need to start a worker service
		if (*cfg.QuerierWorkerConfig).FrontendAddress == "" && (*cfg.QuerierWorkerConfig).SchedulerAddress == "" {
			return nil, nil
		}

		// If a frontend or scheduler address has been configured, return a querier worker service that uses
		// the external Loki Server HTTP server, which has now has the internal handler's routes registered with it
		return querier_worker.NewQuerierWorker(
			*(cfg.QuerierWorkerConfig), httpgrpc_server.NewServer(externalHandler), util_log.Logger, prometheus.DefaultRegisterer)
	}

	// Since we must be running a querier with either a frontend and/or scheduler at this point, if no frontend or scheduler address
	// is configured, Loki will default to using the frontend on localhost on it's own GRPC listening port.
	if (*cfg.QuerierWorkerConfig).FrontendAddress == "" && (*cfg.QuerierWorkerConfig).SchedulerAddress == "" {
		address := fmt.Sprintf("127.0.0.1:%d", cfg.GrpcListenPort)
		level.Warn(util_log.Logger).Log(
			"msg", "Worker address is empty, attempting automatic worker configuration.  If queries are unresponsive consider configuring the worker explicitly.",
			"address", address)
		cfg.QuerierWorkerConfig.FrontendAddress = address
	}

	// Add a middleware to extract the trace context and add a header.
	var internalHandler http.Handler
	internalHandler = nethttp.MiddlewareFunc(
		opentracing.GlobalTracer(),
		internalRouter.ServeHTTP,
		nethttp.OperationNameFunc(func(r *http.Request) string {
			return "internalQuerier"
		}))

	// If queries are processed using the external HTTP Server, we need wrap the internal querier with
	// HTTP router with middleware to parse the tenant ID from the HTTP header and inject it into the
	// request context, as well as make sure any x-www-url-formencoded params are correctly parsed
	httpMiddleware := middleware.Merge(
		authMiddleware,
		serverutil.NewPrepopulateMiddleware(),
	)

	internalHandler = httpMiddleware.Wrap(internalHandler)

	//Querier worker's max concurrent requests must be the same as the querier setting
	(*cfg.QuerierWorkerConfig).MaxConcurrentRequests = cfg.QuerierMaxConcurrent

	//Return a querier worker pointed to the internal querier HTTP handler so there is not a conflict in routes between the querier
	//and the query frontend
	return querier_worker.NewQuerierWorker(
		*(cfg.QuerierWorkerConfig), httpgrpc_server.NewServer(internalHandler), util_log.Logger, prometheus.DefaultRegisterer)
}

func registerRoutesExternally(routes []string, externalRouter *mux.Router, internalHandler http.Handler, authMiddleware middleware.Interface) {
	httpMiddleware := middleware.Merge(
		serverutil.RecoveryHTTPMiddleware,
		authMiddleware,
		serverutil.NewPrepopulateMiddleware(),
		serverutil.ResponseJSONMiddleware(),
	)

	for _, route := range routes {
		externalRouter.Handle(route, httpMiddleware.Wrap(internalHandler))
	}
}

func querierRunningStandalone(cfg WorkerServiceConfig) bool {
	runningStandalone := !cfg.QueryFrontendEnabled && !cfg.QuerySchedulerEnabled && !cfg.AllEnabled
	level.Debug(util_log.Logger).Log(
		"msg", "determing if querier is running as standalone target",
		"runningStandalone", runningStandalone,
		"queryFrontendEnabled", cfg.QueryFrontendEnabled,
		"queryScheduleEnabled", cfg.QuerySchedulerEnabled,
		"allEnabled", cfg.AllEnabled,
	)

	return runningStandalone
}
