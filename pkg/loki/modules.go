package loki

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"

	"github.com/NYTimes/gziphandler"
	"github.com/cortexproject/cortex/pkg/cortex"
	"github.com/cortexproject/cortex/pkg/frontend"
	"github.com/cortexproject/cortex/pkg/frontend/transport"
	"github.com/cortexproject/cortex/pkg/frontend/v1/frontendv1pb"
	"github.com/cortexproject/cortex/pkg/frontend/v2/frontendv2pb"
	cortex_querier_worker "github.com/cortexproject/cortex/pkg/querier/worker"
	"github.com/cortexproject/cortex/pkg/ring"
	cortex_ruler "github.com/cortexproject/cortex/pkg/ruler"
	"github.com/cortexproject/cortex/pkg/scheduler"
	"github.com/cortexproject/cortex/pkg/scheduler/schedulerpb"
	"github.com/go-kit/kit/log/level"
	"github.com/grafana/dskit/dslog"
	"github.com/grafana/dskit/kv/codec"
	"github.com/grafana/dskit/kv/memberlist"
	"github.com/grafana/dskit/runtimeconfig"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/thanos/pkg/discovery/dns"
	httpgrpc_server "github.com/weaveworks/common/httpgrpc/server"
	"github.com/weaveworks/common/middleware"
	"github.com/weaveworks/common/server"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/distributor"
	"github.com/grafana/loki/pkg/ingester"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/querier"
	"github.com/grafana/loki/pkg/querier/queryrange"
	"github.com/grafana/loki/pkg/ruler"
	"github.com/grafana/loki/pkg/runtime"
	loki_storage "github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/chunk/storage"
	chunk_storage "github.com/grafana/loki/pkg/storage/chunk/storage"
	chunk_util "github.com/grafana/loki/pkg/storage/chunk/util"
	"github.com/grafana/loki/pkg/storage/stores/shipper"
	"github.com/grafana/loki/pkg/storage/stores/shipper/compactor"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexgateway"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexgateway/indexgatewaypb"
	"github.com/grafana/loki/pkg/storage/stores/shipper/uploads"
	util_log "github.com/grafana/loki/pkg/util/log"
	serverutil "github.com/grafana/loki/pkg/util/server"
	"github.com/grafana/loki/pkg/validation"
)

const maxChunkAgeForTableManager = 12 * time.Hour

// The various modules that make up Loki.
const (
	Ring                     string = "ring"
	RuntimeConfig            string = "runtime-config"
	Overrides                string = "overrides"
	TenantConfigs            string = "tenant-configs"
	Server                   string = "server"
	Distributor              string = "distributor"
	Ingester                 string = "ingester"
	Querier                  string = "querier"
	IngesterQuerier          string = "ingester-querier"
	QueryFrontend            string = "query-frontend"
	QueryFrontendTripperware string = "query-frontend-tripperware"
	RulerStorage             string = "ruler-storage"
	Ruler                    string = "ruler"
	Store                    string = "store"
	TableManager             string = "table-manager"
	MemberlistKV             string = "memberlist-kv"
	Compactor                string = "compactor"
	IndexGateway             string = "index-gateway"
	QueryScheduler           string = "query-scheduler"
	All                      string = "all"
)

func (t *Loki) initServer() (services.Service, error) {
	// Loki handles signals on its own.
	cortex.DisableSignalHandling(&t.Cfg.Server)
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

	s := cortex.NewServerService(t.Server, servicesToWaitFor)

	// Best effort to propagate the org ID from the start.
	t.Server.HTTPServer.Handler = func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !t.Cfg.AuthEnabled {
				next.ServeHTTP(w, r.WithContext(user.InjectOrgID(r.Context(), "fake")))
				return
			}
			_, ctx, _ := user.ExtractOrgIDFromHTTPRequest(r)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}(t.Server.HTTPServer.Handler)

	return s, nil
}

