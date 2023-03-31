package loki

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/NYTimes/gziphandler"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/dns"
	"github.com/grafana/dskit/kv/codec"
	"github.com/grafana/dskit/kv/memberlist"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/runtimeconfig"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/tenant"
	gerrors "github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/common/version"
	"github.com/weaveworks/common/middleware"
	"github.com/weaveworks/common/server"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/distributor"
	"github.com/grafana/loki/pkg/ingester"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/lokifrontend/frontend"
	"github.com/grafana/loki/pkg/lokifrontend/frontend/transport"
	"github.com/grafana/loki/pkg/lokifrontend/frontend/v1/frontendv1pb"
	"github.com/grafana/loki/pkg/lokifrontend/frontend/v2/frontendv2pb"
	"github.com/grafana/loki/pkg/querier"
	"github.com/grafana/loki/pkg/querier/queryrange"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/ruler"
	base_ruler "github.com/grafana/loki/pkg/ruler/base"
	"github.com/grafana/loki/pkg/runtime"
	"github.com/grafana/loki/pkg/scheduler"
	"github.com/grafana/loki/pkg/scheduler/schedulerpb"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	chunk_util "github.com/grafana/loki/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/compactor"
	compactor_client "github.com/grafana/loki/pkg/storage/stores/indexshipper/compactor/client"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/compactor/client/grpc"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/compactor/deletion"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/compactor/generationnumber"
	"github.com/grafana/loki/pkg/storage/stores/series/index"
	shipper_index "github.com/grafana/loki/pkg/storage/stores/shipper/index"
	boltdb_shipper_compactor "github.com/grafana/loki/pkg/storage/stores/shipper/index/compactor"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexgateway"
	"github.com/grafana/loki/pkg/storage/stores/tsdb"
	"github.com/grafana/loki/pkg/usagestats"
	"github.com/grafana/loki/pkg/util/httpreq"
	"github.com/grafana/loki/pkg/util/limiter"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/util/querylimits"
	serverutil "github.com/grafana/loki/pkg/util/server"
	"github.com/grafana/loki/pkg/validation"
)

const maxChunkAgeForTableManager = 12 * time.Hour

// The various modules that make up Loki.
const (
	Ring                     string = "ring"
	RuntimeConfig            string = "runtime-config"
	Overrides                string = "overrides"
	OverridesExporter        string = "overrides-exporter"
	TenantConfigs            string = "tenant-configs"
	Server                   string = "server"
	InternalServer           string = "internal-server"
	Distributor              string = "distributor"
	Ingester                 string = "ingester"
	Querier                  string = "querier"
	CacheGenerationLoader    string = "cache-generation-loader"
	IngesterQuerier          string = "ingester-querier"
	QueryFrontend            string = "query-frontend"
	QueryFrontendTripperware string = "query-frontend-tripperware"
	QueryLimiter             string = "query-limiter"
	QueryLimitsInterceptors  string = "query-limits-interceptors"
	QueryLimitsTripperware   string = "query-limits-tripper"
	RulerStorage             string = "ruler-storage"
	Ruler                    string = "ruler"
	RuleEvaluator            string = "rule-evaluator"
	Store                    string = "store"
	TableManager             string = "table-manager"
	MemberlistKV             string = "memberlist-kv"
	Compactor                string = "compactor"
	IndexGateway             string = "index-gateway"
	IndexGatewayRing         string = "index-gateway-ring"
	QueryScheduler           string = "query-scheduler"
	All                      string = "all"
	Read                     string = "read"
	Write                    string = "write"
	Backend                  string = "backend"
	UsageReport              string = "usage-report"
)

func (t *Loki) initServer() (services.Service, error) {
	prometheus.MustRegister(version.NewCollector("loki"))
	// unregister default go collector
	prometheus.Unregister(collectors.NewGoCollector())
	// register collector with additional metrics
	prometheus.MustRegister(collectors.NewGoCollector(
		collectors.WithGoCollectorRuntimeMetrics(collectors.MetricsAll),
	))

	// Loki handles signals on its own.
	DisableSignalHandling(&t.Cfg.Server)
	serv, err := server.New(t.Cfg.Server)
	if err != nil {
		return nil, err
	}

	t.Server = serv

	servicesToWaitFor := func() []services.Service {
		svs := []services.Service(nil)
		for m, s := range t.serviceMap {
			// Server should not wait for itself.
			if m != Server {
				svs = append(svs, s)
			}
		}
		return svs
	}

	s := NewServerService(t.Server, servicesToWaitFor)

	// Best effort to propagate the org ID from the start.
	h := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !t.Cfg.AuthEnabled {
				next.ServeHTTP(w, r.WithContext(user.InjectOrgID(r.Context(), "fake")))
				return
			}

			_, ctx, _ := user.ExtractOrgIDFromHTTPRequest(r)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}(t.Server.HTTPServer.Handler)

	t.Server.HTTPServer.Handler = middleware.Merge(serverutil.RecoveryHTTPMiddleware).Wrap(h)

	if t.Cfg.Server.HTTPListenPort == 0 {
		t.Cfg.Server.HTTPListenPort = portFromAddr(t.Server.HTTPListenAddr().String())
	}

	if t.Cfg.Server.GRPCListenPort == 0 {
		t.Cfg.Server.GRPCListenPort = portFromAddr(t.Server.GRPCListenAddr().String())
	}

	return s, nil
}

func portFromAddr(addr string) int {
	parts := strings.Split(addr, ":")
	port := parts[len(parts)-1]
	portNumber, err := strconv.Atoi(port)
	if err != nil {
		return 0
	}
	return portNumber
}

func (t *Loki) initInternalServer() (services.Service, error) {
	// Loki handles signals on its own.
	DisableSignalHandling(&t.Cfg.InternalServer.Config)
	serv, err := server.New(t.Cfg.InternalServer.Config)
	if err != nil {
		return nil, err
	}

	t.InternalServer = serv

	servicesToWaitFor := func() []services.Service {
		svs := []services.Service(nil)
		for m, s := range t.serviceMap {
			// Server should not wait for itself.
			if m != InternalServer {
				svs = append(svs, s)
			}
		}
		return svs
	}

	s := NewServerService(t.InternalServer, servicesToWaitFor)

	return s, nil
}

func (t *Loki) initRing() (_ services.Service, err error) {
	t.ring, err = ring.New(t.Cfg.Ingester.LifecyclerConfig.RingConfig, "ingester", ingester.RingKey, util_log.Logger, prometheus.WrapRegistererWithPrefix("cortex_", prometheus.DefaultRegisterer))
	if err != nil {
		return
	}
	t.Server.HTTP.Path("/ring").Methods("GET", "POST").Handler(t.ring)

	if t.Cfg.InternalServer.Enable {
		t.InternalServer.HTTP.Path("/ring").Methods("GET").Handler(t.ring)
	}
	return t.ring, nil
}

func (t *Loki) initRuntimeConfig() (services.Service, error) {
	if len(t.Cfg.RuntimeConfig.LoadPath) == 0 {
		if len(t.Cfg.LimitsConfig.PerTenantOverrideConfig) != 0 {
			t.Cfg.RuntimeConfig.LoadPath = []string{t.Cfg.LimitsConfig.PerTenantOverrideConfig}
		}
		t.Cfg.RuntimeConfig.ReloadPeriod = time.Duration(t.Cfg.LimitsConfig.PerTenantOverridePeriod)
	}

	if len(t.Cfg.RuntimeConfig.LoadPath) == 0 {
		// no need to initialize module if load path is empty
		return nil, nil
	}

	t.Cfg.RuntimeConfig.Loader = loadRuntimeConfig

	// make sure to set default limits before we start loading configuration into memory
	validation.SetDefaultLimitsForYAMLUnmarshalling(t.Cfg.LimitsConfig)

	var err error
	t.runtimeConfig, err = runtimeconfig.New(t.Cfg.RuntimeConfig, prometheus.WrapRegistererWithPrefix("loki_", prometheus.DefaultRegisterer), util_log.Logger)
	t.TenantLimits = newtenantLimitsFromRuntimeConfig(t.runtimeConfig)

	// Update config fields using runtime config. Only if multiKV is used for given ring these returned functions will be
	// called and register the listener.
	//
	// By doing the initialization here instead of per-module init function, we avoid the problem
	// of projects based on Loki forgetting the wiring if they override module's init method (they also don't have access to private symbols).
	t.Cfg.CompactorConfig.CompactorRing.KVStore.Multi.ConfigProvider = multiClientRuntimeConfigChannel(t.runtimeConfig)
	t.Cfg.Distributor.DistributorRing.KVStore.Multi.ConfigProvider = multiClientRuntimeConfigChannel(t.runtimeConfig)
	t.Cfg.IndexGateway.Ring.KVStore.Multi.ConfigProvider = multiClientRuntimeConfigChannel(t.runtimeConfig)
	t.Cfg.Ingester.LifecyclerConfig.RingConfig.KVStore.Multi.ConfigProvider = multiClientRuntimeConfigChannel(t.runtimeConfig)
	t.Cfg.QueryScheduler.SchedulerRing.KVStore.Multi.ConfigProvider = multiClientRuntimeConfigChannel(t.runtimeConfig)
	t.Cfg.Ruler.Ring.KVStore.Multi.ConfigProvider = multiClientRuntimeConfigChannel(t.runtimeConfig)

	return t.runtimeConfig, err
}

