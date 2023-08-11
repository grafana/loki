// SPDX-License-Identifier: AGPL-3.0-only
// Provenance-includes-location: https://github.com/cortexproject/cortex/blob/master/pkg/cortex/modules.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: The Cortex Authors.

package mimir

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/dns"
	"github.com/grafana/dskit/kv/codec"
	"github.com/grafana/dskit/kv/memberlist"
	"github.com/grafana/dskit/modules"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/runtimeconfig"
	"github.com/grafana/dskit/services"
	"github.com/opentracing-contrib/go-stdlib/nethttp"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/rules"
	prom_storage "github.com/prometheus/prometheus/storage"
	prom_remote "github.com/prometheus/prometheus/storage/remote"
	httpgrpc_server "github.com/weaveworks/common/httpgrpc/server"
	"github.com/weaveworks/common/server"

	"github.com/grafana/mimir/pkg/alertmanager"
	"github.com/grafana/mimir/pkg/alertmanager/alertstore"
	"github.com/grafana/mimir/pkg/api"
	"github.com/grafana/mimir/pkg/compactor"
	"github.com/grafana/mimir/pkg/distributor"
	"github.com/grafana/mimir/pkg/flusher"
	"github.com/grafana/mimir/pkg/frontend"
	"github.com/grafana/mimir/pkg/frontend/querymiddleware"
	"github.com/grafana/mimir/pkg/frontend/transport"
	"github.com/grafana/mimir/pkg/ingester"
	"github.com/grafana/mimir/pkg/querier"
	"github.com/grafana/mimir/pkg/querier/engine"
	"github.com/grafana/mimir/pkg/querier/tenantfederation"
	querier_worker "github.com/grafana/mimir/pkg/querier/worker"
	"github.com/grafana/mimir/pkg/ruler"
	"github.com/grafana/mimir/pkg/scheduler"
	"github.com/grafana/mimir/pkg/storage/bucket"
	"github.com/grafana/mimir/pkg/storegateway"
	"github.com/grafana/mimir/pkg/usagestats"
	"github.com/grafana/mimir/pkg/util"
	"github.com/grafana/mimir/pkg/util/activitytracker"
	util_log "github.com/grafana/mimir/pkg/util/log"
	"github.com/grafana/mimir/pkg/util/validation"
	"github.com/grafana/mimir/pkg/util/version"
)

// The various modules that make up Mimir.
const (
	ActivityTracker          string = "activity-tracker"
	API                      string = "api"
	SanityCheck              string = "sanity-check"
	Ring                     string = "ring"
	RuntimeConfig            string = "runtime-config"
	Overrides                string = "overrides"
	OverridesExporter        string = "overrides-exporter"
	Server                   string = "server"
	Distributor              string = "distributor"
	DistributorService       string = "distributor-service"
	Ingester                 string = "ingester"
	IngesterService          string = "ingester-service"
	Flusher                  string = "flusher"
	Querier                  string = "querier"
	Queryable                string = "queryable"
	StoreQueryable           string = "store-queryable"
	QueryFrontend            string = "query-frontend"
	QueryFrontendTripperware string = "query-frontend-tripperware"
	RulerStorage             string = "ruler-storage"
	Ruler                    string = "ruler"
	AlertManager             string = "alertmanager"
	Compactor                string = "compactor"
	StoreGateway             string = "store-gateway"
	MemberlistKV             string = "memberlist-kv"
	QueryScheduler           string = "query-scheduler"
	TenantFederation         string = "tenant-federation"
	UsageStats               string = "usage-stats"
	All                      string = "all"

	// Write Read and Backend are the targets used when using the read-write deployment mode.
	Write   string = "write"
	Read    string = "read"
	Backend string = "backend"
)

func newDefaultConfig() *Config {
	defaultConfig := &Config{}
	defaultFS := flag.NewFlagSet("", flag.PanicOnError)
	defaultConfig.RegisterFlags(defaultFS, util_log.Logger)
	return defaultConfig
}

func (t *Mimir) initAPI() (services.Service, error) {
	t.Cfg.API.ServerPrefix = t.Cfg.Server.PathPrefix

	a, err := api.New(t.Cfg.API, t.Cfg.Server, t.Server, util_log.Logger)
	if err != nil {
		return nil, err
	}

	t.BuildInfoHandler = version.BuildInfoHandler(
		t.Cfg.ApplicationName,
		version.BuildInfoFeatures{
			AlertmanagerConfigAPI: strconv.FormatBool(t.Cfg.Alertmanager.EnableAPI),
			QuerySharding:         strconv.FormatBool(t.Cfg.Frontend.QueryMiddleware.ShardedQueries),
			RulerConfigAPI:        strconv.FormatBool(t.Cfg.Ruler.EnableAPI),
			FederatedRules:        strconv.FormatBool(t.Cfg.Ruler.TenantFederation.Enabled),
		})

	t.API = a
	t.API.RegisterAPI(t.Cfg.Server.PathPrefix, t.Cfg, newDefaultConfig(), t.BuildInfoHandler)

	return nil, nil
}

