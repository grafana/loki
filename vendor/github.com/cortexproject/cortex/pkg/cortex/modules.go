package cortex

import (
	"fmt"
	"os"
	"strings"

	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/promql"
	httpgrpc_server "github.com/weaveworks/common/httpgrpc/server"
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
	"github.com/cortexproject/cortex/pkg/util/runtimeconfig"
	"github.com/cortexproject/cortex/pkg/util/services"
	"github.com/cortexproject/cortex/pkg/util/validation"
)

// ModuleName is used to describe a running module
type ModuleName string

// The various modules that make up Cortex.
const (
	API                 ModuleName = "api"
	Ring                ModuleName = "ring"
	RuntimeConfig       ModuleName = "runtime-config"
	Overrides           ModuleName = "overrides"
	Server              ModuleName = "server"
	Distributor         ModuleName = "distributor"
	Ingester            ModuleName = "ingester"
	Flusher             ModuleName = "flusher"
	Querier             ModuleName = "querier"
	StoreQueryable      ModuleName = "store-queryable"
	QueryFrontend       ModuleName = "query-frontend"
	Store               ModuleName = "store"
	DeleteRequestsStore ModuleName = "delete-requests-store"
	TableManager        ModuleName = "table-manager"
	Ruler               ModuleName = "ruler"
	Configs             ModuleName = "configs"
	AlertManager        ModuleName = "alertmanager"
	Compactor           ModuleName = "compactor"
	StoreGateway        ModuleName = "store-gateway"
	MemberlistKV        ModuleName = "memberlist-kv"
	DataPurger          ModuleName = "data-purger"
	All                 ModuleName = "all"
)

func (m ModuleName) String() string {
	return string(m)
}

func (m *ModuleName) Set(s string) error {
	l := ModuleName(strings.ToLower(s))
	if _, ok := modules[l]; !ok {
		return fmt.Errorf("unrecognised module name: %s", s)
	}
	*m = l
	return nil
}

func (m ModuleName) MarshalYAML() (interface{}, error) {
	return m.String(), nil
}

func (m *ModuleName) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	return m.Set(s)
}

func (t *Cortex) initAPI(cfg *Config) (services.Service, error) {
	cfg.API.ServerPrefix = cfg.Server.PathPrefix
	cfg.API.LegacyHTTPPrefix = cfg.HTTPPrefix

	a, err := api.New(cfg.API, t.server, util.Logger)
	if err != nil {
		return nil, err
	}

	t.api = a

	t.api.RegisterAPI(cfg)

	return nil, nil
}

func (t *Cortex) initServer(cfg *Config) (services.Service, error) {
	serv, err := server.New(cfg.Server)
	if err != nil {
		return nil, err
	}

	t.server = serv

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

	s := NewServerService(t.server, servicesToWaitFor)

	return s, nil
}

func (t *Cortex) initRing(cfg *Config) (serv services.Service, err error) {
	cfg.Ingester.LifecyclerConfig.RingConfig.KVStore.Multi.ConfigProvider = multiClientRuntimeConfigChannel(t.runtimeConfig)
	cfg.Ingester.LifecyclerConfig.RingConfig.KVStore.MemberlistKV = t.memberlistKV.GetMemberlistKV
	t.ring, err = ring.New(cfg.Ingester.LifecyclerConfig.RingConfig, "ingester", ring.IngesterRingKey)
	if err != nil {
		return nil, err
	}
	prometheus.MustRegister(t.ring)

	t.api.RegisterRing(t.ring)

	return t.ring, nil
}

func (t *Cortex) initRuntimeConfig(cfg *Config) (services.Service, error) {
	if cfg.RuntimeConfig.LoadPath == "" {
		cfg.RuntimeConfig.LoadPath = cfg.LimitsConfig.PerTenantOverrideConfig
		cfg.RuntimeConfig.ReloadPeriod = cfg.LimitsConfig.PerTenantOverridePeriod
	}
	cfg.RuntimeConfig.Loader = loadRuntimeConfig

	// make sure to set default limits before we start loading configuration into memory
	validation.SetDefaultLimitsForYAMLUnmarshalling(cfg.LimitsConfig)

	serv, err := runtimeconfig.NewRuntimeConfigManager(cfg.RuntimeConfig, prometheus.DefaultRegisterer)
	t.runtimeConfig = serv
	return serv, err
}