func (t *Loki) initOverrides() (_ services.Service, err error) {
	t.Overrides, err = validation.NewOverrides(t.Cfg.LimitsConfig, t.TenantLimits)
	// overrides are not a service, since they don't have any operational state.
	return nil, err
}

func (t *Loki) initOverridesExporter() (services.Service, error) {
	if t.Cfg.isModuleEnabled(OverridesExporter) && t.TenantLimits == nil || t.Overrides == nil {
		// This target isn't enabled by default ("all") and requires per-tenant limits to run.
		return nil, errors.New("overrides-exporter has been enabled, but no runtime configuration file was configured")
	}

	exporter := validation.NewOverridesExporter(t.Overrides)
	prometheus.MustRegister(exporter)

	// The overrides-exporter has no state and reads overrides for runtime configuration each time it
	// is collected so there is no need to return any service.
	return nil, nil
}

func (t *Loki) initTenantConfigs() (_ services.Service, err error) {
	t.tenantConfigs, err = runtime.NewTenantConfigs(tenantConfigFromRuntimeConfig(t.runtimeConfig))
	// tenantConfigs are not a service, since they don't have any operational state.
	return nil, err
}

func (t *Loki) initDistributor() (services.Service, error) {
	var err error
	t.distributor, err = distributor.New(
		t.Cfg.Distributor,
		t.Cfg.IngesterClient,
		t.tenantConfigs,
		t.ring,
		t.Overrides,
		prometheus.DefaultRegisterer,
	)
	if err != nil {
		return nil, err
	}

	// Register the distributor to receive Push requests over GRPC
	// EXCEPT when running with `-target=all` or `-target=` contains `ingester`
	if !t.Cfg.isModuleEnabled(All) && !t.Cfg.isModuleEnabled(Write) && !t.Cfg.isModuleEnabled(Ingester) {
		logproto.RegisterPusherServer(t.Server.GRPC, t.distributor)
	}

	// If the querier module is not part of this process we need to check if multi-tenant queries are enabled.
	// If the querier module is part of this process the querier module will configure everything.
	if !t.Cfg.isModuleEnabled(Querier) && t.Cfg.Querier.MultiTenantQueriesEnabled {
		tenant.WithDefaultResolver(tenant.NewMultiResolver())
	}

	pushHandler := middleware.Merge(
		serverutil.RecoveryHTTPMiddleware,
		t.HTTPAuthMiddleware,
	).Wrap(http.HandlerFunc(t.distributor.PushHandler))

	t.Server.HTTP.Path("/distributor/ring").Methods("GET", "POST").Handler(t.distributor)

	if t.Cfg.InternalServer.Enable {
		t.InternalServer.HTTP.Path("/distributor/ring").Methods("GET").Handler(t.distributor)
	}

	t.Server.HTTP.Path("/api/prom/push").Methods("POST").Handler(pushHandler)
	t.Server.HTTP.Path("/loki/api/v1/push").Methods("POST").Handler(pushHandler)
	return t.distributor, nil
}

func (t *Loki) initQuerier() (services.Service, error) {
	if t.Cfg.Ingester.QueryStoreMaxLookBackPeriod != 0 {
		t.Cfg.Querier.IngesterQueryStoreMaxLookback = t.Cfg.Ingester.QueryStoreMaxLookBackPeriod
	}
	// Querier worker's max concurrent requests must be the same as the querier setting
	t.Cfg.Worker.MaxConcurrentRequests = t.Cfg.Querier.MaxConcurrent

	deleteStore, err := t.deleteRequestsClient("querier", t.Overrides)
	if err != nil {
		return nil, err
	}

	q, err := querier.New(t.Cfg.Querier, t.Store, t.ingesterQuerier, t.Overrides, deleteStore, prometheus.DefaultRegisterer)
	if err != nil {
		return nil, err
	}

	if t.Cfg.Querier.MultiTenantQueriesEnabled {
		t.Querier = querier.NewMultiTenantQuerier(q, util_log.Logger)
		tenant.WithDefaultResolver(tenant.NewMultiResolver())
	} else {
		t.Querier = q
	}

	querierWorkerServiceConfig := querier.WorkerServiceConfig{
		AllEnabled:            t.Cfg.isModuleEnabled(All),
		ReadEnabled:           t.Cfg.isModuleEnabled(Read),
		GrpcListenAddress:     t.Cfg.Server.GRPCListenAddress,
		GrpcListenPort:        t.Cfg.Server.GRPCListenPort,
		QuerierMaxConcurrent:  t.Cfg.Querier.MaxConcurrent,
		QuerierWorkerConfig:   &t.Cfg.Worker,
		QueryFrontendEnabled:  t.Cfg.isModuleEnabled(QueryFrontend),
		QuerySchedulerEnabled: t.Cfg.isModuleEnabled(QueryScheduler),
		SchedulerRing:         scheduler.SafeReadRing(t.queryScheduler),
	}

	toMerge := []middleware.Interface{
		httpreq.ExtractQueryMetricsMiddleware(),
	}
	if t.supportIndexDeleteRequest() && t.Cfg.CompactorConfig.RetentionEnabled {
		toMerge = append(
			toMerge,
			queryrangebase.CacheGenNumberHeaderSetterMiddleware(t.cacheGenerationLoader),
		)
	}

	logger := log.With(util_log.Logger, "component", "querier")
	t.querierAPI = querier.NewQuerierAPI(t.Cfg.Querier, t.Querier, t.Overrides, logger)

	labelsHTTPMiddleware := querier.WrapQuerySpanAndTimeout("query.Label", t.querierAPI)

	if t.Cfg.Querier.PerRequestLimitsEnabled {
		toMerge = append(
			toMerge,
			querylimits.NewQueryLimitsMiddleware(log.With(util_log.Logger, "component", "query-limits-middleware")),
		)
		labelsHTTPMiddleware = middleware.Merge(
			querylimits.NewQueryLimitsMiddleware(log.With(util_log.Logger, "component", "query-limits-middleware")),
			labelsHTTPMiddleware,
		)
	}

	httpMiddleware := middleware.Merge(toMerge...)

	queryHandlers := map[string]http.Handler{
		"/loki/api/v1/query_range": middleware.Merge(
			httpMiddleware,
			querier.WrapQuerySpanAndTimeout("query.RangeQuery", t.querierAPI),
		).Wrap(http.HandlerFunc(t.querierAPI.RangeQueryHandler)),

		"/loki/api/v1/query": middleware.Merge(
			httpMiddleware,
			querier.WrapQuerySpanAndTimeout("query.InstantQuery", t.querierAPI),
		).Wrap(http.HandlerFunc(t.querierAPI.InstantQueryHandler)),

		"/loki/api/v1/label":               labelsHTTPMiddleware.Wrap(http.HandlerFunc(t.querierAPI.LabelHandler)),
		"/loki/api/v1/labels":              labelsHTTPMiddleware.Wrap(http.HandlerFunc(t.querierAPI.LabelHandler)),
		"/loki/api/v1/label/{name}/values": labelsHTTPMiddleware.Wrap(http.HandlerFunc(t.querierAPI.LabelHandler)),

		"/loki/api/v1/series":      querier.WrapQuerySpanAndTimeout("query.Series", t.querierAPI).Wrap(http.HandlerFunc(t.querierAPI.SeriesHandler)),
		"/loki/api/v1/index/stats": querier.WrapQuerySpanAndTimeout("query.IndexStats", t.querierAPI).Wrap(http.HandlerFunc(t.querierAPI.IndexStatsHandler)),

		"/api/prom/query": middleware.Merge(
			httpMiddleware,
			querier.WrapQuerySpanAndTimeout("query.LogQuery", t.querierAPI),
		).Wrap(http.HandlerFunc(t.querierAPI.LogQueryHandler)),

		"/api/prom/label":               labelsHTTPMiddleware.Wrap(http.HandlerFunc(t.querierAPI.LabelHandler)),
		"/api/prom/label/{name}/values": labelsHTTPMiddleware.Wrap(http.HandlerFunc(t.querierAPI.LabelHandler)),
		"/api/prom/series":              querier.WrapQuerySpanAndTimeout("query.Series", t.querierAPI).Wrap(http.HandlerFunc(t.querierAPI.SeriesHandler)),
	}

	// We always want to register tail routes externally, tail requests are different from normal queries, they
	// are HTTP requests that get upgraded to websocket requests and need to be handled/kept open by the Queriers.
	// The frontend has code to proxy these requests, however when running in the same processes
	// (such as target=All or target=Read) we don't want the frontend to proxy and instead we want the Queriers
	// to directly register these routes.
	// In practice this means we always want the queriers to register the tail routes externally, when a querier
	// is standalone ALL routes are registered externally, and when it's in the same process as a frontend,
	// we disable the proxying of the tail routes in initQueryFrontend() and we still want these routes regiestered
	// on the external router.
	alwaysExternalHandlers := map[string]http.Handler{
		"/loki/api/v1/tail": http.HandlerFunc(t.querierAPI.TailHandler),
		"/api/prom/tail":    http.HandlerFunc(t.querierAPI.TailHandler),
	}

	svc, err := querier.InitWorkerService(
		querierWorkerServiceConfig,
		prometheus.DefaultRegisterer,
		queryHandlers,
		alwaysExternalHandlers,
		t.Server.HTTP,
		t.Server.HTTPServer.Handler,
		t.HTTPAuthMiddleware,
	)
	if err != nil {
		return nil, err
	}

	if svc != nil {
		svc.AddListener(deleteRequestsStoreListener(deleteStore))
	}
	return svc, nil
}