func (t *Loki) initRing() (_ services.Service, err error) {
	t.Cfg.Ingester.LifecyclerConfig.RingConfig.KVStore.Multi.ConfigProvider = multiClientRuntimeConfigChannel(t.runtimeConfig)
	t.Cfg.Ingester.LifecyclerConfig.RingConfig.KVStore.MemberlistKV = t.MemberlistKV.GetMemberlistKV
	t.ring, err = ring.New(t.Cfg.Ingester.LifecyclerConfig.RingConfig, "ingester", ring.IngesterRingKey, prometheus.DefaultRegisterer)
	if err != nil {
		return
	}
	prometheus.MustRegister(t.ring)
	t.Server.HTTP.Handle("/ring", t.ring)
	return t.ring, nil
}

func (t *Loki) initRuntimeConfig() (services.Service, error) {
	if t.Cfg.RuntimeConfig.LoadPath == "" {
		t.Cfg.RuntimeConfig.LoadPath = t.Cfg.LimitsConfig.PerTenantOverrideConfig
		t.Cfg.RuntimeConfig.ReloadPeriod = time.Duration(t.Cfg.LimitsConfig.PerTenantOverridePeriod)
	}

	if t.Cfg.RuntimeConfig.LoadPath == "" {
		// no need to initialize module if load path is empty
		return nil, nil
	}

	t.Cfg.RuntimeConfig.Loader = loadRuntimeConfig

	// make sure to set default limits before we start loading configuration into memory
	validation.SetDefaultLimitsForYAMLUnmarshalling(t.Cfg.LimitsConfig)

	var err error
	t.runtimeConfig, err = runtimeconfig.New(t.Cfg.RuntimeConfig, prometheus.WrapRegistererWithPrefix("loki_", prometheus.DefaultRegisterer), util_log.Logger)
	return t.runtimeConfig, err
}

func (t *Loki) initOverrides() (_ services.Service, err error) {
	t.overrides, err = validation.NewOverrides(t.Cfg.LimitsConfig, newtenantLimitsFromRuntimeConfig(t.runtimeConfig))
	// overrides are not a service, since they don't have any operational state.
	return nil, err
}

func (t *Loki) initTenantConfigs() (_ services.Service, err error) {
	t.tenantConfigs, err = runtime.NewTenantConfigs(tenantConfigFromRuntimeConfig(t.runtimeConfig))
	// tenantConfigs are not a service, since they don't have any operational state.
	return nil, err
}

func (t *Loki) initDistributor() (services.Service, error) {
	t.Cfg.Distributor.DistributorRing.KVStore.Multi.ConfigProvider = multiClientRuntimeConfigChannel(t.runtimeConfig)
	t.Cfg.Distributor.DistributorRing.KVStore.MemberlistKV = t.MemberlistKV.GetMemberlistKV
	var err error
	t.distributor, err = distributor.New(t.Cfg.Distributor, t.Cfg.IngesterClient, t.tenantConfigs, t.ring, t.overrides, prometheus.DefaultRegisterer)
	if err != nil {
		return nil, err
	}

	if !t.Cfg.isModuleEnabled(All) {
		logproto.RegisterPusherServer(t.Server.GRPC, t.distributor)
	}

	pushHandler := middleware.Merge(
		serverutil.RecoveryHTTPMiddleware,
		t.HTTPAuthMiddleware,
	).Wrap(http.HandlerFunc(t.distributor.PushHandler))

	t.Server.HTTP.Handle("/api/prom/push", pushHandler)
	t.Server.HTTP.Handle("/loki/api/v1/push", pushHandler)
	return t.distributor, nil
}