func (t *Cortex) initOverrides(cfg *Config) (serv services.Service, err error) {
	t.overrides, err = validation.NewOverrides(cfg.LimitsConfig, tenantLimitsFromRuntimeConfig(t.runtimeConfig))
	// overrides don't have operational state, nor do they need to do anything more in starting/stopping phase,
	// so there is no need to return any service.
	return nil, err
}

func (t *Cortex) initDistributor(cfg *Config) (serv services.Service, err error) {
	cfg.Distributor.DistributorRing.ListenPort = cfg.Server.GRPCListenPort
	cfg.Distributor.DistributorRing.KVStore.MemberlistKV = t.memberlistKV.GetMemberlistKV

	// Check whether the distributor can join the distributors ring, which is
	// whenever it's not running as an internal dependency (ie. querier or
	// ruler's dependency)
	canJoinDistributorsRing := (cfg.Target == All || cfg.Target == Distributor)

	t.distributor, err = distributor.New(cfg.Distributor, cfg.IngesterClient, t.overrides, t.ring, canJoinDistributorsRing)
	if err != nil {
		return
	}

	t.api.RegisterDistributor(t.distributor, cfg.Distributor)

	return t.distributor, nil
}

func (t *Cortex) initQuerier(cfg *Config) (serv services.Service, err error) {
	queryable, engine := querier.New(cfg.Querier, t.distributor, t.storeQueryable, t.tombstonesLoader, prometheus.DefaultRegisterer)

	// if we are not configured for single binary mode then the querier needs to register its paths externally
	registerExternally := cfg.Target != All
	handler := t.api.RegisterQuerier(queryable, engine, t.distributor, registerExternally, t.tombstonesLoader)

	// single binary mode requires a properly configured worker.  if the operator did not attempt to configure the
	//  worker we will attempt an automatic configuration here
	if cfg.Worker.Address == "" && cfg.Target == All {
		address := fmt.Sprintf("127.0.0.1:%d", cfg.Server.GRPCListenPort)
		level.Warn(util.Logger).Log("msg", "Worker address is empty in single binary mode.  Attempting automatic worker configuration.  If queries are unresponsive consider configuring the worker explicitly.", "address", address)
		cfg.Worker.Address = address
	}

	// Query frontend worker will only be started after all its dependencies are started, not here.
	// Worker may also be nil, if not configured, which is OK.
	worker, err := frontend.NewWorker(cfg.Worker, cfg.Querier, httpgrpc_server.NewServer(handler), util.Logger)
	if err != nil {
		return
	}

	return worker, nil
}

func (t *Cortex) initStoreQueryable(cfg *Config) (services.Service, error) {
	if cfg.Storage.Engine == storage.StorageEngineChunks {
		t.storeQueryable = querier.NewChunkStoreQueryable(cfg.Querier, t.store)
		return nil, nil
	}

	if cfg.Storage.Engine == storage.StorageEngineTSDB && !cfg.TSDB.StoreGatewayEnabled {
		storeQueryable, err := querier.NewBlockQueryable(cfg.TSDB, cfg.Server.LogLevel, prometheus.DefaultRegisterer)
		if err != nil {
			return nil, err
		}
		t.storeQueryable = storeQueryable
		return storeQueryable, nil
	}

	if cfg.Storage.Engine == storage.StorageEngineTSDB && cfg.TSDB.StoreGatewayEnabled {
		// When running in single binary, if the blocks sharding is disabled and no custom
		// store-gateway address has been configured, we can set it to the running process.
		if cfg.Target == All && !cfg.StoreGateway.ShardingEnabled && cfg.Querier.StoreGatewayAddresses == "" {
			cfg.Querier.StoreGatewayAddresses = fmt.Sprintf("127.0.0.1:%d", cfg.Server.GRPCListenPort)
		}

		storeQueryable, err := querier.NewBlocksStoreQueryableFromConfig(cfg.Querier, cfg.StoreGateway, cfg.TSDB, util.Logger, prometheus.DefaultRegisterer)
		if err != nil {
			return nil, err
		}
		t.storeQueryable = storeQueryable
		return storeQueryable, nil
	}

	return nil, fmt.Errorf("unknown storage engine '%s'", cfg.Storage.Engine)
}

