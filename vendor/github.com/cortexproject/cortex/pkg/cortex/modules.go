package cortex

import (
	"fmt"
	"os"
	"time"

	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/promql"
	prom_storage "github.com/prometheus/prometheus/storage"
	httpgrpc_server "github.com/weaveworks/common/httpgrpc/server"
	"github.com/weaveworks/common/instrument"
	"github.com/weaveworks/common/middleware"
	"github.com/weaveworks/common/server"

	"github.com/cortexproject/cortex/pkg/alertmanager"
	"github.com/cortexproject/cortex/pkg/api"
	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/purger"
	"github.com/cortexproject/cortex/pkg/chunk/storage"
	"github.com/cortexproject/cortex/pkg/compactor"
	configAPI "github.com/cortexproject/cortex/pkg/configs/api"
	"github.com/cortexproject/cortex/pkg/configs/db"
	"github.com/cortexproject/cortex/pkg/distributor"
	"github.com/cortexproject/cortex/pkg/flusher"
	"github.com/cortexproject/cortex/pkg/ingester"
	"github.com/cortexproject/cortex/pkg/querier"
	"github.com/cortexproject/cortex/pkg/querier/frontend"
	"github.com/cortexproject/cortex/pkg/querier/queryrange"
	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/ring/kv/codec"
	"github.com/cortexproject/cortex/pkg/ring/kv/memberlist"
	"github.com/cortexproject/cortex/pkg/ruler"
	"github.com/cortexproject/cortex/pkg/storegateway"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/modules"
	"github.com/cortexproject/cortex/pkg/util/runtimeconfig"
	"github.com/cortexproject/cortex/pkg/util/services"
	"github.com/cortexproject/cortex/pkg/util/validation"
)

// The various modules that make up Cortex.
const (
	API                 string = "api"
	Ring                string = "ring"
	RuntimeConfig       string = "runtime-config"
	Overrides           string = "overrides"
	Server              string = "server"
	Distributor         string = "distributor"
	Ingester            string = "ingester"
	Flusher             string = "flusher"
	Querier             string = "querier"
	StoreQueryable      string = "store-queryable"
	QueryFrontend       string = "query-frontend"
	Store               string = "store"
	DeleteRequestsStore string = "delete-requests-store"
	TableManager        string = "table-manager"
	RulerStorage        string = "ruler-storage"
	Ruler               string = "ruler"
	Configs             string = "configs"
	AlertManager        string = "alertmanager"
	Compactor           string = "compactor"
	StoreGateway        string = "store-gateway"
	MemberlistKV        string = "memberlist-kv"
	Purger              string = "purger"
	All                 string = "all"
)

func (t *Cortex) initAPI() (services.Service, error) {
	t.Cfg.API.ServerPrefix = t.Cfg.Server.PathPrefix
	t.Cfg.API.LegacyHTTPPrefix = t.Cfg.HTTPPrefix

	a, err := api.New(t.Cfg.API, t.Cfg.Server, t.Server, util.Logger)
	if err != nil {
		return nil, err
	}

	t.API = a

	t.API.RegisterAPI(t.Cfg)

	return nil, nil
}

func (t *Cortex) initServer() (services.Service, error) {
	// Cortex handles signals on its own.
	DisableSignalHandling(&t.Cfg.Server)
	serv, err := server.New(t.Cfg.Server)
	if err != nil {
		return nil, err
	}

	t.Server = serv

	servicesToWaitFor := func() []services.Service {
		svs := []services.Service(nil)
		for m, s := range t.ServiceMap {
			// Server should not wait for itself.
			if m != Server {
				svs = append(svs, s)
			}
		}
		return svs
	}

	s := NewServerService(t.Server, servicesToWaitFor)

	return s, nil
}

func (t *Cortex) initRing() (serv services.Service, err error) {
	t.Cfg.Ingester.LifecyclerConfig.RingConfig.KVStore.Multi.ConfigProvider = multiClientRuntimeConfigChannel(t.RuntimeConfig)
	t.Ring, err = ring.New(t.Cfg.Ingester.LifecyclerConfig.RingConfig, "ingester", ring.IngesterRingKey, prometheus.DefaultRegisterer)
	if err != nil {
		return nil, err
	}
	prometheus.MustRegister(t.Ring)

	t.API.RegisterRing(t.Ring)

	return t.Ring, nil
}