func (t *Loki) initQuerier() (services.Service, error) {
	var (
		worker services.Service
		err    error
	)

	// NewQuerierWorker now expects Frontend (or Scheduler) address to be set.
	if t.Cfg.Worker.FrontendAddress != "" || t.Cfg.Worker.SchedulerAddress != "" {
		t.Cfg.Worker.MaxConcurrentRequests = t.Cfg.Querier.MaxConcurrent
		level.Debug(util_log.Logger).Log("msg", "initializing querier worker", "config", fmt.Sprintf("%+v", t.Cfg.Worker))
		worker, err = cortex_querier_worker.NewQuerierWorker(t.Cfg.Worker, httpgrpc_server.NewServer(t.Server.HTTPServer.Handler), util_log.Logger, prometheus.DefaultRegisterer)
		if err != nil {
			return nil, err
		}
	}

	if t.Cfg.Ingester.QueryStoreMaxLookBackPeriod != 0 {
		t.Cfg.Querier.IngesterQueryStoreMaxLookback = t.Cfg.Ingester.QueryStoreMaxLookBackPeriod
	}
	t.Querier, err = querier.New(t.Cfg.Querier, t.Store, t.ingesterQuerier, t.overrides)
	if err != nil {
		return nil, err
	}

	httpMiddleware := middleware.Merge(
		serverutil.RecoveryHTTPMiddleware,
		t.HTTPAuthMiddleware,
		serverutil.NewPrepopulateMiddleware(),
		serverutil.ResponseJSONMiddleware(),
	)
	t.Server.HTTP.Handle("/loki/api/v1/query_range", httpMiddleware.Wrap(http.HandlerFunc(t.Querier.RangeQueryHandler)))
	t.Server.HTTP.Handle("/loki/api/v1/query", httpMiddleware.Wrap(http.HandlerFunc(t.Querier.InstantQueryHandler)))
	// Prometheus compatibility requires `loki/api/v1/labels` however we already released `loki/api/v1/label`
	// which is a little more consistent with `/loki/api/v1/label/{name}/values` so we are going to handle both paths.
	t.Server.HTTP.Handle("/loki/api/v1/label", httpMiddleware.Wrap(http.HandlerFunc(t.Querier.LabelHandler)))
	t.Server.HTTP.Handle("/loki/api/v1/labels", httpMiddleware.Wrap(http.HandlerFunc(t.Querier.LabelHandler)))
	t.Server.HTTP.Handle("/loki/api/v1/label/{name}/values", httpMiddleware.Wrap(http.HandlerFunc(t.Querier.LabelHandler)))
	t.Server.HTTP.Handle("/loki/api/v1/tail", httpMiddleware.Wrap(http.HandlerFunc(t.Querier.TailHandler)))
	t.Server.HTTP.Handle("/loki/api/v1/series", httpMiddleware.Wrap(http.HandlerFunc(t.Querier.SeriesHandler)))

	t.Server.HTTP.Handle("/api/prom/query", httpMiddleware.Wrap(http.HandlerFunc(t.Querier.LogQueryHandler)))
	t.Server.HTTP.Handle("/api/prom/label", httpMiddleware.Wrap(http.HandlerFunc(t.Querier.LabelHandler)))
	t.Server.HTTP.Handle("/api/prom/label/{name}/values", httpMiddleware.Wrap(http.HandlerFunc(t.Querier.LabelHandler)))
	t.Server.HTTP.Handle("/api/prom/tail", httpMiddleware.Wrap(http.HandlerFunc(t.Querier.TailHandler)))
	t.Server.HTTP.Handle("/api/prom/series", httpMiddleware.Wrap(http.HandlerFunc(t.Querier.SeriesHandler)))
	return worker, nil // ok if worker is nil here
}

func (t *Loki) initIngester() (_ services.Service, err error) {
	t.Cfg.Ingester.LifecyclerConfig.RingConfig.KVStore.Multi.ConfigProvider = multiClientRuntimeConfigChannel(t.runtimeConfig)
	t.Cfg.Ingester.LifecyclerConfig.RingConfig.KVStore.MemberlistKV = t.MemberlistKV.GetMemberlistKV
	t.Cfg.Ingester.LifecyclerConfig.ListenPort = t.Cfg.Server.GRPCListenPort

	t.Ingester, err = ingester.New(t.Cfg.Ingester, t.Cfg.IngesterClient, t.Store, t.overrides, t.tenantConfigs, prometheus.DefaultRegisterer)
	if err != nil {
		return
	}

	logproto.RegisterPusherServer(t.Server.GRPC, t.Ingester)
	logproto.RegisterQuerierServer(t.Server.GRPC, t.Ingester)
	logproto.RegisterIngesterServer(t.Server.GRPC, t.Ingester)
	t.Server.HTTP.Path("/flush").Handler(http.HandlerFunc(t.Ingester.FlushHandler))
	t.Server.HTTP.Methods("POST").Path("/ingester/flush_shutdown").Handler(http.HandlerFunc(t.Ingester.ShutdownHandler))
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

	tableClient, err := storage.NewTableClient(lastConfig.IndexType, t.Cfg.StorageConfig.Config, reg)
	if err != nil {
		return nil, err
	}

	bucketClient, err := storage.NewBucketClient(t.Cfg.StorageConfig.Config)
	dslog.CheckFatal("initializing bucket client", err, util_log.Logger)

	t.tableManager, err = chunk.NewTableManager(t.Cfg.TableManager, t.Cfg.SchemaConfig.SchemaConfig, maxChunkAgeForTableManager, tableClient, bucketClient, nil, prometheus.DefaultRegisterer)
	if err != nil {
		return nil, err
	}

	return t.tableManager, nil
}