func (t *Cortex) initIngester(cfg *Config) (serv services.Service, err error) {
	cfg.Ingester.LifecyclerConfig.RingConfig.KVStore.Multi.ConfigProvider = multiClientRuntimeConfigChannel(t.runtimeConfig)
	cfg.Ingester.LifecyclerConfig.RingConfig.KVStore.MemberlistKV = t.memberlistKV.GetMemberlistKV
	cfg.Ingester.LifecyclerConfig.ListenPort = cfg.Server.GRPCListenPort
	cfg.Ingester.TSDBEnabled = cfg.Storage.Engine == storage.StorageEngineTSDB
	cfg.Ingester.TSDBConfig = cfg.TSDB
	cfg.Ingester.ShardByAllLabels = cfg.Distributor.ShardByAllLabels

	t.ingester, err = ingester.New(cfg.Ingester, cfg.IngesterClient, t.overrides, t.store, prometheus.DefaultRegisterer)
	if err != nil {
		return
	}

	t.api.RegisterIngester(t.ingester, cfg.Distributor)

	return t.ingester, nil
}

func (t *Cortex) initFlusher(cfg *Config) (serv services.Service, err error) {
	t.flusher, err = flusher.New(
		cfg.Flusher,
		cfg.Ingester,
		cfg.IngesterClient,
		t.store,
		prometheus.DefaultRegisterer,
	)
	if err != nil {
		return
	}

	return t.flusher, nil
}

func (t *Cortex) initStore(cfg *Config) (serv services.Service, err error) {
	if cfg.Storage.Engine == storage.StorageEngineTSDB {
		return nil, nil
	}

	err = cfg.Schema.Load()
	if err != nil {
		return
	}

	t.store, err = storage.NewStore(cfg.Storage, cfg.ChunkStore, cfg.Schema, t.overrides, prometheus.DefaultRegisterer, t.tombstonesLoader)
	if err != nil {
		return
	}

	return services.NewIdleService(nil, func(_ error) error {
		t.store.Stop()
		return nil
	}), nil
}

func (t *Cortex) initDeleteRequestsStore(cfg *Config) (serv services.Service, err error) {
	if !cfg.DataPurgerConfig.Enable {
		// until we need to explicitly enable delete series support we need to do create TombstonesLoader without DeleteStore which acts as noop
		t.tombstonesLoader = purger.NewTombstonesLoader(nil, nil)

		return
	}

	var indexClient chunk.IndexClient
	indexClient, err = storage.NewIndexClient(cfg.Storage.DeleteStoreConfig.Store, cfg.Storage, cfg.Schema)
	if err != nil {
		return
	}

	t.deletesStore, err = purger.NewDeleteStore(cfg.Storage.DeleteStoreConfig, indexClient)
	if err != nil {
		return
	}

	t.tombstonesLoader = purger.NewTombstonesLoader(t.deletesStore, prometheus.DefaultRegisterer)

	return
}

func (t *Cortex) initQueryFrontend(cfg *Config) (serv services.Service, err error) {
	// Load the schema only if sharded queries is set.
	if cfg.QueryRange.ShardedQueries {
		err = cfg.Schema.Load()
		if err != nil {
			return
		}
	}

	t.frontend, err = frontend.New(cfg.Frontend, util.Logger, prometheus.DefaultRegisterer)
	if err != nil {
		return
	}

	tripperware, cache, err := queryrange.NewTripperware(
		cfg.QueryRange,
		util.Logger,
		t.overrides,
		queryrange.PrometheusCodec,
		queryrange.PrometheusResponseExtractor{},
		cfg.Schema,
		promql.EngineOpts{
			Logger:     util.Logger,
			Reg:        prometheus.DefaultRegisterer,
			MaxSamples: cfg.Querier.MaxSamples,
			Timeout:    cfg.Querier.Timeout,
		},
		cfg.Querier.QueryIngestersWithin,
		prometheus.DefaultRegisterer,
		t.tombstonesLoader,
	)

	if err != nil {
		return nil, err
	}
	t.cache = cache
	t.frontend.Wrap(tripperware)

	t.api.RegisterQueryFrontend(t.frontend)

	return services.NewIdleService(nil, func(_ error) error {
		t.frontend.Close()
		if t.cache != nil {
			t.cache.Stop()
			t.cache = nil
		}
		return nil
	}), nil
}