func (t *Mimir) initActivityTracker() (services.Service, error) {
	if t.Cfg.ActivityTracker.Filepath == "" {
		return nil, nil
	}

	entries, err := activitytracker.LoadUnfinishedEntries(t.Cfg.ActivityTracker.Filepath)

	l := log.With(util_log.Logger, "component", "activity-tracker")
	if err != nil {
		level.Warn(l).Log("msg", "failed to fully read file with unfinished activities", "err", err)
	}
	if len(entries) > 0 {
		level.Warn(l).Log("msg", "found unfinished activities from previous run", "count", len(entries))
	}
	for _, e := range entries {
		level.Warn(l).Log("start", e.Timestamp.UTC().Format(time.RFC3339Nano), "activity", e.Activity)
	}

	at, err := activitytracker.NewActivityTracker(t.Cfg.ActivityTracker, t.Registerer)
	if err != nil {
		return nil, err
	}

	t.ActivityTracker = at

	return services.NewIdleService(nil, func(_ error) error {
		entries, err := activitytracker.LoadUnfinishedEntries(t.Cfg.ActivityTracker.Filepath)
		if err != nil {
			level.Warn(l).Log("msg", "failed to fully read file with unfinished activities during shutdown", "err", err)
		}
		if len(entries) > 0 {
			level.Warn(l).Log("msg", "found unfinished activities during shutdown", "count", len(entries))
		}
		for _, e := range entries {
			level.Warn(l).Log("start", e.Timestamp.UTC().Format(time.RFC3339Nano), "activity", e.Activity)
		}
		return nil
	}), nil
}

func (t *Mimir) initSanityCheck() (services.Service, error) {
	return services.NewIdleService(func(ctx context.Context) error {
		return runSanityCheck(ctx, t.Cfg, util_log.Logger)
	}, nil), nil
}

func (t *Mimir) initServer() (services.Service, error) {
	// Mimir handles signals on its own.
	DisableSignalHandling(&t.Cfg.Server)
	serv, err := server.New(t.Cfg.Server)
	if err != nil {
		return nil, err
	}

	t.Server = serv

	servicesToWaitFor := func() []services.Service {
		svs := []services.Service(nil)

		serverDeps := t.ModuleManager.DependenciesForModule(Server)

		for m, s := range t.ServiceMap {
			// Server should not wait for itself or for any of its dependencies.
			if m == Server {
				continue
			}

			if util.StringsContain(serverDeps, m) {
				continue
			}

			svs = append(svs, s)
		}
		return svs
	}

	s := NewServerService(t.Server, servicesToWaitFor)

	return s, nil
}

func (t *Mimir) initRing() (serv services.Service, err error) {
	t.Ring, err = ring.New(t.Cfg.Ingester.IngesterRing.ToRingConfig(), "ingester", ingester.IngesterRingKey, util_log.Logger, prometheus.WrapRegistererWithPrefix("cortex_", t.Registerer))
	if err != nil {
		return nil, err
	}
	return t.Ring, nil
}

func (t *Mimir) initRuntimeConfig() (services.Service, error) {
	if len(t.Cfg.RuntimeConfig.LoadPath) == 0 {
		// no need to initialize module if load path is empty
		return nil, nil
	}
	t.Cfg.RuntimeConfig.Loader = loadRuntimeConfig

	// make sure to set default limits before we start loading configuration into memory
	validation.SetDefaultLimitsForYAMLUnmarshalling(t.Cfg.LimitsConfig)

	serv, err := runtimeconfig.New(t.Cfg.RuntimeConfig, prometheus.WrapRegistererWithPrefix("cortex_", t.Registerer), util_log.Logger)
	if err == nil {
		// TenantLimits just delegates to RuntimeConfig and doesn't have any state or need to do
		// anything in the start/stopping phase. Thus we can create it as part of runtime config
		// setup without any service instance of its own.
		t.TenantLimits = newTenantLimits(serv)
	}

	t.RuntimeConfig = serv
	t.API.RegisterRuntimeConfig(runtimeConfigHandler(t.RuntimeConfig, t.Cfg.LimitsConfig), validation.UserLimitsHandler(t.Cfg.LimitsConfig, t.TenantLimits))

	// Update config fields using runtime config. Only if multiKV is used for given ring these returned functions will be
	// called and register the listener.
	//
	// By doing the initialization here instead of per-module init function, we avoid the problem
	// of projects based on Mimir forgetting the wiring if they override module's init method (they also don't have access to private symbols).
	t.Cfg.Alertmanager.ShardingRing.KVStore.Multi.ConfigProvider = multiClientRuntimeConfigChannel(t.RuntimeConfig)
	t.Cfg.Compactor.ShardingRing.KVStore.Multi.ConfigProvider = multiClientRuntimeConfigChannel(t.RuntimeConfig)
	t.Cfg.Distributor.DistributorRing.KVStore.Multi.ConfigProvider = multiClientRuntimeConfigChannel(t.RuntimeConfig)
	t.Cfg.Ingester.IngesterRing.KVStore.Multi.ConfigProvider = multiClientRuntimeConfigChannel(t.RuntimeConfig)
	t.Cfg.Ruler.Ring.KVStore.Multi.ConfigProvider = multiClientRuntimeConfigChannel(t.RuntimeConfig)
	t.Cfg.StoreGateway.ShardingRing.KVStore.Multi.ConfigProvider = multiClientRuntimeConfigChannel(t.RuntimeConfig)
	t.Cfg.QueryScheduler.ServiceDiscovery.SchedulerRing.KVStore.Multi.ConfigProvider = multiClientRuntimeConfigChannel(t.RuntimeConfig)

	return serv, err
}