func (t *Loki) initStore() (_ services.Service, err error) {
	// If RF > 1 and current or upcoming index type is boltdb-shipper then disable index dedupe and write dedupe cache.
	// This is to ensure that index entries are replicated to all the boltdb files in ingesters flushing replicated data.
	if t.Cfg.Ingester.LifecyclerConfig.RingConfig.ReplicationFactor > 1 && loki_storage.UsingBoltdbShipper(t.Cfg.SchemaConfig.Configs) {
		t.Cfg.ChunkStoreConfig.DisableIndexDeduplication = true
		t.Cfg.ChunkStoreConfig.WriteDedupeCacheConfig = cache.Config{}
	}

	if loki_storage.UsingBoltdbShipper(t.Cfg.SchemaConfig.Configs) {
		t.Cfg.StorageConfig.BoltDBShipperConfig.IngesterName = t.Cfg.Ingester.LifecyclerConfig.ID
		switch true {
		case t.Cfg.isModuleEnabled(Ingester):
			// We do not want ingester to unnecessarily keep downloading files
			t.Cfg.StorageConfig.BoltDBShipperConfig.Mode = shipper.ModeWriteOnly
			// Use fifo cache for caching index in memory.
			t.Cfg.StorageConfig.IndexQueriesCacheConfig = cache.Config{
				EnableFifoCache: true,
				Fifocache: cache.FifoCacheConfig{
					MaxSizeBytes: "200 MB",
					// We snapshot the index in ingesters every minute for reads so reduce the index cache validity by a minute.
					// This is usually set in StorageConfig.IndexCacheValidity but since this is exclusively used for caching the index entries,
					// I(Sandeep) am setting it here which also helps reduce some CPU cycles and allocations required for
					// unmarshalling the cached data to check the expiry.
					Validity: t.Cfg.StorageConfig.IndexCacheValidity - 1*time.Minute,
				},
			}
			t.Cfg.StorageConfig.BoltDBShipperConfig.IngesterDBRetainPeriod = boltdbShipperQuerierIndexUpdateDelay(t.Cfg) + 2*time.Minute
		case t.Cfg.isModuleEnabled(Querier), t.Cfg.isModuleEnabled(Ruler):
			// We do not want query to do any updates to index
			t.Cfg.StorageConfig.BoltDBShipperConfig.Mode = shipper.ModeReadOnly
		default:
			t.Cfg.StorageConfig.BoltDBShipperConfig.Mode = shipper.ModeReadWrite
			t.Cfg.StorageConfig.BoltDBShipperConfig.IngesterDBRetainPeriod = boltdbShipperQuerierIndexUpdateDelay(t.Cfg) + 2*time.Minute
		}
	}

	chunkStore, err := chunk_storage.NewStore(t.Cfg.StorageConfig.Config, t.Cfg.ChunkStoreConfig.StoreConfig, t.Cfg.SchemaConfig.SchemaConfig, t.overrides, prometheus.DefaultRegisterer, nil, util_log.Logger)
	if err != nil {
		return
	}

	if loki_storage.UsingBoltdbShipper(t.Cfg.SchemaConfig.Configs) {
		boltdbShipperMinIngesterQueryStoreDuration := boltdbShipperMinIngesterQueryStoreDuration(t.Cfg)
		switch true {
		case t.Cfg.isModuleEnabled(Querier), t.Cfg.isModuleEnabled(Ruler):
			// Do not use the AsyncStore if the querier is configured with QueryStoreOnly set to true
			if t.Cfg.Querier.QueryStoreOnly {
				break
			}
			// Use AsyncStore to query both ingesters local store and chunk store for store queries.
			// Only queriers should use the AsyncStore, it should never be used in ingesters.
			chunkStore = loki_storage.NewAsyncStore(chunkStore, t.ingesterQuerier,
				calculateAsyncStoreQueryIngestersWithin(t.Cfg.Querier.QueryIngestersWithin, boltdbShipperMinIngesterQueryStoreDuration),
			)
		case t.Cfg.isModuleEnabled(All):
			// We want ingester to also query the store when using boltdb-shipper but only when running with target All.
			// We do not want to use AsyncStore otherwise it would start spiraling around doing queries over and over again to the ingesters and store.
			// ToDo: See if we can avoid doing this when not running loki in clustered mode.
			t.Cfg.Ingester.QueryStore = true
			boltdbShipperConfigIdx := loki_storage.ActivePeriodConfig(t.Cfg.SchemaConfig.Configs)
			if t.Cfg.SchemaConfig.Configs[boltdbShipperConfigIdx].IndexType != shipper.BoltDBShipperType {
				boltdbShipperConfigIdx++
			}
			mlb, err := calculateMaxLookBack(t.Cfg.SchemaConfig.Configs[boltdbShipperConfigIdx], t.Cfg.Ingester.QueryStoreMaxLookBackPeriod,
				boltdbShipperMinIngesterQueryStoreDuration)
			if err != nil {
				return nil, err
			}
			t.Cfg.Ingester.QueryStoreMaxLookBackPeriod = mlb
		}
	}

	t.Store, err = loki_storage.NewStore(t.Cfg.StorageConfig, t.Cfg.SchemaConfig, chunkStore, prometheus.DefaultRegisterer)
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
		t.overrides,
		t.Cfg.SchemaConfig.SchemaConfig,
		t.Cfg.Querier.QueryIngestersWithin,
		prometheus.DefaultRegisterer,
	)
	if err != nil {
		return
	}
	t.stopper = stopper
	t.QueryFrontEndTripperware = tripperware

	return services.NewIdleService(nil, nil), nil
}