func (t *Cortex) initTableManager(cfg *Config) (services.Service, error) {
	if cfg.Storage.Engine == storage.StorageEngineTSDB {
		return nil, nil // table manager isn't used in v2
	}

	err := cfg.Schema.Load()
	if err != nil {
		return nil, err
	}

	// Assume the newest config is the one to use
	lastConfig := &cfg.Schema.Configs[len(cfg.Schema.Configs)-1]

	if (cfg.TableManager.ChunkTables.WriteScale.Enabled ||
		cfg.TableManager.IndexTables.WriteScale.Enabled ||
		cfg.TableManager.ChunkTables.InactiveWriteScale.Enabled ||
		cfg.TableManager.IndexTables.InactiveWriteScale.Enabled ||
		cfg.TableManager.ChunkTables.ReadScale.Enabled ||
		cfg.TableManager.IndexTables.ReadScale.Enabled ||
		cfg.TableManager.ChunkTables.InactiveReadScale.Enabled ||
		cfg.TableManager.IndexTables.InactiveReadScale.Enabled) &&
		cfg.Storage.AWSStorageConfig.Metrics.URL == "" {
		level.Error(util.Logger).Log("msg", "WriteScale is enabled but no Metrics URL has been provided")
		os.Exit(1)
	}

	tableClient, err := storage.NewTableClient(lastConfig.IndexType, cfg.Storage)
	if err != nil {
		return nil, err
	}

	bucketClient, err := storage.NewBucketClient(cfg.Storage)
	util.CheckFatal("initializing bucket client", err)

	t.tableManager, err = chunk.NewTableManager(cfg.TableManager, cfg.Schema, cfg.Ingester.MaxChunkAge, tableClient, bucketClient, prometheus.DefaultRegisterer)
	return t.tableManager, err
}

func (t *Cortex) initRuler(cfg *Config) (serv services.Service, err error) {
	cfg.Ruler.Ring.ListenPort = cfg.Server.GRPCListenPort
	cfg.Ruler.Ring.KVStore.MemberlistKV = t.memberlistKV.GetMemberlistKV
	queryable, engine := querier.New(cfg.Querier, t.distributor, t.storeQueryable, t.tombstonesLoader, prometheus.DefaultRegisterer)

	t.ruler, err = ruler.NewRuler(cfg.Ruler, engine, queryable, t.distributor, prometheus.DefaultRegisterer, util.Logger)
	if err != nil {
		return
	}

	// Expose HTTP endpoints.
	t.api.RegisterRuler(t.ruler, cfg.Ruler.EnableAPI)

	return t.ruler, nil
}

func (t *Cortex) initConfig(cfg *Config) (serv services.Service, err error) {
	t.configDB, err = db.New(cfg.Configs.DB)
	if err != nil {
		return
	}

	t.configAPI = configAPI.New(t.configDB, cfg.Configs.API)
	t.configAPI.RegisterRoutes(t.server.HTTP)
	return services.NewIdleService(nil, func(_ error) error {
		t.configDB.Close()
		return nil
	}), nil
}

func (t *Cortex) initAlertManager(cfg *Config) (serv services.Service, err error) {
	t.alertmanager, err = alertmanager.NewMultitenantAlertmanager(&cfg.Alertmanager, util.Logger, prometheus.DefaultRegisterer)
	if err != nil {
		return
	}
	t.api.RegisterAlertmanager(t.alertmanager, cfg.Target == AlertManager)
	return t.alertmanager, nil
}

func (t *Cortex) initCompactor(cfg *Config) (serv services.Service, err error) {
	cfg.Compactor.ShardingRing.ListenPort = cfg.Server.GRPCListenPort
	cfg.Compactor.ShardingRing.KVStore.MemberlistKV = t.memberlistKV.GetMemberlistKV

	t.compactor, err = compactor.NewCompactor(cfg.Compactor, cfg.TSDB, util.Logger, prometheus.DefaultRegisterer)
	if err != nil {
		return
	}

	// Expose HTTP endpoints.
	t.api.RegisterCompactor(t.compactor)
	return t.compactor, nil
}

func (t *Cortex) initStoreGateway(cfg *Config) (serv services.Service, err error) {
	if cfg.Storage.Engine != storage.StorageEngineTSDB {
		return nil, nil
	}

	cfg.StoreGateway.ShardingRing.ListenPort = cfg.Server.GRPCListenPort
	cfg.StoreGateway.ShardingRing.KVStore.MemberlistKV = t.memberlistKV.GetMemberlistKV

	t.storeGateway, err = storegateway.NewStoreGateway(cfg.StoreGateway, cfg.TSDB, cfg.Server.LogLevel, util.Logger, prometheus.DefaultRegisterer)
	if err != nil {
		return nil, err
	}

	// Expose HTTP endpoints.
	t.api.RegisterStoreGateway(t.storeGateway)

	return t.storeGateway, nil
}