func (t *Cortex) initRuntimeConfig() (services.Service, error) {
	if t.Cfg.RuntimeConfig.LoadPath == "" {
		t.Cfg.RuntimeConfig.LoadPath = t.Cfg.LimitsConfig.PerTenantOverrideConfig
		t.Cfg.RuntimeConfig.ReloadPeriod = t.Cfg.LimitsConfig.PerTenantOverridePeriod
	}

	if t.Cfg.RuntimeConfig.LoadPath == "" {
		// no need to initialize module if load path is empty
		return nil, nil
	}
	t.Cfg.RuntimeConfig.Loader = loadRuntimeConfig

	// make sure to set default limits before we start loading configuration into memory
	validation.SetDefaultLimitsForYAMLUnmarshalling(t.Cfg.LimitsConfig)

	serv, err := runtimeconfig.NewRuntimeConfigManager(t.Cfg.RuntimeConfig, prometheus.DefaultRegisterer)
	t.RuntimeConfig = serv
	return serv, err
}

func (t *Cortex) initOverrides() (serv services.Service, err error) {
	t.Overrides, err = validation.NewOverrides(t.Cfg.LimitsConfig, tenantLimitsFromRuntimeConfig(t.RuntimeConfig))
	// overrides don't have operational state, nor do they need to do anything more in starting/stopping phase,
	// so there is no need to return any service.
	return nil, err
}

func (t *Cortex) initDistributor() (serv services.Service, err error) {
	t.Cfg.Distributor.DistributorRing.ListenPort = t.Cfg.Server.GRPCListenPort

	// Check whether the distributor can join the distributors ring, which is
	// whenever it's not running as an internal dependency (ie. querier or
	// ruler's dependency)
	canJoinDistributorsRing := (t.Cfg.Target == All || t.Cfg.Target == Distributor)

	t.Distributor, err = distributor.New(t.Cfg.Distributor, t.Cfg.IngesterClient, t.Overrides, t.Ring, canJoinDistributorsRing, prometheus.DefaultRegisterer)
	if err != nil {
		return
	}

	t.API.RegisterDistributor(t.Distributor, t.Cfg.Distributor)

	return t.Distributor, nil
}

func (t *Cortex) initQuerier() (serv services.Service, err error) {
	querierRegisterer := prometheus.WrapRegistererWith(prometheus.Labels{"engine": "querier"}, prometheus.DefaultRegisterer)
	queryable, engine := querier.New(t.Cfg.Querier, t.Overrides, t.Distributor, t.StoreQueryables, t.TombstonesLoader, querierRegisterer)

	// Prometheus histograms for requests to the querier.
	querierRequestDuration := promauto.With(prometheus.DefaultRegisterer).NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cortex",
		Name:      "querier_request_duration_seconds",
		Help:      "Time (in seconds) spent serving HTTP requests to the querier.",
		Buckets:   instrument.DefBuckets,
	}, []string{"method", "route", "status_code", "ws"})

	receivedMessageSize := promauto.With(prometheus.DefaultRegisterer).NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cortex",
		Name:      "querier_request_message_bytes",
		Help:      "Size (in bytes) of messages received in the request to the querier.",
		Buckets:   middleware.BodySizeBuckets,
	}, []string{"method", "route"})

	sentMessageSize := promauto.With(prometheus.DefaultRegisterer).NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cortex",
		Name:      "querier_response_message_bytes",
		Help:      "Size (in bytes) of messages sent in response by the querier.",
		Buckets:   middleware.BodySizeBuckets,
	}, []string{"method", "route"})

	inflightRequests := promauto.With(prometheus.DefaultRegisterer).NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "cortex",
		Name:      "querier_inflight_requests",
		Help:      "Current number of inflight requests to the querier.",
	}, []string{"method", "route"})

	// if we are not configured for single binary mode then the querier needs to register its paths externally
	registerExternally := t.Cfg.Target != All
	handler := t.API.RegisterQuerier(queryable, engine, t.Distributor, registerExternally, t.TombstonesLoader, querierRequestDuration, receivedMessageSize, sentMessageSize, inflightRequests)

	// single binary mode requires a properly configured worker.  if the operator did not attempt to configure the
	//  worker we will attempt an automatic configuration here
	if t.Cfg.Worker.Address == "" && t.Cfg.Target == All {
		address := fmt.Sprintf("127.0.0.1:%d", t.Cfg.Server.GRPCListenPort)
		level.Warn(util.Logger).Log("msg", "Worker address is empty in single binary mode.  Attempting automatic worker configuration.  If queries are unresponsive consider configuring the worker explicitly.", "address", address)
		t.Cfg.Worker.Address = address
	}

	// Query frontend worker will only be started after all its dependencies are started, not here.
	// Worker may also be nil, if not configured, which is OK.
	worker, err := frontend.NewWorker(t.Cfg.Worker, t.Cfg.Querier, httpgrpc_server.NewServer(handler), util.Logger)
	if err != nil {
		return
	}

	return worker, nil
}