func (t *Loki) initIngester() (_ services.Service, err error) {
	t.Cfg.Ingester.LifecyclerConfig.ListenPort = t.Cfg.Server.GRPCListenPort

	t.Ingester, err = ingester.New(t.Cfg.Ingester, t.Cfg.IngesterClient, t.Store, t.Overrides, t.tenantConfigs, prometheus.DefaultRegisterer)
	if err != nil {
		return
	}

	if t.Cfg.Ingester.Wrapper != nil {
		t.Ingester = t.Cfg.Ingester.Wrapper.Wrap(t.Ingester)
	}

	logproto.RegisterPusherServer(t.Server.GRPC, t.Ingester)
	logproto.RegisterQuerierServer(t.Server.GRPC, t.Ingester)
	logproto.RegisterIngesterServer(t.Server.GRPC, t.Ingester)
	logproto.RegisterStreamDataServer(t.Server.GRPC, t.Ingester)

	httpMiddleware := middleware.Merge(
		serverutil.RecoveryHTTPMiddleware,
	)
	t.Server.HTTP.Methods("GET", "POST").Path("/flush").Handler(
		httpMiddleware.Wrap(http.HandlerFunc(t.Ingester.FlushHandler)),
	)
	t.Server.HTTP.Methods("POST").Path("/ingester/flush_shutdown").Handler(
		httpMiddleware.Wrap(http.HandlerFunc(t.Ingester.LegacyShutdownHandler)),
	)
	t.Server.HTTP.Methods("POST").Path("/ingester/prepare_shutdown").Handler(
		httpMiddleware.Wrap(http.HandlerFunc(t.Ingester.PrepareShutdown)),
	)
	t.Server.HTTP.Methods("POST").Path("/ingester/shutdown").Handler(
		httpMiddleware.Wrap(http.HandlerFunc(t.Ingester.ShutdownHandler)),
	)
	return t.Ingester, nil
}

func (t *Loki) initTableManager() (services.Service, error) {
	err := t.Cfg.SchemaConfig.Load()
	if err != nil {
		return nil, err
	}

	// Assume the newest config is the one to use
	lastConfig := &t.Cfg.SchemaConfig.Configs[len(t.Cfg.SchemaConfig.Configs)-1]

	if (t.Cfg.TableManager.ChunkTables.WriteScale.Enabled ||
		t.Cfg.TableManager.IndexTables.WriteScale.Enabled ||
		t.Cfg.TableManager.ChunkTables.InactiveWriteScale.Enabled ||
		t.Cfg.TableManager.IndexTables.InactiveWriteScale.Enabled ||
		t.Cfg.TableManager.ChunkTables.ReadScale.Enabled ||
		t.Cfg.TableManager.IndexTables.ReadScale.Enabled ||
		t.Cfg.TableManager.ChunkTables.InactiveReadScale.Enabled ||
		t.Cfg.TableManager.IndexTables.InactiveReadScale.Enabled) &&
		t.Cfg.StorageConfig.AWSStorageConfig.Metrics.URL == "" {
		level.Error(util_log.Logger).Log("msg", "WriteScale is enabled but no Metrics URL has been provided")
		os.Exit(1)
	}

	reg := prometheus.WrapRegistererWith(prometheus.Labels{"component": "table-manager-store"}, prometheus.DefaultRegisterer)

	tableClient, err := storage.NewTableClient(lastConfig.IndexType, t.Cfg.StorageConfig, t.clientMetrics, reg)
	if err != nil {
		return nil, err
	}

	bucketClient, err := storage.NewBucketClient(t.Cfg.StorageConfig)
	util_log.CheckFatal("initializing bucket client", err, util_log.Logger)

	t.tableManager, err = index.NewTableManager(t.Cfg.TableManager, t.Cfg.SchemaConfig, maxChunkAgeForTableManager, tableClient, bucketClient, nil, prometheus.DefaultRegisterer)
	if err != nil {
		return nil, err
	}

	return t.tableManager, nil
}