func (t *Cortex) initMemberlistKV(cfg *Config) (services.Service, error) {
	cfg.MemberlistKV.MetricsRegisterer = prometheus.DefaultRegisterer
	cfg.MemberlistKV.Codecs = []codec.Codec{
		ring.GetCodec(),
	}
	t.memberlistKV = memberlist.NewKVInit(&cfg.MemberlistKV)

	return services.NewIdleService(nil, func(_ error) error {
		t.memberlistKV.Stop()
		return nil
	}), nil
}

func (t *Cortex) initDataPurger(cfg *Config) (services.Service, error) {
	if !cfg.DataPurgerConfig.Enable {
		return nil, nil
	}

	storageClient, err := storage.NewObjectClient(cfg.DataPurgerConfig.ObjectStoreType, cfg.Storage)
	if err != nil {
		return nil, err
	}

	t.dataPurger, err = purger.NewDataPurger(cfg.DataPurgerConfig, t.deletesStore, t.store, storageClient, prometheus.DefaultRegisterer)
	if err != nil {
		return nil, err
	}

	t.api.RegisterPurger(t.deletesStore)

	return t.dataPurger, nil
}

type module struct {
	deps []ModuleName

	// service for this module (can return nil)
	service func(t *Cortex, cfg *Config) (services.Service, error)

	// service that will be wrapped into moduleServiceWrapper, to wait for dependencies to start / end
	// (can return nil)
	wrappedService func(t *Cortex, cfg *Config) (services.Service, error)
}

var modules = map[ModuleName]module{
	Server: {
		// we cannot use 'wrappedService', as stopped Server service is currently a signal to Cortex
		// that it should shutdown. If we used wrappedService, it wouldn't stop until
		// all services that depend on it stopped first... but there is nothing that would make them stop.
		service: (*Cortex).initServer,
	},

	API: {
		deps:           []ModuleName{Server},
		wrappedService: (*Cortex).initAPI,
	},

	RuntimeConfig: {
		wrappedService: (*Cortex).initRuntimeConfig,
	},

	MemberlistKV: {
		wrappedService: (*Cortex).initMemberlistKV,
	},

	Ring: {
		deps:           []ModuleName{API, RuntimeConfig, MemberlistKV},
		wrappedService: (*Cortex).initRing,
	},

	Overrides: {
		deps:           []ModuleName{RuntimeConfig},
		wrappedService: (*Cortex).initOverrides,
	},

	Distributor: {
		deps:           []ModuleName{Ring, API, Overrides},
		wrappedService: (*Cortex).initDistributor,
	},

	Store: {
		deps:           []ModuleName{Overrides, DeleteRequestsStore},
		wrappedService: (*Cortex).initStore,
	},

	DeleteRequestsStore: {
		wrappedService: (*Cortex).initDeleteRequestsStore,
	},

	Ingester: {
		deps:           []ModuleName{Overrides, Store, API, RuntimeConfig, MemberlistKV},
		wrappedService: (*Cortex).initIngester,
	},

	Flusher: {
		deps:           []ModuleName{Store, API},
		wrappedService: (*Cortex).initFlusher,
	},

	Querier: {
		deps:           []ModuleName{Distributor, Store, Ring, API, StoreQueryable},
		wrappedService: (*Cortex).initQuerier,
	},

	StoreQueryable: {
		deps:           []ModuleName{Store},
		wrappedService: (*Cortex).initStoreQueryable,
	},

	QueryFrontend: {
		deps:           []ModuleName{API, Overrides, DeleteRequestsStore},
		wrappedService: (*Cortex).initQueryFrontend,
	},

	TableManager: {
		deps:           []ModuleName{API},
		wrappedService: (*Cortex).initTableManager,
	},

	Ruler: {
		deps:           []ModuleName{Distributor, Store, StoreQueryable},
		wrappedService: (*Cortex).initRuler,
	},

	Configs: {
		deps:           []ModuleName{API},
		wrappedService: (*Cortex).initConfig,
	},

	AlertManager: {
		deps:           []ModuleName{API},
		wrappedService: (*Cortex).initAlertManager,
	},

	Compactor: {
		deps:           []ModuleName{API},
		wrappedService: (*Cortex).initCompactor,
	},

	StoreGateway: {
		deps:           []ModuleName{API},
		wrappedService: (*Cortex).initStoreGateway,
	},

	DataPurger: {
		deps:           []ModuleName{Store, DeleteRequestsStore, API},
		wrappedService: (*Cortex).initDataPurger,
	},

	All: {
		deps: []ModuleName{QueryFrontend, Querier, Ingester, Distributor, TableManager, DataPurger, StoreGateway},
	},
}
