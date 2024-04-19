package querier

import (
	"net"
	"strconv"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	querier_worker "github.com/grafana/loki/v3/pkg/querier/worker"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

type WorkerServiceConfig struct {
	AllEnabled            bool
	ReadEnabled           bool
	GrpcListenAddress     string
	GrpcListenPort        int
	QuerierMaxConcurrent  int
	QuerierWorkerConfig   *querier_worker.Config
	QueryFrontendEnabled  bool
	QuerySchedulerEnabled bool
	SchedulerRing         ring.ReadRing
}

func (cfg WorkerServiceConfig) QuerierRunningStandalone() bool {
	runningStandalone := !cfg.QueryFrontendEnabled && !cfg.QuerySchedulerEnabled && !cfg.ReadEnabled && !cfg.AllEnabled
	level.Debug(util_log.Logger).Log(
		"msg", "determining if querier is running as standalone target",
		"runningStandalone", runningStandalone,
		"queryFrontendEnabled", cfg.QueryFrontendEnabled,
		"queryScheduleEnabled", cfg.QuerySchedulerEnabled,
		"readEnabled", cfg.ReadEnabled,
		"allEnabled", cfg.AllEnabled,
	)

	return runningStandalone
}

// InitWorkerService takes a config object, a map of routes to handlers, an external http router and external
// http handler, and an auth middleware wrapper. This function creates an internal HTTP router that responds to all
// the provided query routes/handlers. This router can either be registered with the external Loki HTTP server, or
// be used internally by a querier worker so that it does not conflict with the routes registered by the Query Frontend module.
//
//  1. Query-Frontend Enabled: If Loki has an All or QueryFrontend target, the internal
//     HTTP router is wrapped with Tenant ID parsing middleware and passed to the frontend
//     worker.
//
//  2. Querier Standalone: The querier will register the internal HTTP router with the external
//     HTTP router for the Prometheus API routes. Then the external HTTP server will be passed
//     as a http.Handler to the frontend worker.
func InitWorkerService(
	logger log.Logger,
	cfg WorkerServiceConfig,
	reg prometheus.Registerer,
	handler queryrangebase.Handler,
	codec querier_worker.RequestCodec,
) (serve services.Service, err error) {

	// If the querier is running standalone without the query-frontend or query-scheduler, we must register the internal
	// HTTP handler externally (as it's the only handler that needs to register on querier routes) and provide the
	// external Loki Server HTTP handler to the frontend worker to ensure requests it processes use the default
	// middleware instrumentation.
	if cfg.QuerierRunningStandalone() {

		//If no scheduler ring or frontend or scheduler address has been configured, then there is no place for the
		//querier worker to request work from, so no need to start a worker service
		if cfg.SchedulerRing == nil && (*cfg.QuerierWorkerConfig).FrontendAddress == "" && (*cfg.QuerierWorkerConfig).SchedulerAddress == "" {
			return nil, nil
		}

		// If a frontend or scheduler address has been configured, return a querier worker service that uses
		// the external Loki Server HTTP server, which has now has the internal handler's routes registered with it
		return querier_worker.NewQuerierWorker(
			*(cfg.QuerierWorkerConfig),
			cfg.SchedulerRing,
			handler,
			logger,
			reg,
			codec,
		)
	}

	// Since we must be running a querier with either a frontend and/or scheduler at this point, if no scheduler ring, frontend, or scheduler address
	// is configured, Loki will default to using the frontend on localhost on it's own GRPC listening port.
	if cfg.SchedulerRing == nil && (*cfg.QuerierWorkerConfig).FrontendAddress == "" && (*cfg.QuerierWorkerConfig).SchedulerAddress == "" {
		listenAddress := "127.0.0.1"
		if cfg.GrpcListenAddress != "" {
			listenAddress = cfg.GrpcListenAddress
		}
		address := net.JoinHostPort(listenAddress, strconv.Itoa(cfg.GrpcListenPort))
		level.Warn(util_log.Logger).Log(
			"msg", "Worker address is empty, attempting automatic worker configuration. If queries are unresponsive consider configuring the worker explicitly.",
			"address", address)
		cfg.QuerierWorkerConfig.FrontendAddress = address
	}

	//Return a querier worker pointed to the internal querier HTTP handler so there is not a conflict in routes between the querier
	//and the query frontend
	return querier_worker.NewQuerierWorker(
		*(cfg.QuerierWorkerConfig),
		cfg.SchedulerRing,
		handler,
		logger,
		reg,
		codec,
	)
}