func (t *Loki) initStore() (_ services.Service, err error) {
	// Always set these configs
	t.Cfg.StorageConfig.BoltDBShipperConfig.IndexGatewayClientConfig.Mode = t.Cfg.IndexGateway.Mode
	t.Cfg.StorageConfig.TSDBShipperConfig.IndexGatewayClientConfig.Mode = t.Cfg.IndexGateway.Mode
	if t.Cfg.IndexGateway.Mode == indexgateway.RingMode {
		t.Cfg.StorageConfig.BoltDBShipperConfig.IndexGatewayClientConfig.Ring = t.indexGatewayRingManager.Ring
		t.Cfg.StorageConfig.TSDBShipperConfig.IndexGatewayClientConfig.Ring = t.indexGatewayRingManager.Ring
	}

	// If RF > 1 and current or upcoming index type is boltdb-shipper then disable index dedupe and write dedupe cache.
	// This is to ensure that index entries are replicated to all the boltdb files in ingesters flushing replicated data.
	if t.Cfg.Ingester.LifecyclerConfig.RingConfig.ReplicationFactor > 1 && config.UsingObjectStorageIndex(t.Cfg.SchemaConfig.Configs) {
		t.Cfg.ChunkStoreConfig.DisableIndexDeduplication = true
		t.Cfg.ChunkStoreConfig.WriteDedupeCacheConfig = cache.Config{}
	}

	// Set configs pertaining to object storage based indices
	if config.UsingObjectStorageIndex(t.Cfg.SchemaConfig.Configs) {
		t.Cfg.StorageConfig.BoltDBShipperConfig.IngesterName = t.Cfg.Ingester.LifecyclerConfig.ID
		t.Cfg.StorageConfig.TSDBShipperConfig.IngesterName = t.Cfg.Ingester.LifecyclerConfig.ID

		switch true {
		case t.Cfg.isModuleEnabled(Ingester), t.Cfg.isModuleEnabled(Write):
			// Use fifo cache for caching index in memory, this also significantly helps performance.
			t.Cfg.StorageConfig.IndexQueriesCacheConfig = cache.Config{
				EmbeddedCache: cache.EmbeddedCacheConfig{
					Enabled:   true,
					MaxSizeMB: 200,
					// This is a small hack to save some CPU cycles.
					// We check if the object is still valid after pulling it from cache using the IndexCacheValidity value
					// however it has to be deserialized to do so, setting the cache validity to some arbitrary amount less than the
					// IndexCacheValidity guarantees the FIFO cache will expire the object first which can be done without
					// having to deserialize the object.
					TTL: t.Cfg.StorageConfig.IndexCacheValidity - 1*time.Minute,
				},
			}
			// Force the retain period to be longer than the IndexCacheValidity used in the store, this guarantees we don't
			// have query gaps on chunks flushed after an index entry is cached by keeping them retained in the ingester
			// and queried as part of live data until the cache TTL expires on the index entry.
			t.Cfg.Ingester.RetainPeriod = t.Cfg.StorageConfig.IndexCacheValidity + 1*time.Minute

			// We do not want ingester to unnecessarily keep downloading files
			t.Cfg.StorageConfig.BoltDBShipperConfig.Mode = indexshipper.ModeWriteOnly
			t.Cfg.StorageConfig.BoltDBShipperConfig.IngesterDBRetainPeriod = shipperQuerierIndexUpdateDelay(t.Cfg.StorageConfig.IndexCacheValidity, t.Cfg.StorageConfig.BoltDBShipperConfig.ResyncInterval)

			t.Cfg.StorageConfig.TSDBShipperConfig.Mode = indexshipper.ModeWriteOnly
			t.Cfg.StorageConfig.TSDBShipperConfig.IngesterDBRetainPeriod = shipperQuerierIndexUpdateDelay(t.Cfg.StorageConfig.IndexCacheValidity, t.Cfg.StorageConfig.TSDBShipperConfig.ResyncInterval)

		case t.Cfg.isModuleEnabled(Querier), t.Cfg.isModuleEnabled(Ruler), t.Cfg.isModuleEnabled(Read), t.Cfg.isModuleEnabled(Backend), t.isModuleActive(IndexGateway):
			// We do not want query to do any updates to index
			t.Cfg.StorageConfig.BoltDBShipperConfig.Mode = indexshipper.ModeReadOnly
			t.Cfg.StorageConfig.TSDBShipperConfig.Mode = indexshipper.ModeReadOnly
		default:
			t.Cfg.StorageConfig.BoltDBShipperConfig.Mode = indexshipper.ModeReadWrite
			t.Cfg.StorageConfig.BoltDBShipperConfig.IngesterDBRetainPeriod = shipperQuerierIndexUpdateDelay(t.Cfg.StorageConfig.IndexCacheValidity, t.Cfg.StorageConfig.BoltDBShipperConfig.ResyncInterval)
			t.Cfg.StorageConfig.TSDBShipperConfig.Mode = indexshipper.ModeReadWrite
			t.Cfg.StorageConfig.TSDBShipperConfig.IngesterDBRetainPeriod = shipperQuerierIndexUpdateDelay(t.Cfg.StorageConfig.IndexCacheValidity, t.Cfg.StorageConfig.TSDBShipperConfig.ResyncInterval)

		}
	}

	if config.UsingObjectStorageIndex(t.Cfg.SchemaConfig.Configs) {
		var asyncStore bool

		shipperConfigIdx := config.ActivePeriodConfig(t.Cfg.SchemaConfig.Configs)
		iTy := t.Cfg.SchemaConfig.Configs[shipperConfigIdx].IndexType
		if iTy != config.BoltDBShipperType && iTy != config.TSDBType {
			shipperConfigIdx++
		}

		// TODO(owen-d): make helper more agnostic between boltdb|tsdb
		var resyncInterval time.Duration
		switch t.Cfg.SchemaConfig.Configs[shipperConfigIdx].IndexType {
		case config.BoltDBShipperType:
			resyncInterval = t.Cfg.StorageConfig.BoltDBShipperConfig.ResyncInterval
		case config.TSDBType:
			resyncInterval = t.Cfg.StorageConfig.TSDBShipperConfig.ResyncInterval
		}

		minIngesterQueryStoreDuration := shipperMinIngesterQueryStoreDuration(
			t.Cfg.Ingester.MaxChunkAge,
			shipperQuerierIndexUpdateDelay(
				t.Cfg.StorageConfig.IndexCacheValidity,
				resyncInterval,
			),
		)

		switch true {
		case t.Cfg.isModuleEnabled(Querier), t.Cfg.isModuleEnabled(Ruler), t.Cfg.isModuleEnabled(Read):
			// Do not use the AsyncStore if the querier is configured with QueryStoreOnly set to true
			if t.Cfg.Querier.QueryStoreOnly {
				break
			}
			// Use AsyncStore to query both ingesters local store and chunk store for store queries.
			// Only queriers should use the AsyncStore, it should never be used in ingesters.
			asyncStore = true

			// The legacy Read target includes the index gateway, so disable the index-gateway client in that configuration.
			if t.Cfg.LegacyReadTarget && t.Cfg.isModuleEnabled(Read) {
				t.Cfg.StorageConfig.BoltDBShipperConfig.IndexGatewayClientConfig.Disabled = true
				t.Cfg.StorageConfig.TSDBShipperConfig.IndexGatewayClientConfig.Disabled = true
			}
			// Backend target includes the index gateway
		case t.Cfg.isModuleEnabled(IndexGateway), t.Cfg.isModuleEnabled(Backend):
			// we want to use the actual storage when running the index-gateway, so we remove the Addr from the config
			t.Cfg.StorageConfig.BoltDBShipperConfig.IndexGatewayClientConfig.Disabled = true
			t.Cfg.StorageConfig.TSDBShipperConfig.IndexGatewayClientConfig.Disabled = true
		case t.Cfg.isModuleEnabled(All):
			// We want ingester to also query the store when using boltdb-shipper but only when running with target All.
			// We do not want to use AsyncStore otherwise it would start spiraling around doing queries over and over again to the ingesters and store.
			// ToDo: See if we can avoid doing this when not running loki in clustered mode.
			t.Cfg.Ingester.QueryStore = true

			mlb, err := calculateMaxLookBack(
				t.Cfg.SchemaConfig.Configs[shipperConfigIdx],
				t.Cfg.Ingester.QueryStoreMaxLookBackPeriod,
				minIngesterQueryStoreDuration,
			)
			if err != nil {
				return nil, err
			}
			t.Cfg.Ingester.QueryStoreMaxLookBackPeriod = mlb
		}

		if asyncStore {
			t.Cfg.StorageConfig.EnableAsyncStore = true
			t.Cfg.StorageConfig.AsyncStoreConfig = storage.AsyncStoreCfg{
				IngesterQuerier: t.ingesterQuerier,
				QueryIngestersWithin: calculateAsyncStoreQueryIngestersWithin(
					t.Cfg.Querier.QueryIngestersWithin,
					minIngesterQueryStoreDuration,
				),
			}
		}
	}

	t.Store, err = storage.NewStore(t.Cfg.StorageConfig, t.Cfg.ChunkStoreConfig, t.Cfg.SchemaConfig, t.Overrides, t.clientMetrics, prometheus.DefaultRegisterer, util_log.Logger)
	if err != nil {
		return
	}

	return services.NewIdleService(nil, func(_ error) error {
		t.Store.Stop()
		return nil
	}), nil
}

func (t *Loki) initIngesterQuerier() (_ services.Service, err error) {
	t.ingesterQuerier, err = querier.NewIngesterQuerier(t.Cfg.IngesterClient, t.ring, t.Cfg.Querier.ExtraQueryDelay)
	if err != nil {
		return nil, err
	}

	return services.NewIdleService(nil, nil), nil
}

// Placeholder limits type to pass to cortex frontend
type disabledShuffleShardingLimits struct{}

func (disabledShuffleShardingLimits) MaxQueriersPerUser(userID string) int { return 0 }

func (t *Loki) initQueryFrontendTripperware() (_ services.Service, err error) {
	level.Debug(util_log.Logger).Log("msg", "initializing query frontend tripperware")

	tripperware, stopper, err := queryrange.NewTripperware(
		t.Cfg.QueryRange,
		util_log.Logger,
		t.Overrides,
		t.Cfg.SchemaConfig,
		t.cacheGenerationLoader, t.Cfg.CompactorConfig.RetentionEnabled,
		prometheus.DefaultRegisterer,
	)
	if err != nil {
		return
	}
	t.stopper = stopper
	t.QueryFrontEndTripperware = tripperware

	return services.NewIdleService(nil, nil), nil
}