func (t *Mimir) initOverrides() (serv services.Service, err error) {
	t.Overrides, err = validation.NewOverrides(t.Cfg.LimitsConfig, t.TenantLimits)
	// overrides don't have operational state, nor do they need to do anything more in starting/stopping phase,
	// so there is no need to return any service.
	return nil, err
}

func (t *Mimir) initOverridesExporter() (services.Service, error) {
	exporter := validation.NewOverridesExporter(&t.Cfg.LimitsConfig, t.TenantLimits)
	if t.Registerer != nil {
		t.Registerer.MustRegister(exporter)
	}

	// the overrides exporter has no state and reads overrides for runtime configuration each time it
	// is collected so there is no need to return any service
	return nil, nil
}

func (t *Mimir) initDistributorService() (serv services.Service, err error) {
	t.Cfg.Distributor.DistributorRing.ListenPort = t.Cfg.Server.GRPCListenPort

	// Only enable shuffle sharding on the read path when `query-ingesters-within`
	// is non-zero since otherwise we can't determine if an ingester should be part
	// of a tenant's shuffle sharding subring (we compare its registration time with
	// the lookback period).
	if t.Cfg.Querier.ShuffleShardingIngestersEnabled && t.Cfg.Querier.QueryIngestersWithin > 0 {
		t.Cfg.Distributor.ShuffleShardingLookbackPeriod = t.Cfg.Querier.QueryIngestersWithin
	}

	// Check whether the distributor can join the distributors ring, which is
	// whenever it's not running as an internal dependency (ie. querier or
	// ruler's dependency)
	canJoinDistributorsRing := t.Cfg.isAnyModuleEnabled(Distributor, Write, All)

	t.Distributor, err = distributor.New(t.Cfg.Distributor, t.Cfg.IngesterClient, t.Overrides, t.Ring, canJoinDistributorsRing, t.Registerer, util_log.Logger)
	if err != nil {
		return
	}

	return t.Distributor, nil
}

func (t *Mimir) initDistributor() (serv services.Service, err error) {
	t.API.RegisterDistributor(t.Distributor, t.Cfg.Distributor, t.Registerer)

	return nil, nil
}

// initQueryable instantiates the queryable and promQL engine used to service queries to
// Mimir. It also registers the API endpoints associated with those two services.
func (t *Mimir) initQueryable() (serv services.Service, err error) {
	querierRegisterer := prometheus.WrapRegistererWith(prometheus.Labels{"engine": "querier"}, t.Registerer)

	// Create a querier queryable and PromQL engine
	t.QuerierQueryable, t.ExemplarQueryable, t.QuerierEngine = querier.New(t.Cfg.Querier, t.Overrides, t.Distributor, t.StoreQueryables, querierRegisterer, util_log.Logger, t.ActivityTracker)

	// Use the distributor to return metric metadata by default
	t.MetadataSupplier = t.Distributor

	// Register the default endpoints that are always enabled for the querier module
	t.API.RegisterQueryable(t.QuerierQueryable, t.Distributor)

	return nil, nil
}

// Enable merge querier if multi tenant query federation is enabled
func (t *Mimir) initTenantFederation() (serv services.Service, err error) {
	if t.Cfg.TenantFederation.Enabled {
		// Make sure the mergeQuerier is only used for request with more than a
		// single tenant. This allows for a less impactful enabling of tenant
		// federation.
		const bypassForSingleQuerier = true
		t.QuerierQueryable = querier.NewSampleAndChunkQueryable(tenantfederation.NewQueryable(t.QuerierQueryable, bypassForSingleQuerier, util_log.Logger))
		t.ExemplarQueryable = tenantfederation.NewExemplarQueryable(t.ExemplarQueryable, bypassForSingleQuerier, util_log.Logger)
		t.MetadataSupplier = tenantfederation.NewMetadataSupplier(t.MetadataSupplier, util_log.Logger)
	}
	return nil, nil
}