func (t *Loki) initQueryFrontend() (_ services.Service, err error) {
	level.Debug(util_log.Logger).Log("msg", "initializing query frontend", "config", fmt.Sprintf("%+v", t.Cfg.Frontend))

	roundTripper, frontendV1, frontendV2, err := frontend.InitFrontend(frontend.CombinedFrontendConfig{
		Handler:       t.Cfg.Frontend.Handler,
		FrontendV1:    t.Cfg.Frontend.FrontendV1,
		FrontendV2:    t.Cfg.Frontend.FrontendV2,
		DownstreamURL: t.Cfg.Frontend.DownstreamURL,
	}, disabledShuffleShardingLimits{}, t.Cfg.Server.GRPCListenPort, util_log.Logger, prometheus.DefaultRegisterer)
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

	frontendHandler = middleware.Merge(
		serverutil.RecoveryHTTPMiddleware,
		t.HTTPAuthMiddleware,
		queryrange.StatsHTTPMiddleware,
		serverutil.NewPrepopulateMiddleware(),
		serverutil.ResponseJSONMiddleware(),
	).Wrap(frontendHandler)

	var defaultHandler http.Handler
	if t.Cfg.Frontend.TailProxyURL != "" {
		httpMiddleware := middleware.Merge(
			t.HTTPAuthMiddleware,
			queryrange.StatsHTTPMiddleware,
		)
		tailURL, err := url.Parse(t.Cfg.Frontend.TailProxyURL)
		if err != nil {
			return nil, err
		}
		tp := httputil.NewSingleHostReverseProxy(tailURL)

		director := tp.Director
		tp.Director = func(req *http.Request) {
			director(req)
			req.Host = tailURL.Host
		}

		defaultHandler = httpMiddleware.Wrap(tp)
	} else {
		defaultHandler = frontendHandler
	}
	t.Server.HTTP.Handle("/loki/api/v1/query_range", frontendHandler)
	t.Server.HTTP.Handle("/loki/api/v1/query", frontendHandler)
	t.Server.HTTP.Handle("/loki/api/v1/label", frontendHandler)
	t.Server.HTTP.Handle("/loki/api/v1/labels", frontendHandler)
	t.Server.HTTP.Handle("/loki/api/v1/label/{name}/values", frontendHandler)
	t.Server.HTTP.Handle("/loki/api/v1/series", frontendHandler)
	t.Server.HTTP.Handle("/api/prom/query", frontendHandler)
	t.Server.HTTP.Handle("/api/prom/label", frontendHandler)
	t.Server.HTTP.Handle("/api/prom/label/{name}/values", frontendHandler)
	t.Server.HTTP.Handle("/api/prom/series", frontendHandler)

	// defer tail endpoints to the default handler
	t.Server.HTTP.Handle("/loki/api/v1/tail", defaultHandler)
	t.Server.HTTP.Handle("/api/prom/tail", defaultHandler)

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
	if t.Cfg.isModuleEnabled(All) && t.Cfg.Ruler.StoreConfig.IsDefaults() {
		level.Info(util_log.Logger).Log("msg", "RulerStorage is not configured in single binary mode and will not be started.")
		return
	}

	// Loki doesn't support the configdb backend, but without excessive mangling/refactoring
	// it's hard to enforce this at validation time. Therefore detect this and fail early.
	if t.Cfg.Ruler.StoreConfig.Type == "configdb" {
		return nil, errors.New("configdb is not supported as a Loki rules backend type")
	}

	// Make sure storage directory exists if using filesystem store
	if t.Cfg.Ruler.StoreConfig.Type == "local" && t.Cfg.Ruler.StoreConfig.Local.Directory != "" {
		err := chunk_util.EnsureDirectory(t.Cfg.Ruler.StoreConfig.Local.Directory)
		if err != nil {
			return nil, err
		}
	}

	t.RulerStorage, err = cortex_ruler.NewLegacyRuleStore(t.Cfg.Ruler.StoreConfig, ruler.GroupLoader{}, util_log.Logger)

	return
}