func (t *Loki) initCacheGenerationLoader() (_ services.Service, err error) {
	var client generationnumber.CacheGenClient
	if t.supportIndexDeleteRequest() {
		compactorAddress, isGRPCAddress, err := t.compactorAddress()
		if err != nil {
			return nil, err
		}

		reg := prometheus.WrapRegistererWith(prometheus.Labels{"for": "cache_gen", "client_type": t.Cfg.Target.String()}, prometheus.DefaultRegisterer)
		if isGRPCAddress {
			client, err = compactor_client.NewGRPCClient(compactorAddress, t.Cfg.CompactorGRPCClient, reg)
			if err != nil {
				return nil, err
			}
		} else {
			client, err = compactor_client.NewHTTPClient(compactorAddress, t.Cfg.CompactorHTTPClient)
			if err != nil {
				return nil, err
			}
		}
	}

	t.cacheGenerationLoader = generationnumber.NewGenNumberLoader(client, prometheus.DefaultRegisterer)
	return services.NewIdleService(nil, func(failureCase error) error {
		t.cacheGenerationLoader.Stop()
		return nil
	}), nil
}

func (t *Loki) supportIndexDeleteRequest() bool {
	return config.UsingObjectStorageIndex(t.Cfg.SchemaConfig.Configs)
}

// compactorAddress returns the configured address of the compactor.
// It prefers grpc address over http. If the address is grpc then the bool would be true otherwise false
func (t *Loki) compactorAddress() (string, bool, error) {
	legacyReadMode := t.Cfg.LegacyReadTarget && t.Cfg.isModuleEnabled(Read)
	if t.Cfg.isModuleEnabled(All) || legacyReadMode || t.Cfg.isModuleEnabled(Backend) {
		// In single binary or read modes, this module depends on Server
		return fmt.Sprintf("%s:%d", t.Cfg.Server.GRPCListenAddress, t.Cfg.Server.GRPCListenPort), true, nil
	}

	if t.Cfg.Common.CompactorAddress == "" && t.Cfg.Common.CompactorGRPCAddress == "" {
		return "", false, errors.New("query filtering for deletes requires 'compactor_grpc_address' or 'compactor_address' to be configured")
	}

	if t.Cfg.Common.CompactorGRPCAddress != "" {
		return t.Cfg.Common.CompactorGRPCAddress, true, nil
	}

	return t.Cfg.Common.CompactorAddress, false, nil
}

func (t *Loki) initQueryFrontend() (_ services.Service, err error) {
	level.Debug(util_log.Logger).Log("msg", "initializing query frontend", "config", fmt.Sprintf("%+v", t.Cfg.Frontend))

	combinedCfg := frontend.CombinedFrontendConfig{
		Handler:       t.Cfg.Frontend.Handler,
		FrontendV1:    t.Cfg.Frontend.FrontendV1,
		FrontendV2:    t.Cfg.Frontend.FrontendV2,
		DownstreamURL: t.Cfg.Frontend.DownstreamURL,
	}
	roundTripper, frontendV1, frontendV2, err := frontend.InitFrontend(
		combinedCfg,
		scheduler.SafeReadRing(t.queryScheduler),
		disabledShuffleShardingLimits{},
		t.Cfg.Server.GRPCListenPort,
		util_log.Logger,
		prometheus.DefaultRegisterer)
	if err != nil {
		return nil, err
	}

	if frontendV1 != nil {
		frontendv1pb.RegisterFrontendServer(t.Server.GRPC, frontendV1)
		t.frontend = frontendV1
		level.Debug(util_log.Logger).Log("msg", "using query frontend", "version", "v1")
	} else if frontendV2 != nil {
		frontendv2pb.RegisterFrontendForQuerierServer(t.Server.GRPC, frontendV2)
		t.frontend = frontendV2
		level.Debug(util_log.Logger).Log("msg", "using query frontend", "version", "v2")
	} else {
		level.Debug(util_log.Logger).Log("msg", "no query frontend configured")
	}

	roundTripper = t.QueryFrontEndTripperware(roundTripper)

	frontendHandler := transport.NewHandler(t.Cfg.Frontend.Handler, roundTripper, util_log.Logger, prometheus.DefaultRegisterer)
	if t.Cfg.Frontend.CompressResponses {
		frontendHandler = gziphandler.GzipHandler(frontendHandler)
	}

	toMerge := []middleware.Interface{
		httpreq.ExtractQueryTagsMiddleware(),
		httpreq.PropagateHeadersMiddleware(httpreq.LokiActorPathHeader),
		serverutil.RecoveryHTTPMiddleware,
		t.HTTPAuthMiddleware,
		queryrange.StatsHTTPMiddleware,
		serverutil.NewPrepopulateMiddleware(),
		serverutil.ResponseJSONMiddleware(),
	}

	if t.Cfg.Querier.PerRequestLimitsEnabled {
		logger := log.With(util_log.Logger, "component", "query-limiter-middleware")
		toMerge = append(toMerge, querylimits.NewQueryLimitsMiddleware(logger))
	}

	frontendHandler = middleware.Merge(toMerge...).Wrap(frontendHandler)

	var defaultHandler http.Handler
	// If this process also acts as a Querier we don't do any proxying of tail requests
	if t.Cfg.Frontend.TailProxyURL != "" && !t.isModuleActive(Querier) {
		httpMiddleware := middleware.Merge(
			httpreq.ExtractQueryTagsMiddleware(),
			t.HTTPAuthMiddleware,
			queryrange.StatsHTTPMiddleware,
		)
		tailURL, err := url.Parse(t.Cfg.Frontend.TailProxyURL)
		if err != nil {
			return nil, err
		}
		tp := httputil.NewSingleHostReverseProxy(tailURL)

		cfg, err := t.Cfg.Frontend.TLS.GetTLSConfig()
		if err != nil {
			return nil, err
		}

		tp.Transport = &http.Transport{
			TLSClientConfig: cfg,
		}

		director := tp.Director
		tp.Director = func(req *http.Request) {
			director(req)
			req.Host = tailURL.Host
		}

		defaultHandler = httpMiddleware.Wrap(tp)
	} else {
		defaultHandler = frontendHandler
	}
	t.Server.HTTP.Path("/loki/api/v1/query_range").Methods("GET", "POST").Handler(frontendHandler)
	t.Server.HTTP.Path("/loki/api/v1/query").Methods("GET", "POST").Handler(frontendHandler)
	t.Server.HTTP.Path("/loki/api/v1/label").Methods("GET", "POST").Handler(frontendHandler)
	t.Server.HTTP.Path("/loki/api/v1/labels").Methods("GET", "POST").Handler(frontendHandler)
	t.Server.HTTP.Path("/loki/api/v1/label/{name}/values").Methods("GET", "POST").Handler(frontendHandler)
	t.Server.HTTP.Path("/loki/api/v1/series").Methods("GET", "POST").Handler(frontendHandler)
	t.Server.HTTP.Path("/loki/api/v1/index/stats").Methods("GET", "POST").Handler(frontendHandler)
	t.Server.HTTP.Path("/api/prom/query").Methods("GET", "POST").Handler(frontendHandler)
	t.Server.HTTP.Path("/api/prom/label").Methods("GET", "POST").Handler(frontendHandler)
	t.Server.HTTP.Path("/api/prom/label/{name}/values").Methods("GET", "POST").Handler(frontendHandler)
	t.Server.HTTP.Path("/api/prom/series").Methods("GET", "POST").Handler(frontendHandler)

	// Only register tailing requests if this process does not act as a Querier
	// If this process is also a Querier the Querier will register the tail endpoints.
	if !t.isModuleActive(Querier) {
		// defer tail endpoints to the default handler
		t.Server.HTTP.Path("/loki/api/v1/tail").Methods("GET", "POST").Handler(defaultHandler)
		t.Server.HTTP.Path("/api/prom/tail").Methods("GET", "POST").Handler(defaultHandler)
	}

	if t.frontend == nil {
		return services.NewIdleService(nil, func(_ error) error {
			if t.stopper != nil {
				t.stopper.Stop()
				t.stopper = nil
			}
			return nil
		}), nil
	}

	return services.NewIdleService(func(ctx context.Context) error {
		return services.StartAndAwaitRunning(ctx, t.frontend)
	}, func(_ error) error {
		// Log but not return in case of error, so that other following dependencies
		// are stopped too.
		if err := services.StopAndAwaitTerminated(context.Background(), t.frontend); err != nil {
			level.Warn(util_log.Logger).Log("msg", "failed to stop frontend service", "err", err)
		}

		if t.stopper != nil {
			t.stopper.Stop()
		}
		return nil
	}), nil
}