// initQuerier registers an internal HTTP router with a Prometheus API backed by the
// Mimir Queryable. Then it does one of the following:
//
//  1. Query-Frontend Enabled: If Mimir has an All or QueryFrontend target, the internal
//     HTTP router is wrapped with Tenant ID parsing middleware and passed to the frontend
//     worker.
//
//  2. Querier Standalone: The querier will register the internal HTTP router with the external
//     HTTP router for the Prometheus API routes. Then the external HTTP server will be passed
//     as a http.Handler to the frontend worker.
//
// Route Diagram:
//
//	                      │  query
//	                      │ request
//	                      │
//	                      ▼
//	            ┌──────────────────┐    QF to      ┌──────────────────┐
//	            │  external HTTP   │    Worker     │                  │
//	            │      router      │──────────────▶│ frontend worker  │
//	            │                  │               │                  │
//	            └──────────────────┘               └──────────────────┘
//	                      │                                  │
//	                                                         │
//	             only in  │                                  │
//	          microservice         ┌──────────────────┐      │
//	            querier   │        │ internal Querier │      │
//	                       ─ ─ ─ ─▶│      router      │◀─────┘
//	                               │                  │
//	                               └──────────────────┘
//	                                         │
//	                                         │
//	/metadata & /chunk ┌─────────────────────┼─────────────────────┐
//	      requests     │                     │                     │
//	                   │                     │                     │
//	                   ▼                     ▼                     ▼
//	         ┌──────────────────┐  ┌──────────────────┐  ┌────────────────────┐
//	         │                  │  │                  │  │                    │
//	         │Querier Queryable │  │  /api/v1 router  │  │ /prometheus router │
//	         │                  │  │                  │  │                    │
//	         └──────────────────┘  └──────────────────┘  └────────────────────┘
//	                   ▲                     │                     │
//	                   │                     └──────────┬──────────┘
//	                   │                                ▼
//	                   │                      ┌──────────────────┐
//	                   │                      │                  │
//	                   └──────────────────────│  Prometheus API  │
//	                                          │                  │
//	                                          └──────────────────┘
func (t *Mimir) initQuerier() (serv services.Service, err error) {
	t.Cfg.Worker.MaxConcurrentRequests = t.Cfg.Querier.EngineConfig.MaxConcurrent
	t.Cfg.Worker.QuerySchedulerDiscovery = t.Cfg.QueryScheduler.ServiceDiscovery

	// Create a internal HTTP handler that is configured with the Prometheus API routes and points
	// to a Prometheus API struct instantiated with the Mimir Queryable.
	internalQuerierRouter := api.NewQuerierHandler(
		t.Cfg.API,
		t.QuerierQueryable,
		t.ExemplarQueryable,
		t.MetadataSupplier,
		t.QuerierEngine,
		t.Distributor,
		t.Registerer,
		util_log.Logger,
		t.Overrides,
	)

	// If the querier is running standalone without the query-frontend or query-scheduler, we must register it's internal
	// HTTP handler externally and provide the external Mimir Server HTTP handler to the frontend worker
	// to ensure requests it processes use the default middleware instrumentation.
	if !t.Cfg.isAnyModuleEnabled(QueryFrontend, QueryScheduler, Read, All) {
		// First, register the internal querier handler with the external HTTP server
		t.API.RegisterQueryAPI(internalQuerierRouter, t.BuildInfoHandler)

		// Second, set the http.Handler that the frontend worker will use to process requests to point to
		// the external HTTP server. This will allow the querier to consolidate query metrics both external
		// and internal using the default instrumentation when running as a standalone service.
		internalQuerierRouter = t.Server.HTTPServer.Handler
	} else {
		// Monolithic mode requires a query-frontend endpoint for the worker. If no frontend and scheduler endpoint
		// is configured, Mimir will default to using frontend on localhost on it's own GRPC listening port.
		if !t.Cfg.Worker.IsFrontendOrSchedulerConfigured() {
			address := fmt.Sprintf("127.0.0.1:%d", t.Cfg.Server.GRPCListenPort)
			level.Info(util_log.Logger).Log("msg", "The querier worker has not been configured with either the query-frontend or query-scheduler address. Because Mimir is running in monolithic mode, it's attempting an automatic worker configuration. If queries are unresponsive, consider explicitly configuring the query-frontend or query-scheduler address for querier worker.", "address", address)
			t.Cfg.Worker.FrontendAddress = address
		}

		// Add a middleware to extract the trace context and add a header.
		internalQuerierRouter = nethttp.MiddlewareFunc(opentracing.GlobalTracer(), internalQuerierRouter.ServeHTTP, nethttp.OperationNameFunc(func(r *http.Request) string {
			return "internalQuerier"
		}))

		// If queries are processed using the external HTTP Server, we need wrap the internal querier with
		// HTTP router with middleware to parse the tenant ID from the HTTP header and inject it into the
		// request context.
		internalQuerierRouter = t.API.AuthMiddleware.Wrap(internalQuerierRouter)
	}

	// If neither query-frontend or query-scheduler is in use, then no worker is needed.
	if !t.Cfg.Worker.IsFrontendOrSchedulerConfigured() {
		return nil, nil
	}

	return querier_worker.NewQuerierWorker(t.Cfg.Worker, httpgrpc_server.NewServer(internalQuerierRouter), util_log.Logger, t.Registerer)
}