func (t *Cortex) initStoreQueryables() (services.Service, error) {
	var servs []services.Service

	//nolint:golint // I prefer this form over removing 'else', because it allows q to have smaller scope.
	if q, err := initQueryableForEngine(t.Cfg.Storage.Engine, t.Cfg, t.Store, t.Overrides, prometheus.DefaultRegisterer); err != nil {
		return nil, fmt.Errorf("failed to initialize querier for engine '%s': %v", t.Cfg.Storage.Engine, err)
	} else {
		t.StoreQueryables = append(t.StoreQueryables, querier.UseAlwaysQueryable(q))
		if s, ok := q.(services.Service); ok {
			servs = append(servs, s)
		}
	}

	if t.Cfg.Querier.SecondStoreEngine != "" {
		if t.Cfg.Querier.SecondStoreEngine == t.Cfg.Storage.Engine {
			return nil, fmt.Errorf("second store engine used by querier '%s' must be different than primary engine '%s'", t.Cfg.Querier.SecondStoreEngine, t.Cfg.Storage.Engine)
		}

		sq, err := initQueryableForEngine(t.Cfg.Querier.SecondStoreEngine, t.Cfg, t.Store, t.Overrides, prometheus.DefaultRegisterer)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize querier for engine '%s': %v", t.Cfg.Querier.SecondStoreEngine, err)
		}

		t.StoreQueryables = append(t.StoreQueryables, querier.UseBeforeTimestampQueryable(sq, time.Time(t.Cfg.Querier.UseSecondStoreBeforeTime)))

		if s, ok := sq.(services.Service); ok {
			servs = append(servs, s)
		}
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

func initQueryableForEngine(engine string, cfg Config, chunkStore chunk.Store, limits *validation.Overrides, reg prometheus.Registerer) (prom_storage.Queryable, error) {
	switch engine {
	case storage.StorageEngineChunks:
		if chunkStore == nil {
			return nil, fmt.Errorf("chunk store not initialized")
		}
		return querier.NewChunkStoreQueryable(cfg.Querier, chunkStore), nil

	case storage.StorageEngineBlocks:
		// When running in single binary, if the blocks sharding is disabled and no custom
		// store-gateway address has been configured, we can set it to the running process.
		if cfg.Target == All && !cfg.StoreGateway.ShardingEnabled && cfg.Querier.StoreGatewayAddresses == "" {
			cfg.Querier.StoreGatewayAddresses = fmt.Sprintf("127.0.0.1:%d", cfg.Server.GRPCListenPort)
		}

		return querier.NewBlocksStoreQueryableFromConfig(cfg.Querier, cfg.StoreGateway, cfg.BlocksStorage, limits, util.Logger, reg)

	default:
		return nil, fmt.Errorf("unknown storage engine '%s'", engine)
	}
}

func (t *Cortex) tsdbIngesterConfig() {
	t.Cfg.Ingester.BlocksStorageEnabled = t.Cfg.Storage.Engine == storage.StorageEngineBlocks
	t.Cfg.Ingester.BlocksStorageConfig = t.Cfg.BlocksStorage
}

func (t *Cortex) initIngester() (serv services.Service, err error) {
	t.Cfg.Ingester.LifecyclerConfig.RingConfig.KVStore.Multi.ConfigProvider = multiClientRuntimeConfigChannel(t.RuntimeConfig)
	t.Cfg.Ingester.LifecyclerConfig.ListenPort = t.Cfg.Server.GRPCListenPort
	t.Cfg.Ingester.ShardByAllLabels = t.Cfg.Distributor.ShardByAllLabels
	t.tsdbIngesterConfig()

	t.Ingester, err = ingester.New(t.Cfg.Ingester, t.Cfg.IngesterClient, t.Overrides, t.Store, prometheus.DefaultRegisterer)
	if err != nil {
		return
	}

	t.API.RegisterIngester(t.Ingester, t.Cfg.Distributor)

	return t.Ingester, nil
}

func (t *Cortex) initFlusher() (serv services.Service, err error) {
	t.tsdbIngesterConfig()

	t.Flusher, err = flusher.New(
		t.Cfg.Flusher,
		t.Cfg.Ingester,
		t.Store,
		prometheus.DefaultRegisterer,
	)
	if err != nil {
		return
	}

	return t.Flusher, nil
}

func (t *Cortex) initChunkStore() (serv services.Service, err error) {
	if t.Cfg.Storage.Engine != storage.StorageEngineChunks && t.Cfg.Querier.SecondStoreEngine != storage.StorageEngineChunks {
		return nil, nil
	}
	err = t.Cfg.Schema.Load()
	if err != nil {
		return
	}

	t.Store, err = storage.NewStore(t.Cfg.Storage, t.Cfg.ChunkStore, t.Cfg.Schema, t.Overrides, prometheus.DefaultRegisterer, t.TombstonesLoader, util.Logger)
	if err != nil {
		return
	}

	return services.NewIdleService(nil, func(_ error) error {
		t.Store.Stop()
		return nil
	}), nil
}

func (t *Cortex) initDeleteRequestsStore() (serv services.Service, err error) {
	if !t.Cfg.PurgerConfig.Enable {
		// until we need to explicitly enable delete series support we need to do create TombstonesLoader without DeleteStore which acts as noop
		t.TombstonesLoader = purger.NewTombstonesLoader(nil, nil)

		return
	}

	var indexClient chunk.IndexClient
	reg := prometheus.WrapRegistererWith(
		prometheus.Labels{"component": DeleteRequestsStore}, prometheus.DefaultRegisterer)
	indexClient, err = storage.NewIndexClient(t.Cfg.Storage.DeleteStoreConfig.Store, t.Cfg.Storage, t.Cfg.Schema, reg)
	if err != nil {
		return
	}

	t.DeletesStore, err = purger.NewDeleteStore(t.Cfg.Storage.DeleteStoreConfig, indexClient)
	if err != nil {
		return
	}

	t.TombstonesLoader = purger.NewTombstonesLoader(t.DeletesStore, prometheus.DefaultRegisterer)

	return
}

func (t *Cortex) initQueryFrontend() (serv services.Service, err error) {
	// Load the schema only if sharded queries is set.
	if t.Cfg.QueryRange.ShardedQueries {
		err = t.Cfg.Schema.Load()
		if err != nil {
			return
		}
	}

	t.Frontend, err = frontend.New(t.Cfg.Frontend, util.Logger, prometheus.DefaultRegisterer)
	if err != nil {
		return
	}

	tripperware, cache, err := queryrange.NewTripperware(
		t.Cfg.QueryRange,
		util.Logger,
		t.Overrides,
		queryrange.PrometheusCodec,
		queryrange.PrometheusResponseExtractor{},
		t.Cfg.Schema,
		promql.EngineOpts{
			Logger:     util.Logger,
			Reg:        prometheus.DefaultRegisterer,
			MaxSamples: t.Cfg.Querier.MaxSamples,
			Timeout:    t.Cfg.Querier.Timeout,
			NoStepSubqueryIntervalFn: func(int64) int64 {
				return t.Cfg.Querier.DefaultEvaluationInterval.Milliseconds()
			},
		},
		t.Cfg.Querier.QueryIngestersWithin,
		prometheus.DefaultRegisterer,
		t.TombstonesLoader,
	)

	if err != nil {
		return nil, err
	}
	t.Cache = cache
	t.Frontend.Wrap(tripperware)

	t.API.RegisterQueryFrontend(t.Frontend)

	return services.NewIdleService(nil, func(_ error) error {
		t.Frontend.Close()
		if t.Cache != nil {
			t.Cache.Stop()
			t.Cache = nil
		}
		return nil
	}), nil
}

func (t *Cortex) initTableManager() (services.Service, error) {
	if t.Cfg.Storage.Engine == storage.StorageEngineBlocks {
		return nil, nil // table manager isn't used in v2
	}

	err := t.Cfg.Schema.Load()
	if err != nil {
		return nil, err
	}

	// Assume the newest config is the one to use
	lastConfig := &t.Cfg.Schema.Configs[len(t.Cfg.Schema.Configs)-1]

	if (t.Cfg.TableManager.ChunkTables.WriteScale.Enabled ||
		t.Cfg.TableManager.IndexTables.WriteScale.Enabled ||
		t.Cfg.TableManager.ChunkTables.InactiveWriteScale.Enabled ||
		t.Cfg.TableManager.IndexTables.InactiveWriteScale.Enabled ||
		t.Cfg.TableManager.ChunkTables.ReadScale.Enabled ||
		t.Cfg.TableManager.IndexTables.ReadScale.Enabled ||
		t.Cfg.TableManager.ChunkTables.InactiveReadScale.Enabled ||
		t.Cfg.TableManager.IndexTables.InactiveReadScale.Enabled) &&
		t.Cfg.Storage.AWSStorageConfig.Metrics.URL == "" {
		level.Error(util.Logger).Log("msg", "WriteScale is enabled but no Metrics URL has been provided")
		os.Exit(1)
	}

	reg := prometheus.WrapRegistererWith(
		prometheus.Labels{"component": "table-manager-store"}, prometheus.DefaultRegisterer)

	tableClient, err := storage.NewTableClient(lastConfig.IndexType, t.Cfg.Storage, reg)
	if err != nil {
		return nil, err
	}

	bucketClient, err := storage.NewBucketClient(t.Cfg.Storage)
	util.CheckFatal("initializing bucket client", err)

	var extraTables []chunk.ExtraTables
	if t.Cfg.PurgerConfig.Enable {
		reg := prometheus.WrapRegistererWith(
			prometheus.Labels{"component": "table-manager-" + DeleteRequestsStore}, prometheus.DefaultRegisterer)

		deleteStoreTableClient, err := storage.NewTableClient(t.Cfg.Storage.DeleteStoreConfig.Store, t.Cfg.Storage, reg)
		if err != nil {
			return nil, err
		}

		extraTables = append(extraTables, chunk.ExtraTables{TableClient: deleteStoreTableClient, Tables: t.Cfg.Storage.DeleteStoreConfig.GetTables()})
	}

	t.TableManager, err = chunk.NewTableManager(t.Cfg.TableManager, t.Cfg.Schema, t.Cfg.Ingester.MaxChunkAge, tableClient,
		bucketClient, extraTables, prometheus.DefaultRegisterer)
	return t.TableManager, err
}

func (t *Cortex) initRulerStorage() (serv services.Service, err error) {
	// if the ruler is not configured and we're in single binary then let's just log an error and continue.
	// unfortunately there is no way to generate a "default" config and compare default against actual
	// to determine if it's unconfigured.  the following check, however, correctly tests this.
	// Single binary integration tests will break if this ever drifts
	if t.Cfg.Target == All && t.Cfg.Ruler.StoreConfig.IsDefaults() {
		level.Info(util.Logger).Log("msg", "RulerStorage is not configured in single binary mode and will not be started.")
		return
	}

	t.RulerStorage, err = ruler.NewRuleStorage(t.Cfg.Ruler.StoreConfig)

	return
}

func (t *Cortex) initRuler() (serv services.Service, err error) {
	if t.RulerStorage == nil {
		level.Info(util.Logger).Log("msg", "RulerStorage is nil.  Not starting the ruler.")
		return nil, nil
	}

	t.Cfg.Ruler.Ring.ListenPort = t.Cfg.Server.GRPCListenPort
	rulerRegisterer := prometheus.WrapRegistererWith(prometheus.Labels{"engine": "ruler"}, prometheus.DefaultRegisterer)
	queryable, engine := querier.New(t.Cfg.Querier, t.Overrides, t.Distributor, t.StoreQueryables, t.TombstonesLoader, rulerRegisterer)

	managerFactory := ruler.DefaultTenantManagerFactory(t.Cfg.Ruler, t.Distributor, queryable, engine)
	manager, err := ruler.NewDefaultMultiTenantManager(t.Cfg.Ruler, managerFactory, prometheus.DefaultRegisterer, util.Logger)
	if err != nil {
		return nil, err
	}

	t.Ruler, err = ruler.NewRuler(
		t.Cfg.Ruler,
		manager,
		prometheus.DefaultRegisterer,
		util.Logger,
		t.RulerStorage,
	)
	if err != nil {
		return
	}

	// Expose HTTP endpoints.
	t.API.RegisterRuler(t.Ruler, t.Cfg.Ruler.EnableAPI)

	return t.Ruler, nil
}

func (t *Cortex) initConfig() (serv services.Service, err error) {
	t.ConfigDB, err = db.New(t.Cfg.Configs.DB)
	if err != nil {
		return
	}

	t.ConfigAPI = configAPI.New(t.ConfigDB, t.Cfg.Configs.API)
	t.ConfigAPI.RegisterRoutes(t.Server.HTTP)
	return services.NewIdleService(nil, func(_ error) error {
		t.ConfigDB.Close()
		return nil
	}), nil
}

func (t *Cortex) initAlertManager() (serv services.Service, err error) {
	t.Alertmanager, err = alertmanager.NewMultitenantAlertmanager(&t.Cfg.Alertmanager, util.Logger, prometheus.DefaultRegisterer)
	if err != nil {
		return
	}

	t.API.RegisterAlertmanager(t.Alertmanager, t.Cfg.Target == AlertManager, t.Cfg.Alertmanager.EnableAPI)
	return t.Alertmanager, nil
}

func (t *Cortex) initCompactor() (serv services.Service, err error) {
	t.Cfg.Compactor.ShardingRing.ListenPort = t.Cfg.Server.GRPCListenPort

	t.Compactor, err = compactor.NewCompactor(t.Cfg.Compactor, t.Cfg.BlocksStorage, util.Logger, prometheus.DefaultRegisterer)
	if err != nil {
		return
	}

	// Expose HTTP endpoints.
	t.API.RegisterCompactor(t.Compactor)
	return t.Compactor, nil
}

func (t *Cortex) initStoreGateway() (serv services.Service, err error) {
	if t.Cfg.Storage.Engine != storage.StorageEngineBlocks {
		return nil, nil
	}

	t.Cfg.StoreGateway.ShardingRing.ListenPort = t.Cfg.Server.GRPCListenPort

	t.StoreGateway, err = storegateway.NewStoreGateway(t.Cfg.StoreGateway, t.Cfg.BlocksStorage, t.Overrides, t.Cfg.Server.LogLevel, util.Logger, prometheus.DefaultRegisterer)
	if err != nil {
		return nil, err
	}

	// Expose HTTP endpoints.
	t.API.RegisterStoreGateway(t.StoreGateway)

	return t.StoreGateway, nil
}

func (t *Cortex) initMemberlistKV() (services.Service, error) {
	t.Cfg.MemberlistKV.MetricsRegisterer = prometheus.DefaultRegisterer
	t.Cfg.MemberlistKV.Codecs = []codec.Codec{
		ring.GetCodec(),
	}
	t.MemberlistKV = memberlist.NewKVInitService(&t.Cfg.MemberlistKV)

	// Update the config.
	t.Cfg.Distributor.DistributorRing.KVStore.MemberlistKV = t.MemberlistKV.GetMemberlistKV
	t.Cfg.Ingester.LifecyclerConfig.RingConfig.KVStore.MemberlistKV = t.MemberlistKV.GetMemberlistKV
	t.Cfg.StoreGateway.ShardingRing.KVStore.MemberlistKV = t.MemberlistKV.GetMemberlistKV
	t.Cfg.Compactor.ShardingRing.KVStore.MemberlistKV = t.MemberlistKV.GetMemberlistKV
	t.Cfg.Ruler.Ring.KVStore.MemberlistKV = t.MemberlistKV.GetMemberlistKV

	return t.MemberlistKV, nil
}

func (t *Cortex) initPurger() (services.Service, error) {
	if !t.Cfg.PurgerConfig.Enable {
		return nil, nil
	}

	storageClient, err := storage.NewObjectClient(t.Cfg.PurgerConfig.ObjectStoreType, t.Cfg.Storage)
	if err != nil {
		return nil, err
	}

	t.Purger, err = purger.NewPurger(t.Cfg.PurgerConfig, t.DeletesStore, t.Store, storageClient, prometheus.DefaultRegisterer)
	if err != nil {
		return nil, err
	}

	t.API.RegisterPurger(t.DeletesStore, t.Cfg.PurgerConfig.DeleteRequestCancelPeriod)

	return t.Purger, nil
}

func (t *Cortex) setupModuleManager() error {
	mm := modules.NewManager()

	// Register all modules here.
	// RegisterModule(name string, initFn func()(services.Service, error))
	mm.RegisterModule(Server, t.initServer, modules.UserInvisibleModule)
	mm.RegisterModule(API, t.initAPI, modules.UserInvisibleModule)
	mm.RegisterModule(RuntimeConfig, t.initRuntimeConfig, modules.UserInvisibleModule)
	mm.RegisterModule(MemberlistKV, t.initMemberlistKV, modules.UserInvisibleModule)
	mm.RegisterModule(Ring, t.initRing, modules.UserInvisibleModule)
	mm.RegisterModule(Overrides, t.initOverrides, modules.UserInvisibleModule)
	mm.RegisterModule(Distributor, t.initDistributor)
	mm.RegisterModule(Store, t.initChunkStore, modules.UserInvisibleModule)
	mm.RegisterModule(DeleteRequestsStore, t.initDeleteRequestsStore, modules.UserInvisibleModule)
	mm.RegisterModule(Ingester, t.initIngester)
	mm.RegisterModule(Flusher, t.initFlusher)
	mm.RegisterModule(Querier, t.initQuerier)
	mm.RegisterModule(StoreQueryable, t.initStoreQueryables, modules.UserInvisibleModule)
	mm.RegisterModule(QueryFrontend, t.initQueryFrontend)
	mm.RegisterModule(TableManager, t.initTableManager)
	mm.RegisterModule(RulerStorage, t.initRulerStorage, modules.UserInvisibleModule)
	mm.RegisterModule(Ruler, t.initRuler)
	mm.RegisterModule(Configs, t.initConfig)
	mm.RegisterModule(AlertManager, t.initAlertManager)
	mm.RegisterModule(Compactor, t.initCompactor)
	mm.RegisterModule(StoreGateway, t.initStoreGateway)
	mm.RegisterModule(Purger, t.initPurger)
	mm.RegisterModule(All, nil)

	// Add dependencies
	deps := map[string][]string{
		API:            {Server},
		Ring:           {API, RuntimeConfig, MemberlistKV},
		Overrides:      {RuntimeConfig},
		Distributor:    {Ring, API, Overrides},
		Store:          {Overrides, DeleteRequestsStore},
		Ingester:       {Overrides, Store, API, RuntimeConfig, MemberlistKV},
		Flusher:        {Store, API},
		Querier:        {Overrides, Distributor, Store, Ring, API, StoreQueryable, MemberlistKV},
		StoreQueryable: {Overrides, Store},
		QueryFrontend:  {API, Overrides, DeleteRequestsStore},
		TableManager:   {API},
		Ruler:          {Overrides, Distributor, Store, StoreQueryable, RulerStorage},
		Configs:        {API},
		AlertManager:   {API},
		Compactor:      {API, MemberlistKV},
		StoreGateway:   {API, Overrides, MemberlistKV},
		Purger:         {Store, DeleteRequestsStore, API},
		All:            {QueryFrontend, Querier, Ingester, Distributor, TableManager, Purger, StoreGateway, Ruler},
	}
	for mod, targets := range deps {
		if err := mm.AddDependency(mod, targets...); err != nil {
			return err
		}
	}

	t.ModuleManager = mm

	return nil
}