func (t *Loki) initRulerStorage() (_ services.Service, err error) {
	// if the ruler is not configured and we're in single binary then let's just log an error and continue.
	// unfortunately there is no way to generate a "default" config and compare default against actual
	// to determine if it's unconfigured.  the following check, however, correctly tests this.
	// Single binary integration tests will break if this ever drifts
	legacyReadMode := t.Cfg.LegacyReadTarget && t.Cfg.isModuleEnabled(Read)
	if (t.Cfg.isModuleEnabled(All) || legacyReadMode || t.Cfg.isModuleEnabled(Backend)) && t.Cfg.Ruler.StoreConfig.IsDefaults() {
		level.Info(util_log.Logger).Log("msg", "Ruler storage is not configured; ruler will not be started.")
		return
	}

	// Make sure storage directory exists if using filesystem store
	if t.Cfg.Ruler.StoreConfig.Type == "local" && t.Cfg.Ruler.StoreConfig.Local.Directory != "" {
		err := chunk_util.EnsureDirectory(t.Cfg.Ruler.StoreConfig.Local.Directory)
		if err != nil {
			return nil, err
		}
	}

	t.RulerStorage, err = base_ruler.NewLegacyRuleStore(t.Cfg.Ruler.StoreConfig, t.Cfg.StorageConfig.Hedging, t.clientMetrics, ruler.GroupLoader{}, util_log.Logger)

	return
}

func (t *Loki) initRuler() (_ services.Service, err error) {
	if t.RulerStorage == nil {
		level.Warn(util_log.Logger).Log("msg", "RulerStorage is nil. Not starting the ruler.")
		return nil, nil
	}

	if t.ruleEvaluator == nil {
		level.Warn(util_log.Logger).Log("msg", "RuleEvaluator is nil. Not starting the ruler.") // TODO better error msg
		return nil, nil
	}

	t.Cfg.Ruler.Ring.ListenPort = t.Cfg.Server.GRPCListenPort

	t.ruler, err = ruler.NewRuler(
		t.Cfg.Ruler,
		t.ruleEvaluator,
		prometheus.DefaultRegisterer,
		util_log.Logger,
		t.RulerStorage,
		t.Overrides,
	)

	if err != nil {
		return
	}

	t.rulerAPI = base_ruler.NewAPI(t.ruler, t.RulerStorage, util_log.Logger)

	// Expose HTTP endpoints.
	if t.Cfg.Ruler.EnableAPI {
		t.Server.HTTP.Path("/ruler/ring").Methods("GET", "POST").Handler(t.ruler)

		if t.Cfg.InternalServer.Enable {
			t.InternalServer.HTTP.Path("/ruler/ring").Methods("GET").Handler(t.ruler)
		}

		base_ruler.RegisterRulerServer(t.Server.GRPC, t.ruler)

		// Prometheus Rule API Routes
		t.Server.HTTP.Path("/prometheus/api/v1/rules").Methods("GET").Handler(t.HTTPAuthMiddleware.Wrap(http.HandlerFunc(t.rulerAPI.PrometheusRules)))
		t.Server.HTTP.Path("/prometheus/api/v1/alerts").Methods("GET").Handler(t.HTTPAuthMiddleware.Wrap(http.HandlerFunc(t.rulerAPI.PrometheusAlerts)))

		// Ruler Legacy API Routes
		t.Server.HTTP.Path("/api/prom/rules").Methods("GET").Handler(t.HTTPAuthMiddleware.Wrap(http.HandlerFunc(t.rulerAPI.ListRules)))
		t.Server.HTTP.Path("/api/prom/rules/{namespace}").Methods("GET").Handler(t.HTTPAuthMiddleware.Wrap(http.HandlerFunc(t.rulerAPI.ListRules)))
		t.Server.HTTP.Path("/api/prom/rules/{namespace}").Methods("POST").Handler(t.HTTPAuthMiddleware.Wrap(http.HandlerFunc(t.rulerAPI.CreateRuleGroup)))
		t.Server.HTTP.Path("/api/prom/rules/{namespace}").Methods("DELETE").Handler(t.HTTPAuthMiddleware.Wrap(http.HandlerFunc(t.rulerAPI.DeleteNamespace)))
		t.Server.HTTP.Path("/api/prom/rules/{namespace}/{groupName}").Methods("GET").Handler(t.HTTPAuthMiddleware.Wrap(http.HandlerFunc(t.rulerAPI.GetRuleGroup)))
		t.Server.HTTP.Path("/api/prom/rules/{namespace}/{groupName}").Methods("DELETE").Handler(t.HTTPAuthMiddleware.Wrap(http.HandlerFunc(t.rulerAPI.DeleteRuleGroup)))

		// Ruler API Routes
		t.Server.HTTP.Path("/loki/api/v1/rules").Methods("GET").Handler(t.HTTPAuthMiddleware.Wrap(http.HandlerFunc(t.rulerAPI.ListRules)))
		t.Server.HTTP.Path("/loki/api/v1/rules/{namespace}").Methods("GET").Handler(t.HTTPAuthMiddleware.Wrap(http.HandlerFunc(t.rulerAPI.ListRules)))
		t.Server.HTTP.Path("/loki/api/v1/rules/{namespace}").Methods("POST").Handler(t.HTTPAuthMiddleware.Wrap(http.HandlerFunc(t.rulerAPI.CreateRuleGroup)))
		t.Server.HTTP.Path("/loki/api/v1/rules/{namespace}").Methods("DELETE").Handler(t.HTTPAuthMiddleware.Wrap(http.HandlerFunc(t.rulerAPI.DeleteNamespace)))
		t.Server.HTTP.Path("/loki/api/v1/rules/{namespace}/{groupName}").Methods("GET").Handler(t.HTTPAuthMiddleware.Wrap(http.HandlerFunc(t.rulerAPI.GetRuleGroup)))
		t.Server.HTTP.Path("/loki/api/v1/rules/{namespace}/{groupName}").Methods("DELETE").Handler(t.HTTPAuthMiddleware.Wrap(http.HandlerFunc(t.rulerAPI.DeleteRuleGroup)))
	}

	deleteStore, err := t.deleteRequestsClient("ruler", t.Overrides)
	if err != nil {
		return nil, err
	}
	t.ruler.AddListener(deleteRequestsStoreListener(deleteStore))

	return t.ruler, nil
}

func (t *Loki) initRuleEvaluator() (services.Service, error) {
	if err := t.Cfg.Ruler.Evaluation.Validate(); err != nil {
		return nil, fmt.Errorf("invalid ruler evaluation config: %w", err)
	}

	var (
		evaluator ruler.Evaluator
		err       error
	)

	mode := t.Cfg.Ruler.Evaluation.Mode
	logger := log.With(util_log.Logger, "component", "ruler", "evaluation_mode", mode)

	switch mode {
	case ruler.EvalModeLocal:
		var engine *logql.Engine

		engine, err = t.createRulerQueryEngine(logger)
		if err != nil {
			break
		}

		evaluator, err = ruler.NewLocalEvaluator(engine, logger)
	case ruler.EvalModeRemote:
		qfClient, e := ruler.DialQueryFrontend(&t.Cfg.Ruler.Evaluation.QueryFrontend)
		if e != nil {
			return nil, fmt.Errorf("failed to dial query frontend for remote rule evaluation: %w", err)
		}

		evaluator, err = ruler.NewRemoteEvaluator(qfClient, t.Overrides, logger, prometheus.DefaultRegisterer)
	default:
		err = fmt.Errorf("unknown rule evaluation mode %q", mode)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create %s rule evaluator: %w", mode, err)
	}

	t.ruleEvaluator = ruler.NewEvaluatorWithJitter(evaluator, t.Cfg.Ruler.Evaluation.MaxJitter, fnv.New32a(), logger)

	return nil, nil
}