func (t *Mimir) initStoreQueryables() (services.Service, error) {
	var servs []services.Service

	//nolint:revive // I prefer this form over removing 'else', because it allows q to have smaller scope.
	if q, err := querier.NewBlocksStoreQueryableFromConfig(t.Cfg.Querier, t.Cfg.StoreGateway, t.Cfg.BlocksStorage, t.Overrides, util_log.Logger, t.Registerer); err != nil {
		return nil, fmt.Errorf("failed to initialize querier: %v", err)
	} else {
		t.StoreQueryables = append(t.StoreQueryables, querier.UseAlwaysQueryable(q))
		servs = append(servs, q)
	}

	// Return service, if any.
	switch len(servs) {
	case 0:
		return nil, nil
	case 1:
		return servs[0], nil
	default:
		// No need to support this case yet, since chunk store is not a service.
		// When we get there, we will need a wrapper service, that starts all subservices, and will also monitor them for failures.
		// Not difficult, but also not necessary right now.
		return nil, fmt.Errorf("too many services")
	}
}

func (t *Mimir) tsdbIngesterConfig() {
	t.Cfg.Ingester.BlocksStorageConfig = t.Cfg.BlocksStorage
}

func (t *Mimir) initIngesterService() (serv services.Service, err error) {
	t.Cfg.Ingester.IngesterRing.ListenPort = t.Cfg.Server.GRPCListenPort
	t.Cfg.Ingester.StreamTypeFn = ingesterChunkStreaming(t.RuntimeConfig)
	t.Cfg.Ingester.InstanceLimitsFn = ingesterInstanceLimits(t.RuntimeConfig)
	t.tsdbIngesterConfig()

	t.Ingester, err = ingester.New(t.Cfg.Ingester, t.Overrides, t.Registerer, util_log.Logger)
	if err != nil {
		return
	}

	return t.Ingester, nil
}

func (t *Mimir) initIngester() (serv services.Service, err error) {
	var ing api.Ingester

	ing = t.Ingester
	if t.ActivityTracker != nil {
		ing = ingester.NewIngesterActivityTracker(t.Ingester, t.ActivityTracker)
	}
	t.API.RegisterIngester(ing, t.Cfg.Distributor)
	return nil, nil
}

func (t *Mimir) initFlusher() (serv services.Service, err error) {
	t.tsdbIngesterConfig()

	t.Flusher, err = flusher.New(
		t.Cfg.Flusher,
		t.Cfg.Ingester,
		t.Overrides,
		t.Registerer,
		util_log.Logger,
	)
	if err != nil {
		return
	}

	return t.Flusher, nil
}

// initQueryFrontendTripperware instantiates the tripperware used by the query frontend
// to optimize Prometheus query requests.
func (t *Mimir) initQueryFrontendTripperware() (serv services.Service, err error) {
	promqlEngineRegisterer := prometheus.WrapRegistererWith(prometheus.Labels{"engine": "query-frontend"}, t.Registerer)

	tripperware, err := querymiddleware.NewTripperware(
		t.Cfg.Frontend.QueryMiddleware,
		util_log.Logger,
		t.Overrides,
		querymiddleware.PrometheusCodec,
		querymiddleware.PrometheusResponseExtractor{},
		engine.NewPromQLEngineOptions(t.Cfg.Querier.EngineConfig, t.ActivityTracker, util_log.Logger, promqlEngineRegisterer),
		t.Registerer,
	)
	if err != nil {
		return nil, err
	}

	t.QueryFrontendTripperware = tripperware
	return nil, nil
}

func (t *Mimir) initQueryFrontend() (serv services.Service, err error) {
	t.Cfg.Frontend.FrontendV2.QuerySchedulerDiscovery = t.Cfg.QueryScheduler.ServiceDiscovery

	roundTripper, frontendV1, frontendV2, err := frontend.InitFrontend(t.Cfg.Frontend, t.Overrides, t.Cfg.Server.GRPCListenPort, util_log.Logger, t.Registerer)
	if err != nil {
		return nil, err
	}

	// Wrap roundtripper into Tripperware.
	roundTripper = t.QueryFrontendTripperware(roundTripper)

	handler := transport.NewHandler(t.Cfg.Frontend.Handler, roundTripper, util_log.Logger, t.Registerer, t.ActivityTracker)
	t.API.RegisterQueryFrontendHandler(handler, t.BuildInfoHandler)

	if frontendV1 != nil {
		t.API.RegisterQueryFrontend1(frontendV1)
		t.Frontend = frontendV1

		return frontendV1, nil
	} else if frontendV2 != nil {
		t.API.RegisterQueryFrontend2(frontendV2)

		return frontendV2, nil
	}

	return nil, nil
}

func (t *Mimir) initRulerStorage() (serv services.Service, err error) {
	// If the ruler is not configured and Mimir is running in monolithic or read-write mode, then we just skip starting the ruler.
	if t.Cfg.isAnyModuleEnabled(Backend, All) && t.Cfg.RulerStorage.IsDefaults() {
		level.Info(util_log.Logger).Log("msg", "The ruler is not being started because you need to configure the ruler storage.")
		return
	}

	t.RulerStorage, err = ruler.NewRuleStore(context.Background(), t.Cfg.RulerStorage, t.Overrides, rules.FileLoader{}, util_log.Logger, t.Registerer)
	return
}