func (t *Loki) initRuler() (_ services.Service, err error) {
	if t.RulerStorage == nil {
		level.Info(util_log.Logger).Log("msg", "RulerStorage is nil.  Not starting the ruler.")
		return nil, nil
	}

	t.Cfg.Ruler.Ring.ListenPort = t.Cfg.Server.GRPCListenPort
	t.Cfg.Ruler.Ring.KVStore.MemberlistKV = t.MemberlistKV.GetMemberlistKV
	q, err := querier.New(t.Cfg.Querier, t.Store, t.ingesterQuerier, t.overrides)
	if err != nil {
		return nil, err
	}

	engine := logql.NewEngine(t.Cfg.Querier.Engine, q, t.overrides)

	t.ruler, err = ruler.NewRuler(
		t.Cfg.Ruler,
		engine,
		prometheus.DefaultRegisterer,
		util_log.Logger,
		t.RulerStorage,
		t.overrides,
	)

	if err != nil {
		return
	}

	t.rulerAPI = cortex_ruler.NewAPI(t.ruler, t.RulerStorage, util_log.Logger)

	// Expose HTTP endpoints.
	if t.Cfg.Ruler.EnableAPI {

		t.Server.HTTP.Handle("/ruler/ring", t.ruler)
		cortex_ruler.RegisterRulerServer(t.Server.GRPC, t.ruler)

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

	return t.ruler, nil
}

func (t *Loki) initMemberlistKV() (services.Service, error) {
	reg := prometheus.DefaultRegisterer

	t.Cfg.MemberlistKV.MetricsRegisterer = reg
	t.Cfg.MemberlistKV.Codecs = []codec.Codec{
		ring.GetCodec(),
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
	return t.MemberlistKV, nil
}

func (t *Loki) initCompactor() (services.Service, error) {
	err := t.Cfg.SchemaConfig.Load()
	if err != nil {
		return nil, err
	}
	t.compactor, err = compactor.NewCompactor(t.Cfg.CompactorConfig, t.Cfg.StorageConfig.Config, t.Cfg.SchemaConfig, t.overrides, prometheus.DefaultRegisterer)
	if err != nil {
		return nil, err
	}

	if t.Cfg.CompactorConfig.RetentionEnabled {
		t.Server.HTTP.Path("/loki/api/admin/delete").Methods("PUT", "POST").Handler(t.HTTPAuthMiddleware.Wrap(http.HandlerFunc(t.compactor.DeleteRequestsHandler.AddDeleteRequestHandler)))
		t.Server.HTTP.Path("/loki/api/admin/delete").Methods("GET").Handler(t.HTTPAuthMiddleware.Wrap(http.HandlerFunc(t.compactor.DeleteRequestsHandler.GetAllDeleteRequestsHandler)))
		t.Server.HTTP.Path("/loki/api/admin/cancel_delete_request").Methods("PUT", "POST").Handler(t.HTTPAuthMiddleware.Wrap(http.HandlerFunc(t.compactor.DeleteRequestsHandler.CancelDeleteRequestHandler)))
	}

	return t.compactor, nil
}

func (t *Loki) initIndexGateway() (services.Service, error) {
	t.Cfg.StorageConfig.BoltDBShipperConfig.Mode = shipper.ModeReadOnly
	objectClient, err := storage.NewObjectClient(t.Cfg.StorageConfig.BoltDBShipperConfig.SharedStoreType, t.Cfg.StorageConfig.Config)
	if err != nil {
		return nil, err
	}

	shipperIndexClient, err := shipper.NewShipper(t.Cfg.StorageConfig.BoltDBShipperConfig, objectClient, prometheus.DefaultRegisterer)
	if err != nil {
		return nil, err
	}

	gateway := indexgateway.NewIndexGateway(shipperIndexClient.(*shipper.Shipper))
	indexgatewaypb.RegisterIndexGatewayServer(t.Server.GRPC, gateway)
	return gateway, nil
}

func (t *Loki) initQueryScheduler() (services.Service, error) {
	s, err := scheduler.NewScheduler(t.Cfg.QueryScheduler, t.overrides, util_log.Logger, prometheus.DefaultRegisterer)
	if err != nil {
		return nil, err
	}

	schedulerpb.RegisterSchedulerForFrontendServer(t.Server.GRPC, s)
	schedulerpb.RegisterSchedulerForQuerierServer(t.Server.GRPC, s)
	return s, nil
}

func calculateMaxLookBack(pc chunk.PeriodConfig, maxLookBackConfig, minDuration time.Duration) (time.Duration, error) {
	if pc.ObjectType != shipper.FilesystemObjectStoreType && maxLookBackConfig.Nanoseconds() != 0 {
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

// boltdbShipperQuerierIndexUpdateDelay returns duration it could take for queriers to serve the index since it was uploaded.
// It also considers index cache validity because a querier could have cached index just before it was going to resync which means
// it would keep serving index until the cache entries expire.
func boltdbShipperQuerierIndexUpdateDelay(cfg Config) time.Duration {
	return cfg.StorageConfig.IndexCacheValidity + cfg.StorageConfig.BoltDBShipperConfig.ResyncInterval
}

// boltdbShipperIngesterIndexUploadDelay returns duration it could take for an index file containing id of a chunk to be uploaded to the shared store since it got flushed.
func boltdbShipperIngesterIndexUploadDelay() time.Duration {
	return uploads.ShardDBsByDuration + shipper.UploadInterval
}

// boltdbShipperMinIngesterQueryStoreDuration returns minimum duration(with some buffer) ingesters should query their stores to
// avoid missing any logs or chunk ids due to async nature of BoltDB Shipper.
func boltdbShipperMinIngesterQueryStoreDuration(cfg Config) time.Duration {
	return cfg.Ingester.MaxChunkAge + boltdbShipperIngesterIndexUploadDelay() + boltdbShipperQuerierIndexUpdateDelay(cfg) + 2*time.Minute
}