func (t *Loki) initMemberlistKV() (services.Service, error) {
	reg := prometheus.DefaultRegisterer

	t.Cfg.MemberlistKV.MetricsNamespace = "loki"
	t.Cfg.MemberlistKV.MetricsRegisterer = reg
	t.Cfg.MemberlistKV.Codecs = []codec.Codec{
		ring.GetCodec(),
		usagestats.JSONCodec,
	}

	dnsProviderReg := prometheus.WrapRegistererWithPrefix(
		"cortex_",
		prometheus.WrapRegistererWith(
			prometheus.Labels{"name": "memberlist"},
			reg,
		),
	)
	dnsProvider := dns.NewProvider(util_log.Logger, dnsProviderReg, dns.GolangResolverType)

	t.MemberlistKV = memberlist.NewKVInitService(&t.Cfg.MemberlistKV, util_log.Logger, dnsProvider, reg)

	t.Cfg.CompactorConfig.CompactorRing.KVStore.MemberlistKV = t.MemberlistKV.GetMemberlistKV
	t.Cfg.Distributor.DistributorRing.KVStore.MemberlistKV = t.MemberlistKV.GetMemberlistKV
	t.Cfg.IndexGateway.Ring.KVStore.MemberlistKV = t.MemberlistKV.GetMemberlistKV
	t.Cfg.Ingester.LifecyclerConfig.RingConfig.KVStore.MemberlistKV = t.MemberlistKV.GetMemberlistKV
	t.Cfg.QueryScheduler.SchedulerRing.KVStore.MemberlistKV = t.MemberlistKV.GetMemberlistKV
	t.Cfg.Ruler.Ring.KVStore.MemberlistKV = t.MemberlistKV.GetMemberlistKV

	t.Server.HTTP.Handle("/memberlist", t.MemberlistKV)

	if t.Cfg.InternalServer.Enable {
		t.InternalServer.HTTP.Path("/memberlist").Methods("GET").Handler(t.MemberlistKV)
	}

	return t.MemberlistKV, nil
}

func (t *Loki) initCompactor() (services.Service, error) {
	// Set some config sections from other config sections in the config struct
	t.Cfg.CompactorConfig.CompactorRing.ListenPort = t.Cfg.Server.GRPCListenPort

	if !config.UsingObjectStorageIndex(t.Cfg.SchemaConfig.Configs) {
		level.Info(util_log.Logger).Log("msg", "Not using object storage index, not starting compactor")
		return nil, nil
	}

	err := t.Cfg.SchemaConfig.Load()
	if err != nil {
		return nil, err
	}

	objectClient, err := storage.NewObjectClient(t.Cfg.CompactorConfig.SharedStoreType, t.Cfg.StorageConfig, t.clientMetrics)
	if err != nil {
		return nil, err
	}

	t.compactor, err = compactor.NewCompactor(t.Cfg.CompactorConfig, objectClient, t.Cfg.SchemaConfig, t.Overrides, prometheus.DefaultRegisterer)
	if err != nil {
		return nil, err
	}

	t.compactor.RegisterIndexCompactor(config.BoltDBShipperType, boltdb_shipper_compactor.NewIndexCompactor())
	t.compactor.RegisterIndexCompactor(config.TSDBType, tsdb.NewIndexCompactor())
	t.Server.HTTP.Path("/compactor/ring").Methods("GET", "POST").Handler(t.compactor)

	if t.Cfg.InternalServer.Enable {
		t.InternalServer.HTTP.Path("/compactor/ring").Methods("GET").Handler(t.compactor)
	}

	if t.Cfg.CompactorConfig.RetentionEnabled {
		t.Server.HTTP.Path("/loki/api/v1/delete").Methods("PUT", "POST").Handler(t.addCompactorMiddleware(t.compactor.DeleteRequestsHandler.AddDeleteRequestHandler))
		t.Server.HTTP.Path("/loki/api/v1/delete").Methods("GET").Handler(t.addCompactorMiddleware(t.compactor.DeleteRequestsHandler.GetAllDeleteRequestsHandler))
		t.Server.HTTP.Path("/loki/api/v1/delete").Methods("DELETE").Handler(t.addCompactorMiddleware(t.compactor.DeleteRequestsHandler.CancelDeleteRequestHandler))
		t.Server.HTTP.Path("/loki/api/v1/cache/generation_numbers").Methods("GET").Handler(t.addCompactorMiddleware(t.compactor.DeleteRequestsHandler.GetCacheGenerationNumberHandler))
		grpc.RegisterCompactorServer(t.Server.GRPC, t.compactor.DeleteRequestsGRPCHandler)
	}

	return t.compactor, nil
}

func (t *Loki) addCompactorMiddleware(h http.HandlerFunc) http.Handler {
	return t.HTTPAuthMiddleware.Wrap(deletion.TenantMiddleware(t.Overrides, h))
}

func (t *Loki) initIndexGateway() (services.Service, error) {
	t.Cfg.IndexGateway.Ring.ListenPort = t.Cfg.Server.GRPCListenPort

	var indexClient indexgateway.IndexClient
	if schemaHasBoltDBShipperConfig(t.Cfg.SchemaConfig) {
		var err error
		indexClient, err = storage.NewIndexClient(config.BoltDBShipperType, t.Cfg.StorageConfig, t.Cfg.SchemaConfig, t.Overrides, t.clientMetrics, t.indexGatewayRingManager.IndexGatewayOwnsTenant, prometheus.DefaultRegisterer)
		if err != nil {
			return nil, err
		}
	}
	gateway, err := indexgateway.NewIndexGateway(t.Cfg.IndexGateway, util_log.Logger, prometheus.DefaultRegisterer, t.Store, indexClient)
	if err != nil {
		return nil, err
	}

	logproto.RegisterIndexGatewayServer(t.Server.GRPC, gateway)
	return gateway, nil
}

func (t *Loki) initIndexGatewayRing() (_ services.Service, err error) {
	// IndexGateway runs by default on legacy read and backend targets, and should always assume
	// ring mode when run in this way.
	legacyReadMode := t.Cfg.LegacyReadTarget && t.isModuleActive(Read)
	if legacyReadMode || t.isModuleActive(Backend) {
		t.Cfg.IndexGateway.Mode = indexgateway.RingMode
	}

	if t.Cfg.IndexGateway.Mode != indexgateway.RingMode {
		return
	}

	t.Cfg.StorageConfig.BoltDBShipperConfig.Mode = indexshipper.ModeReadOnly
	t.Cfg.StorageConfig.TSDBShipperConfig.Mode = indexshipper.ModeReadOnly
	t.Cfg.IndexGateway.Ring.ListenPort = t.Cfg.Server.GRPCListenPort

	managerMode := indexgateway.ClientMode
	if t.Cfg.isModuleEnabled(IndexGateway) || legacyReadMode || t.Cfg.isModuleEnabled(Backend) {
		managerMode = indexgateway.ServerMode
	}
	rm, err := indexgateway.NewRingManager(managerMode, t.Cfg.IndexGateway, util_log.Logger, prometheus.DefaultRegisterer)

	if err != nil {
		return nil, gerrors.Wrap(err, "new index gateway ring manager")
	}

	t.indexGatewayRingManager = rm

	t.Server.HTTP.Path("/indexgateway/ring").Methods("GET", "POST").Handler(t.indexGatewayRingManager)

	if t.Cfg.InternalServer.Enable {
		t.InternalServer.HTTP.Path("/indexgateway/ring").Methods("GET").Handler(t.indexGatewayRingManager)
	}

	return t.indexGatewayRingManager, nil
}

func (t *Loki) initQueryScheduler() (services.Service, error) {
	// Set some config sections from other config sections in the config struct
	t.Cfg.QueryScheduler.SchedulerRing.ListenPort = t.Cfg.Server.GRPCListenPort

	s, err := scheduler.NewScheduler(t.Cfg.QueryScheduler, t.Overrides, util_log.Logger, prometheus.DefaultRegisterer)
	if err != nil {
		return nil, err
	}

	schedulerpb.RegisterSchedulerForFrontendServer(t.Server.GRPC, s)
	schedulerpb.RegisterSchedulerForQuerierServer(t.Server.GRPC, s)
	t.Server.HTTP.Path("/scheduler/ring").Methods("GET", "POST").Handler(s)

	if t.Cfg.InternalServer.Enable {
		t.InternalServer.HTTP.Path("/scheduler/ring").Methods("GET").Handler(s)
	}

	t.queryScheduler = s
	return s, nil
}

func (t *Loki) initQueryLimiter() (services.Service, error) {
	_ = level.Debug(util_log.Logger).Log("msg", "initializing query limiter")
	logger := log.With(util_log.Logger, "component", "query-limiter")
	t.Overrides = querylimits.NewLimiter(logger, t.Overrides)
	return nil, nil
}