func (t *Mimir) initRuler() (serv services.Service, err error) {
	if t.RulerStorage == nil {
		level.Info(util_log.Logger).Log("msg", "RulerStorage is nil.  Not starting the ruler.")
		return nil, nil
	}

	t.Cfg.Ruler.Ring.ListenPort = t.Cfg.Server.GRPCListenPort

	var embeddedQueryable prom_storage.Queryable
	var queryFunc rules.QueryFunc

	if t.Cfg.Ruler.QueryFrontend.Address != "" {
		queryFrontendClient, err := ruler.DialQueryFrontend(t.Cfg.Ruler.QueryFrontend)
		if err != nil {
			return nil, err
		}
		remoteQuerier := ruler.NewRemoteQuerier(queryFrontendClient, t.Cfg.Querier.EngineConfig.Timeout, t.Cfg.API.PrometheusHTTPPrefix, util_log.Logger, ruler.WithOrgIDMiddleware)

		embeddedQueryable = prom_remote.NewSampleAndChunkQueryableClient(
			remoteQuerier,
			labels.Labels{},
			nil,
			true,
			func() (int64, error) { return 0, nil },
		)
		queryFunc = remoteQuerier.Query

	} else {
		var queryable, federatedQueryable prom_storage.Queryable

		// TODO: Consider wrapping logger to differentiate from querier module logger
		rulerRegisterer := prometheus.WrapRegistererWith(prometheus.Labels{"engine": "ruler"}, t.Registerer)

		queryable, _, eng := querier.New(t.Cfg.Querier, t.Overrides, t.Distributor, t.StoreQueryables, rulerRegisterer, util_log.Logger, t.ActivityTracker)
		queryable = querier.NewErrorTranslateQueryableWithFn(queryable, ruler.WrapQueryableErrors)

		if t.Cfg.Ruler.TenantFederation.Enabled {
			if !t.Cfg.TenantFederation.Enabled {
				return nil, errors.New("-ruler.tenant-federation.enabled=true requires -tenant-federation.enabled=true")
			}
			// Setting bypassForSingleQuerier=false forces `tenantfederation.NewQueryable` to add
			// the `__tenant_id__` label on all metrics regardless if they're for a single tenant or multiple tenants.
			// This makes this label more consistent and hopefully less confusing to users.
			const bypassForSingleQuerier = false

			federatedQueryable = tenantfederation.NewQueryable(queryable, bypassForSingleQuerier, util_log.Logger)

			regularQueryFunc := rules.EngineQueryFunc(eng, queryable)
			federatedQueryFunc := rules.EngineQueryFunc(eng, federatedQueryable)

			embeddedQueryable = federatedQueryable
			queryFunc = ruler.TenantFederationQueryFunc(regularQueryFunc, federatedQueryFunc)

		} else {
			embeddedQueryable = queryable
			queryFunc = rules.EngineQueryFunc(eng, queryable)
		}
	}
	managerFactory := ruler.DefaultTenantManagerFactory(
		t.Cfg.Ruler,
		t.Distributor,
		embeddedQueryable,
		queryFunc,
		t.Overrides,
		t.Registerer,
	)

	// We need to prefix and add a label to the metrics for the DNS resolver because, unlike other mimir components,
	// it doesn't already have the `cortex_` prefix and the `component` label to the metrics it emits
	dnsProviderReg := prometheus.WrapRegistererWithPrefix(
		"cortex_",
		prometheus.WrapRegistererWith(
			prometheus.Labels{"component": "ruler"},
			t.Registerer,
		),
	)

	dnsResolver := dns.NewProvider(util_log.Logger, dnsProviderReg, dns.GolangResolverType)
	manager, err := ruler.NewDefaultMultiTenantManager(t.Cfg.Ruler, managerFactory, t.Registerer, util_log.Logger, dnsResolver)
	if err != nil {
		return nil, err
	}

	t.Ruler, err = ruler.NewRuler(
		t.Cfg.Ruler,
		manager,
		t.Registerer,
		util_log.Logger,
		t.RulerStorage,
		t.Overrides,
	)
	if err != nil {
		return
	}

	// Expose HTTP/GRPC admin endpoints for the Ruler service
	t.API.RegisterRuler(t.Ruler)

	// Expose HTTP configuration and prometheus-compatible Ruler APIs
	t.API.RegisterRulerAPI(ruler.NewAPI(t.Ruler, t.RulerStorage, util_log.Logger), t.Cfg.Ruler.EnableAPI, t.BuildInfoHandler)

	return t.Ruler, nil
}

func (t *Mimir) initAlertManager() (serv services.Service, err error) {
	t.Cfg.Alertmanager.ShardingRing.ListenPort = t.Cfg.Server.GRPCListenPort
	t.Cfg.Alertmanager.CheckExternalURL(t.Cfg.API.AlertmanagerHTTPPrefix, util_log.Logger)

	store, err := alertstore.NewAlertStore(context.Background(), t.Cfg.AlertmanagerStorage, t.Overrides, util_log.Logger, t.Registerer)
	if err != nil {
		return
	}

	t.Alertmanager, err = alertmanager.NewMultitenantAlertmanager(&t.Cfg.Alertmanager, store, t.Overrides, util_log.Logger, t.Registerer)
	if err != nil {
		return
	}

	t.API.RegisterAlertmanager(t.Alertmanager, t.Cfg.Alertmanager.EnableAPI, t.BuildInfoHandler)
	return t.Alertmanager, nil
}

func (t *Mimir) initCompactor() (serv services.Service, err error) {
	t.Cfg.Compactor.ShardingRing.ListenPort = t.Cfg.Server.GRPCListenPort

	t.Compactor, err = compactor.NewMultitenantCompactor(t.Cfg.Compactor, t.Cfg.BlocksStorage, t.Overrides, util_log.Logger, t.Registerer)
	if err != nil {
		return
	}

	// Expose HTTP endpoints.
	t.API.RegisterCompactor(t.Compactor)
	return t.Compactor, nil
}

func (t *Mimir) initStoreGateway() (serv services.Service, err error) {
	t.Cfg.StoreGateway.ShardingRing.ListenPort = t.Cfg.Server.GRPCListenPort

	t.StoreGateway, err = storegateway.NewStoreGateway(t.Cfg.StoreGateway, t.Cfg.BlocksStorage, t.Overrides, t.Cfg.Server.LogLevel, util_log.Logger, t.Registerer, t.ActivityTracker)
	if err != nil {
		return nil, err
	}

	// Expose HTTP endpoints.
	t.API.RegisterStoreGateway(t.StoreGateway)

	return t.StoreGateway, nil
}

func (t *Mimir) initMemberlistKV() (services.Service, error) {
	reg := t.Registerer
	t.Cfg.MemberlistKV.MetricsRegisterer = reg
	t.Cfg.MemberlistKV.Codecs = []codec.Codec{
		ring.GetCodec(),
	}
	dnsProviderReg := prometheus.WrapRegistererWithPrefix(
		"cortex_",
		prometheus.WrapRegistererWith(
			prometheus.Labels{"component": "memberlist"},
			reg,
		),
	)
	dnsProvider := dns.NewProvider(util_log.Logger, dnsProviderReg, dns.GolangResolverType)
	t.MemberlistKV = memberlist.NewKVInitService(&t.Cfg.MemberlistKV, util_log.Logger, dnsProvider, reg)
	t.API.RegisterMemberlistKV(t.Cfg.Server.PathPrefix, t.MemberlistKV)

	// Update the config.
	t.Cfg.Distributor.DistributorRing.KVStore.MemberlistKV = t.MemberlistKV.GetMemberlistKV
	t.Cfg.Ingester.IngesterRing.KVStore.MemberlistKV = t.MemberlistKV.GetMemberlistKV
	t.Cfg.StoreGateway.ShardingRing.KVStore.MemberlistKV = t.MemberlistKV.GetMemberlistKV
	t.Cfg.Compactor.ShardingRing.KVStore.MemberlistKV = t.MemberlistKV.GetMemberlistKV
	t.Cfg.Ruler.Ring.KVStore.MemberlistKV = t.MemberlistKV.GetMemberlistKV
	t.Cfg.Alertmanager.ShardingRing.KVStore.MemberlistKV = t.MemberlistKV.GetMemberlistKV
	t.Cfg.QueryScheduler.ServiceDiscovery.SchedulerRing.KVStore.MemberlistKV = t.MemberlistKV.GetMemberlistKV

	return t.MemberlistKV, nil
}

func (t *Mimir) initQueryScheduler() (services.Service, error) {
	t.Cfg.QueryScheduler.ServiceDiscovery.SchedulerRing.ListenPort = t.Cfg.Server.GRPCListenPort

	s, err := scheduler.NewScheduler(t.Cfg.QueryScheduler, t.Overrides, util_log.Logger, t.Registerer)
	if err != nil {
		return nil, errors.Wrap(err, "query-scheduler init")
	}

	t.API.RegisterQueryScheduler(s)
	return s, nil
}

func (t *Mimir) initUsageStats() (services.Service, error) {
	if !t.Cfg.UsageStats.Enabled {
		return nil, nil
	}

	// Since it requires the access to the blocks storage, we enable it only for components
	// accessing the blocks storage.
	if !t.Cfg.isAnyModuleEnabled(All, Write, Read, Backend, Ingester, Querier, StoreGateway, Compactor) {
		return nil, nil
	}

	bucketClient, err := bucket.NewClient(context.Background(), t.Cfg.BlocksStorage.Bucket, UsageStats, util_log.Logger, t.Registerer)
	if err != nil {
		return nil, errors.Wrapf(err, "create %s bucket client", UsageStats)
	}

	// Track anonymous usage statistics.
	usagestats.GetString("blocks_storage_backend").Set(t.Cfg.BlocksStorage.Bucket.Backend)
	usagestats.GetString("installation_mode").Set(t.Cfg.UsageStats.InstallationMode)

	t.UsageStatsReporter = usagestats.NewReporter(bucketClient, util_log.Logger, t.Registerer)
	return t.UsageStatsReporter, nil
}