func (t *Loki) initQueryLimitsInterceptors() (services.Service, error) {
	_ = level.Debug(util_log.Logger).Log("msg", "initializing query limits interceptors")
	t.Cfg.Server.GRPCMiddleware = append(t.Cfg.Server.GRPCMiddleware, querylimits.ServerQueryLimitsInterceptor)
	t.Cfg.Server.GRPCStreamMiddleware = append(t.Cfg.Server.GRPCStreamMiddleware, querylimits.StreamServerQueryLimitsInterceptor)

	return nil, nil
}

func (t *Loki) initQueryLimitsTripperware() (services.Service, error) {
	_ = level.Debug(util_log.Logger).Log("msg", "initializing query limits tripperware")
	t.QueryFrontEndTripperware = querylimits.WrapTripperware(
		t.QueryFrontEndTripperware,
	)

	return nil, nil
}

func (t *Loki) initUsageReport() (services.Service, error) {
	if !t.Cfg.UsageReport.Enabled {
		return nil, nil
	}
	t.Cfg.UsageReport.Leader = false
	if t.isModuleActive(Ingester) {
		t.Cfg.UsageReport.Leader = true
	}

	usagestats.Target(t.Cfg.Target.String())
	period, err := t.Cfg.SchemaConfig.SchemaForTime(model.Now())
	if err != nil {
		return nil, err
	}

	objectClient, err := storage.NewObjectClient(period.ObjectType, t.Cfg.StorageConfig, t.clientMetrics)
	if err != nil {
		level.Info(util_log.Logger).Log("msg", "failed to initialize usage report", "err", err)
		return nil, nil
	}
	ur, err := usagestats.NewReporter(t.Cfg.UsageReport, t.Cfg.Ingester.LifecyclerConfig.RingConfig.KVStore, objectClient, util_log.Logger, prometheus.DefaultRegisterer)
	if err != nil {
		level.Info(util_log.Logger).Log("msg", "failed to initialize usage report", "err", err)
		return nil, nil
	}
	t.usageReport = ur
	return ur, nil
}

func (t *Loki) deleteRequestsClient(clientType string, limits limiter.CombinedLimits) (deletion.DeleteRequestsClient, error) {
	if !t.supportIndexDeleteRequest() || !t.Cfg.CompactorConfig.RetentionEnabled {
		return deletion.NewNoOpDeleteRequestsStore(), nil
	}

	compactorAddress, isGRPCAddress, err := t.compactorAddress()
	if err != nil {
		return nil, err
	}

	reg := prometheus.WrapRegistererWith(prometheus.Labels{"for": "delete_requests", "client_type": clientType}, prometheus.DefaultRegisterer)
	var compactorClient deletion.CompactorClient
	if isGRPCAddress {
		compactorClient, err = compactor_client.NewGRPCClient(compactorAddress, t.Cfg.CompactorGRPCClient, reg)
		if err != nil {
			return nil, err
		}
	} else {
		compactorClient, err = compactor_client.NewHTTPClient(compactorAddress, t.Cfg.CompactorHTTPClient)
		if err != nil {
			return nil, err
		}
	}

	client, err := deletion.NewDeleteRequestsClient(compactorClient, t.deleteClientMetrics, clientType)
	if err != nil {
		return nil, err
	}

	return deletion.NewPerTenantDeleteRequestsClient(client, limits), nil
}

func (t *Loki) createRulerQueryEngine(logger log.Logger) (eng *logql.Engine, err error) {
	deleteStore, err := t.deleteRequestsClient("rule-evaluator", t.Overrides)
	if err != nil {
		return nil, fmt.Errorf("could not create delete requests store: %w", err)
	}

	q, err := querier.New(t.Cfg.Querier, t.Store, t.ingesterQuerier, t.Overrides, deleteStore, nil)
	if err != nil {
		return nil, fmt.Errorf("could not create querier: %w", err)
	}

	return logql.NewEngine(t.Cfg.Querier.Engine, q, t.Overrides, logger), nil
}

func calculateMaxLookBack(pc config.PeriodConfig, maxLookBackConfig, minDuration time.Duration) (time.Duration, error) {
	if pc.ObjectType != indexshipper.FilesystemObjectStoreType && maxLookBackConfig.Nanoseconds() != 0 {
		return 0, errors.New("it is an error to specify a non zero `query_store_max_look_back_period` value when using any object store other than `filesystem`")
	}

	if maxLookBackConfig == 0 {
		// If the QueryStoreMaxLookBackPeriod is still it's default value of 0, set it to the minDuration.
		return minDuration, nil
	} else if maxLookBackConfig > 0 && maxLookBackConfig < minDuration {
		// If the QueryStoreMaxLookBackPeriod is > 0 (-1 is allowed for infinite), make sure it's at least greater than minDuration or throw an error
		return 0, fmt.Errorf("the configured query_store_max_look_back_period of '%v' is less than the calculated default of '%v' "+
			"which is calculated based on the max_chunk_age + 15 minute boltdb-shipper interval + 15 min additional buffer.  Increase this value"+
			"greater than the default or remove it from the configuration to use the default", maxLookBackConfig, minDuration)
	}
	return maxLookBackConfig, nil
}

func calculateAsyncStoreQueryIngestersWithin(queryIngestersWithinConfig, minDuration time.Duration) time.Duration {
	// 0 means do not limit queries, we would also not limit ingester queries from AsyncStore.
	if queryIngestersWithinConfig == 0 {
		return 0
	}

	if queryIngestersWithinConfig < minDuration {
		return minDuration
	}
	return queryIngestersWithinConfig
}

// shipperQuerierIndexUpdateDelay returns duration it could take for queriers to serve the index since it was uploaded.
// It considers upto 3 sync attempts for the indexgateway/queries to be successful in syncing the files to factor in worst case scenarios like
// failures in sync, low download throughput, various kinds of caches in between etc. which can delay the sync operation from getting all the updates from the storage.
// It also considers index cache validity because a querier could have cached index just before it was going to resync which means
// it would keep serving index until the cache entries expire.
func shipperQuerierIndexUpdateDelay(cacheValidity, resyncInterval time.Duration) time.Duration {
	return cacheValidity + resyncInterval*3
}

// shipperIngesterIndexUploadDelay returns duration it could take for an index file containing id of a chunk to be uploaded to the shared store since it got flushed.
func shipperIngesterIndexUploadDelay() time.Duration {
	return shipper_index.ShardDBsByDuration + indexshipper.UploadInterval
}

// shipperMinIngesterQueryStoreDuration returns minimum duration(with some buffer) ingesters should query their stores to
// avoid missing any logs or chunk ids due to async nature of shipper.
func shipperMinIngesterQueryStoreDuration(maxChunkAge, querierUpdateDelay time.Duration) time.Duration {
	return maxChunkAge + shipperIngesterIndexUploadDelay() + querierUpdateDelay + 5*time.Minute
}

// NewServerService constructs service from Server component.
// servicesToWaitFor is called when server is stopping, and should return all
// services that need to terminate before server actually stops.
// N.B.: this function is NOT Cortex specific, please let's keep it that way.
// Passed server should not react on signals. Early return from Run function is considered to be an error.
func NewServerService(serv *server.Server, servicesToWaitFor func() []services.Service) services.Service {
	serverDone := make(chan error, 1)

	runFn := func(ctx context.Context) error {
		go func() {
			defer close(serverDone)
			serverDone <- serv.Run()
		}()

		select {
		case <-ctx.Done():
			return nil
		case err := <-serverDone:
			if err != nil {
				return err
			}
			return fmt.Errorf("server stopped unexpectedly")
		}
	}

	stoppingFn := func(_ error) error {
		// wait until all modules are done, and then shutdown server.
		for _, s := range servicesToWaitFor() {
			_ = s.AwaitTerminated(context.Background())
		}

		// shutdown HTTP and gRPC servers (this also unblocks Run)
		serv.Shutdown()

		// if not closed yet, wait until server stops.
		<-serverDone
		level.Info(util_log.Logger).Log("msg", "server stopped")
		return nil
	}

	return services.NewBasicService(nil, runFn, stoppingFn)
}

// DisableSignalHandling puts a dummy signal handler
func DisableSignalHandling(config *server.Config) {
	config.SignalHandler = make(ignoreSignalHandler)
}

type ignoreSignalHandler chan struct{}

func (dh ignoreSignalHandler) Loop() {
	<-dh
}

func (dh ignoreSignalHandler) Stop() {
	close(dh)
}

func schemaHasBoltDBShipperConfig(scfg config.SchemaConfig) bool {
	for _, cfg := range scfg.Configs {
		if cfg.IndexType == config.BoltDBShipperType {
			return true
		}
	}

	return false
}