func (t *Mimir) setupModuleManager() error {
	mm := modules.NewManager(util_log.Logger)

	// Register all modules here.
	// RegisterModule(name string, initFn func()(services.Service, error))
	mm.RegisterModule(Server, t.initServer, modules.UserInvisibleModule)
	mm.RegisterModule(ActivityTracker, t.initActivityTracker, modules.UserInvisibleModule)
	mm.RegisterModule(SanityCheck, t.initSanityCheck, modules.UserInvisibleModule)
	mm.RegisterModule(API, t.initAPI, modules.UserInvisibleModule)
	mm.RegisterModule(RuntimeConfig, t.initRuntimeConfig, modules.UserInvisibleModule)
	mm.RegisterModule(MemberlistKV, t.initMemberlistKV, modules.UserInvisibleModule)
	mm.RegisterModule(Ring, t.initRing, modules.UserInvisibleModule)
	mm.RegisterModule(Overrides, t.initOverrides, modules.UserInvisibleModule)
	mm.RegisterModule(OverridesExporter, t.initOverridesExporter)
	mm.RegisterModule(Distributor, t.initDistributor)
	mm.RegisterModule(DistributorService, t.initDistributorService, modules.UserInvisibleModule)
	mm.RegisterModule(Ingester, t.initIngester)
	mm.RegisterModule(IngesterService, t.initIngesterService, modules.UserInvisibleModule)
	mm.RegisterModule(Flusher, t.initFlusher)
	mm.RegisterModule(Queryable, t.initQueryable, modules.UserInvisibleModule)
	mm.RegisterModule(Querier, t.initQuerier)
	mm.RegisterModule(StoreQueryable, t.initStoreQueryables, modules.UserInvisibleModule)
	mm.RegisterModule(QueryFrontendTripperware, t.initQueryFrontendTripperware, modules.UserInvisibleModule)
	mm.RegisterModule(QueryFrontend, t.initQueryFrontend)
	mm.RegisterModule(RulerStorage, t.initRulerStorage, modules.UserInvisibleModule)
	mm.RegisterModule(Ruler, t.initRuler)
	mm.RegisterModule(AlertManager, t.initAlertManager)
	mm.RegisterModule(Compactor, t.initCompactor)
	mm.RegisterModule(StoreGateway, t.initStoreGateway)
	mm.RegisterModule(QueryScheduler, t.initQueryScheduler)
	mm.RegisterModule(TenantFederation, t.initTenantFederation, modules.UserInvisibleModule)
	mm.RegisterModule(UsageStats, t.initUsageStats, modules.UserInvisibleModule)
	mm.RegisterModule(Write, nil)
	mm.RegisterModule(Read, nil)
	mm.RegisterModule(Backend, nil)
	mm.RegisterModule(All, nil)

	// Add dependencies
	deps := map[string][]string{
		Server:                   {ActivityTracker, SanityCheck, UsageStats},
		API:                      {Server},
		MemberlistKV:             {API},
		RuntimeConfig:            {API},
		Ring:                     {API, RuntimeConfig, MemberlistKV},
		Overrides:                {RuntimeConfig},
		OverridesExporter:        {Overrides},
		Distributor:              {DistributorService, API},
		DistributorService:       {Ring, Overrides},
		Ingester:                 {IngesterService, API},
		IngesterService:          {Overrides, RuntimeConfig, MemberlistKV},
		Flusher:                  {Overrides, API},
		Queryable:                {Overrides, DistributorService, Ring, API, StoreQueryable, MemberlistKV},
		Querier:                  {TenantFederation},
		StoreQueryable:           {Overrides, MemberlistKV},
		QueryFrontendTripperware: {API, Overrides},
		QueryFrontend:            {QueryFrontendTripperware, MemberlistKV},
		QueryScheduler:           {API, Overrides, MemberlistKV},
		Ruler:                    {DistributorService, StoreQueryable, RulerStorage},
		RulerStorage:             {Overrides},
		AlertManager:             {API, MemberlistKV, Overrides},
		Compactor:                {API, MemberlistKV, Overrides},
		StoreGateway:             {API, Overrides, MemberlistKV},
		TenantFederation:         {Queryable},
		Write:                    {Distributor, Ingester},
		Read:                     {QueryFrontend, Querier},
		Backend:                  {QueryScheduler, Ruler, StoreGateway, Compactor, AlertManager, OverridesExporter},
		All:                      {QueryFrontend, Querier, Ingester, Distributor, StoreGateway, Ruler, Compactor},
	}
	for mod, targets := range deps {
		if err := mm.AddDependency(mod, targets...); err != nil {
			return err
		}
	}

	t.ModuleManager = mm

	return nil
}
