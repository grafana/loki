package loki

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"

	"github.com/grafana/loki/pkg/storage/stores/shipper/compactor"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/cache"
	"github.com/cortexproject/cortex/pkg/chunk/storage"
	"github.com/cortexproject/cortex/pkg/cortex"
	cortex_querier "github.com/cortexproject/cortex/pkg/querier"
	"github.com/cortexproject/cortex/pkg/querier/frontend"
	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/ring/kv/codec"
	"github.com/cortexproject/cortex/pkg/ring/kv/memberlist"
	cortex_ruler "github.com/cortexproject/cortex/pkg/ruler"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/runtimeconfig"
	"github.com/cortexproject/cortex/pkg/util/services"

	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	httpgrpc_server "github.com/weaveworks/common/httpgrpc/server"
	"github.com/weaveworks/common/middleware"
	"github.com/weaveworks/common/server"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/pkg/distributor"
	"github.com/grafana/loki/pkg/ingester"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/querier"
	"github.com/grafana/loki/pkg/querier/queryrange"
	"github.com/grafana/loki/pkg/ruler"
	loki_storage "github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/stores/shipper"
	serverutil "github.com/grafana/loki/pkg/util/server"
	"github.com/grafana/loki/pkg/util/validation"
)

const maxChunkAgeForTableManager = 12 * time.Hour

// The various modules that make up Loki.
const (
	Ring          string = "ring"
	RuntimeConfig string = "runtime-config"
	Overrides     string = "overrides"
	Server        string = "server"
	Distributor   string = "distributor"
	Ingester      string = "ingester"
	Querier       string = "querier"
	QueryFrontend string = "query-frontend"
	RulerStorage  string = "ruler-storage"
	Ruler         string = "ruler"
	Store         string = "store"
	TableManager  string = "table-manager"
	MemberlistKV  string = "memberlist-kv"
	Compactor     string = "compactor"
	All           string = "all"
)

func (t *Loki) initServer() (services.Service, error) {
	// Loki handles signals on its own.
	cortex.DisableSignalHandling(&t.cfg.Server)
	serv, err := server.New(t.cfg.Server)
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

	s := cortex.NewServerService(t.server, servicesToWaitFor)

	return s, nil
}

func (t *Loki) initRing() (_ services.Service, err error) {
	t.cfg.Ingester.LifecyclerConfig.RingConfig.KVStore.Multi.ConfigProvider = multiClientRuntimeConfigChannel(t.runtimeConfig)
	t.cfg.Ingester.LifecyclerConfig.RingConfig.KVStore.MemberlistKV = t.memberlistKV.GetMemberlistKV
	t.ring, err = ring.New(t.cfg.Ingester.LifecyclerConfig.RingConfig, "ingester", ring.IngesterRingKey, prometheus.DefaultRegisterer)
	if err != nil {
		return
	}
	prometheus.MustRegister(t.ring)
	t.server.HTTP.Handle("/ring", t.ring)
	return t.ring, nil
}

func (t *Loki) initRuntimeConfig() (services.Service, error) {
	if t.cfg.RuntimeConfig.LoadPath == "" {
		t.cfg.RuntimeConfig.LoadPath = t.cfg.LimitsConfig.PerTenantOverrideConfig
		t.cfg.RuntimeConfig.ReloadPeriod = t.cfg.LimitsConfig.PerTenantOverridePeriod
	}

	if t.cfg.RuntimeConfig.LoadPath == "" {
		// no need to initialize module if load path is empty
		return nil, nil
	}

	t.cfg.RuntimeConfig.Loader = loadRuntimeConfig

	// make sure to set default limits before we start loading configuration into memory
	validation.SetDefaultLimitsForYAMLUnmarshalling(t.cfg.LimitsConfig)

	var err error
	t.runtimeConfig, err = runtimeconfig.NewRuntimeConfigManager(t.cfg.RuntimeConfig, prometheus.DefaultRegisterer)
	return t.runtimeConfig, err
}

func (t *Loki) initOverrides() (_ services.Service, err error) {
	t.overrides, err = validation.NewOverrides(t.cfg.LimitsConfig, tenantLimitsFromRuntimeConfig(t.runtimeConfig))
	// overrides are not a service, since they don't have any operational state.
	return nil, err
}

func (t *Loki) initDistributor() (services.Service, error) {
	t.cfg.Distributor.DistributorRing.KVStore.Multi.ConfigProvider = multiClientRuntimeConfigChannel(t.runtimeConfig)
	t.cfg.Distributor.DistributorRing.KVStore.MemberlistKV = t.memberlistKV.GetMemberlistKV
	var err error
	t.distributor, err = distributor.New(t.cfg.Distributor, t.cfg.IngesterClient, t.ring, t.overrides, prometheus.DefaultRegisterer)
	if err != nil {
		return nil, err
	}

	pushHandler := middleware.Merge(
		serverutil.RecoveryHTTPMiddleware,
		t.httpAuthMiddleware,
	).Wrap(http.HandlerFunc(t.distributor.PushHandler))

	t.server.HTTP.Handle("/api/prom/push", pushHandler)
	t.server.HTTP.Handle("/loki/api/v1/push", pushHandler)
	return t.distributor, nil
}

func (t *Loki) initQuerier() (services.Service, error) {
	level.Debug(util.Logger).Log("msg", "initializing querier worker", "config", fmt.Sprintf("%+v", t.cfg.Worker))
	worker, err := frontend.NewWorker(t.cfg.Worker, cortex_querier.Config{MaxConcurrent: t.cfg.Querier.MaxConcurrent}, httpgrpc_server.NewServer(t.server.HTTPServer.Handler), util.Logger)
	if err != nil {
		return nil, err
	}
	if t.cfg.Ingester.QueryStoreMaxLookBackPeriod != 0 {
		t.cfg.Querier.IngesterQueryStoreMaxLookback = t.cfg.Ingester.QueryStoreMaxLookBackPeriod
	}
	t.querier, err = querier.New(t.cfg.Querier, t.cfg.IngesterClient, t.ring, t.store, t.overrides)
	if err != nil {
		return nil, err
	}

	httpMiddleware := middleware.Merge(
		serverutil.RecoveryHTTPMiddleware,
		t.httpAuthMiddleware,
		serverutil.NewPrepopulateMiddleware(),
	)
	t.server.HTTP.Handle("/loki/api/v1/query_range", httpMiddleware.Wrap(http.HandlerFunc(t.querier.RangeQueryHandler)))
	t.server.HTTP.Handle("/loki/api/v1/query", httpMiddleware.Wrap(http.HandlerFunc(t.querier.InstantQueryHandler)))
	// Prometheus compatibility requires `loki/api/v1/labels` however we already released `loki/api/v1/label`
	// which is a little more consistent with `/loki/api/v1/label/{name}/values` so we are going to handle both paths.
	t.server.HTTP.Handle("/loki/api/v1/label", httpMiddleware.Wrap(http.HandlerFunc(t.querier.LabelHandler)))
	t.server.HTTP.Handle("/loki/api/v1/labels", httpMiddleware.Wrap(http.HandlerFunc(t.querier.LabelHandler)))
	t.server.HTTP.Handle("/loki/api/v1/label/{name}/values", httpMiddleware.Wrap(http.HandlerFunc(t.querier.LabelHandler)))
	t.server.HTTP.Handle("/loki/api/v1/tail", httpMiddleware.Wrap(http.HandlerFunc(t.querier.TailHandler)))
	t.server.HTTP.Handle("/loki/api/v1/series", httpMiddleware.Wrap(http.HandlerFunc(t.querier.SeriesHandler)))

	t.server.HTTP.Handle("/api/prom/query", httpMiddleware.Wrap(http.HandlerFunc(t.querier.LogQueryHandler)))
	t.server.HTTP.Handle("/api/prom/label", httpMiddleware.Wrap(http.HandlerFunc(t.querier.LabelHandler)))
	t.server.HTTP.Handle("/api/prom/label/{name}/values", httpMiddleware.Wrap(http.HandlerFunc(t.querier.LabelHandler)))
	t.server.HTTP.Handle("/api/prom/tail", httpMiddleware.Wrap(http.HandlerFunc(t.querier.TailHandler)))
	t.server.HTTP.Handle("/api/prom/series", httpMiddleware.Wrap(http.HandlerFunc(t.querier.SeriesHandler)))
	return worker, nil // ok if worker is nil here
}

func (t *Loki) initIngester() (_ services.Service, err error) {
	t.cfg.Ingester.LifecyclerConfig.RingConfig.KVStore.Multi.ConfigProvider = multiClientRuntimeConfigChannel(t.runtimeConfig)
	t.cfg.Ingester.LifecyclerConfig.RingConfig.KVStore.MemberlistKV = t.memberlistKV.GetMemberlistKV
	t.cfg.Ingester.LifecyclerConfig.ListenPort = t.cfg.Server.GRPCListenPort

	// We want ingester to also query the store when using boltdb-shipper
	pc := t.cfg.SchemaConfig.Configs[loki_storage.ActivePeriodConfig(t.cfg.SchemaConfig)]
	if pc.IndexType == shipper.BoltDBShipperType {
		t.cfg.Ingester.QueryStore = true
		mlb, err := calculateMaxLookBack(pc, t.cfg.Ingester.QueryStoreMaxLookBackPeriod, t.cfg.Ingester.MaxChunkAge)
		if err != nil {
			return nil, err
		}
		t.cfg.Ingester.QueryStoreMaxLookBackPeriod = mlb
	}

	t.ingester, err = ingester.New(t.cfg.Ingester, t.cfg.IngesterClient, t.store, t.overrides, prometheus.DefaultRegisterer)
	if err != nil {
		return
	}

	logproto.RegisterPusherServer(t.server.GRPC, t.ingester)
	logproto.RegisterQuerierServer(t.server.GRPC, t.ingester)
	logproto.RegisterIngesterServer(t.server.GRPC, t.ingester)
	grpc_health_v1.RegisterHealthServer(t.server.GRPC, t.ingester)
	t.server.HTTP.Path("/flush").Handler(http.HandlerFunc(t.ingester.FlushHandler))
	return t.ingester, nil
}

func (t *Loki) initTableManager() (services.Service, error) {
	err := t.cfg.SchemaConfig.Load()
	if err != nil {
		return nil, err
	}

	// Assume the newest config is the one to use
	lastConfig := &t.cfg.SchemaConfig.Configs[len(t.cfg.SchemaConfig.Configs)-1]

	if (t.cfg.TableManager.ChunkTables.WriteScale.Enabled ||
		t.cfg.TableManager.IndexTables.WriteScale.Enabled ||
		t.cfg.TableManager.ChunkTables.InactiveWriteScale.Enabled ||
		t.cfg.TableManager.IndexTables.InactiveWriteScale.Enabled ||
		t.cfg.TableManager.ChunkTables.ReadScale.Enabled ||
		t.cfg.TableManager.IndexTables.ReadScale.Enabled ||
		t.cfg.TableManager.ChunkTables.InactiveReadScale.Enabled ||
		t.cfg.TableManager.IndexTables.InactiveReadScale.Enabled) &&
		t.cfg.StorageConfig.AWSStorageConfig.Metrics.URL == "" {
		level.Error(util.Logger).Log("msg", "WriteScale is enabled but no Metrics URL has been provided")
		os.Exit(1)
	}

	reg := prometheus.WrapRegistererWith(prometheus.Labels{"component": "table-manager-store"}, prometheus.DefaultRegisterer)

	tableClient, err := storage.NewTableClient(lastConfig.IndexType, t.cfg.StorageConfig.Config, reg)
	if err != nil {
		return nil, err
	}

	bucketClient, err := storage.NewBucketClient(t.cfg.StorageConfig.Config)
	util.CheckFatal("initializing bucket client", err)

	t.tableManager, err = chunk.NewTableManager(t.cfg.TableManager, t.cfg.SchemaConfig.SchemaConfig, maxChunkAgeForTableManager, tableClient, bucketClient, nil, prometheus.DefaultRegisterer)
	if err != nil {
		return nil, err
	}

	return t.tableManager, nil
}

func (t *Loki) initStore() (_ services.Service, err error) {
	if t.cfg.SchemaConfig.Configs[loki_storage.ActivePeriodConfig(t.cfg.SchemaConfig)].IndexType == shipper.BoltDBShipperType {
		t.cfg.StorageConfig.BoltDBShipperConfig.IngesterName = t.cfg.Ingester.LifecyclerConfig.ID
		switch t.cfg.Target {
		case Ingester:
			// We do not want ingester to unnecessarily keep downloading files
			t.cfg.StorageConfig.BoltDBShipperConfig.Mode = shipper.ModeWriteOnly
		case Querier:
			// We do not want query to do any updates to index
			t.cfg.StorageConfig.BoltDBShipperConfig.Mode = shipper.ModeReadOnly
		default:
			t.cfg.StorageConfig.BoltDBShipperConfig.Mode = shipper.ModeReadWrite
		}
	}

	// If RF > 1 and current or upcoming index type is boltdb-shipper then disable index dedupe and write dedupe cache.
	// This is to ensure that index entries are replicated to all the boltdb files in ingesters flushing replicated data.
	if t.cfg.Ingester.LifecyclerConfig.RingConfig.ReplicationFactor > 1 && loki_storage.UsingBoltdbShipper(t.cfg.SchemaConfig) {
		t.cfg.ChunkStoreConfig.DisableIndexDeduplication = true
		t.cfg.ChunkStoreConfig.WriteDedupeCacheConfig = cache.Config{}
	}

	t.store, err = loki_storage.NewStore(t.cfg.StorageConfig, t.cfg.ChunkStoreConfig, t.cfg.SchemaConfig, t.overrides, prometheus.DefaultRegisterer)
	if err != nil {
		return
	}

	return services.NewIdleService(nil, func(_ error) error {
		t.store.Stop()
		return nil
	}), nil
}

func (t *Loki) initQueryFrontend() (_ services.Service, err error) {
	level.Debug(util.Logger).Log("msg", "initializing query frontend", "config", fmt.Sprintf("%+v", t.cfg.Frontend))
	t.frontend, err = frontend.New(t.cfg.Frontend.Config, util.Logger, prometheus.DefaultRegisterer)
	if err != nil {
		return
	}
	level.Debug(util.Logger).Log("msg", "initializing query range tripperware",
		"config", fmt.Sprintf("%+v", t.cfg.QueryRange),
		"limits", fmt.Sprintf("%+v", t.cfg.LimitsConfig),
	)
	tripperware, stopper, err := queryrange.NewTripperware(
		t.cfg.QueryRange,
		util.Logger,
		t.overrides,
		t.cfg.SchemaConfig.SchemaConfig,
		t.cfg.Querier.QueryIngestersWithin,
		prometheus.DefaultRegisterer,
	)
	if err != nil {
		return
	}
	t.stopper = stopper
	t.frontend.Wrap(tripperware)
	frontend.RegisterFrontendServer(t.server.GRPC, t.frontend)

	frontendHandler := middleware.Merge(
		serverutil.RecoveryHTTPMiddleware,
		queryrange.StatsHTTPMiddleware,
		t.httpAuthMiddleware,
		serverutil.NewPrepopulateMiddleware(),
	).Wrap(t.frontend.Handler())

	var defaultHandler http.Handler
	if t.cfg.Frontend.TailProxyUrl != "" {
		httpMiddleware := middleware.Merge(
			t.httpAuthMiddleware,
			queryrange.StatsHTTPMiddleware,
		)
		tailURL, err := url.Parse(t.cfg.Frontend.TailProxyUrl)
		if err != nil {
			return nil, err
		}
		tp := httputil.NewSingleHostReverseProxy(tailURL)
		defaultHandler = httpMiddleware.Wrap(tp)
	} else {
		defaultHandler = frontendHandler
	}
	t.server.HTTP.Handle("/loki/api/v1/query_range", frontendHandler)
	t.server.HTTP.Handle("/loki/api/v1/query", frontendHandler)
	t.server.HTTP.Handle("/loki/api/v1/label", frontendHandler)
	t.server.HTTP.Handle("/loki/api/v1/labels", frontendHandler)
	t.server.HTTP.Handle("/loki/api/v1/label/{name}/values", frontendHandler)
	t.server.HTTP.Handle("/loki/api/v1/series", frontendHandler)
	t.server.HTTP.Handle("/api/prom/query", frontendHandler)
	t.server.HTTP.Handle("/api/prom/label", frontendHandler)
	t.server.HTTP.Handle("/api/prom/label/{name}/values", frontendHandler)
	t.server.HTTP.Handle("/api/prom/series", frontendHandler)
	// fallback route
	t.server.HTTP.PathPrefix("/").Handler(defaultHandler)

	return services.NewIdleService(nil, func(_ error) error {
		t.frontend.Close()
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
	if t.cfg.Target == All && t.cfg.Ruler.StoreConfig.IsDefaults() {
		level.Info(util.Logger).Log("msg", "RulerStorage is not configured in single binary mode and will not be started.")
		return
	}

	t.RulerStorage, err = cortex_ruler.NewRuleStorage(t.cfg.Ruler.StoreConfig)

	return
}

func (t *Loki) initRuler() (_ services.Service, err error) {
	if t.RulerStorage == nil {
		level.Info(util.Logger).Log("msg", "RulerStorage is nil.  Not starting the ruler.")
		return nil, nil
	}

	t.cfg.Ruler.Ring.ListenPort = t.cfg.Server.GRPCListenPort
	t.cfg.Ruler.Ring.KVStore.MemberlistKV = t.memberlistKV.GetMemberlistKV
	q, err := querier.New(t.cfg.Querier, t.cfg.IngesterClient, t.ring, t.store, t.overrides)
	if err != nil {
		return nil, err
	}

	engine := logql.NewEngine(t.cfg.Querier.Engine, q)

	t.ruler, err = ruler.NewRuler(
		t.cfg.Ruler,
		engine,
		prometheus.DefaultRegisterer,
		util.Logger,
		t.RulerStorage,
	)

	if err != nil {
		return
	}

	// Expose HTTP endpoints.
	if t.cfg.Ruler.EnableAPI {

		t.server.HTTP.Handle("/ruler/ring", t.ruler)
		cortex_ruler.RegisterRulerServer(t.server.GRPC, t.ruler)

		// Ruler Legacy API Routes
		t.server.HTTP.Path("/api/prom/rules").Methods("GET").Handler(t.httpAuthMiddleware.Wrap(http.HandlerFunc(t.ruler.ListRules)))
		t.server.HTTP.Path("/api/prom/rules/{namespace}").Methods("GET").Handler(t.httpAuthMiddleware.Wrap(http.HandlerFunc(t.ruler.ListRules)))
		t.server.HTTP.Path("/api/prom/rules/{namespace}/{groupName}").Methods("GET").Handler(t.httpAuthMiddleware.Wrap(http.HandlerFunc(t.ruler.GetRuleGroup)))
		t.server.HTTP.Path("/api/prom/rules/{namespace}").Methods("POST").Handler(t.httpAuthMiddleware.Wrap(http.HandlerFunc(t.ruler.CreateRuleGroup)))
		t.server.HTTP.Path("/api/prom/rules/{namespace}/{groupName}").Methods("DELETE").Handler(t.httpAuthMiddleware.Wrap(http.HandlerFunc(t.ruler.DeleteRuleGroup)))

		// Ruler API Routes
		t.server.HTTP.Path("/loki/api/v1/rules").Methods("GET").Handler(t.httpAuthMiddleware.Wrap(http.HandlerFunc(t.ruler.ListRules)))
		t.server.HTTP.Path("/loki/api/v1/rules/{namespace}").Methods("GET").Handler(t.httpAuthMiddleware.Wrap(http.HandlerFunc(t.ruler.ListRules)))
		t.server.HTTP.Path("/loki/api/v1/rules/{namespace}/{groupName}").Methods("GET").Handler(t.httpAuthMiddleware.Wrap(http.HandlerFunc(t.ruler.GetRuleGroup)))
		t.server.HTTP.Path("/loki/api/v1/rules/{namespace}").Methods("POST").Handler(t.httpAuthMiddleware.Wrap(http.HandlerFunc(t.ruler.CreateRuleGroup)))
		t.server.HTTP.Path("/loki/api/v1/rules/{namespace}/{groupName}").Methods("DELETE").Handler(t.httpAuthMiddleware.Wrap(http.HandlerFunc(t.ruler.DeleteRuleGroup)))
	}

	return t.ruler, nil
}

func (t *Loki) initMemberlistKV() (services.Service, error) {
	t.cfg.MemberlistKV.MetricsRegisterer = prometheus.DefaultRegisterer
	t.cfg.MemberlistKV.Codecs = []codec.Codec{
		ring.GetCodec(),
	}

	t.memberlistKV = memberlist.NewKVInitService(&t.cfg.MemberlistKV)
	return t.memberlistKV, nil
}

func (t *Loki) initCompactor() (services.Service, error) {
	var err error
	t.compactor, err = compactor.NewCompactor(t.cfg.CompactorConfig, t.cfg.StorageConfig.Config, prometheus.DefaultRegisterer)
	if err != nil {
		return nil, err
	}

	return t.compactor, nil
}

func calculateMaxLookBack(pc chunk.PeriodConfig, maxLookBackConfig, maxChunkAge time.Duration) (time.Duration, error) {
	if pc.ObjectType != shipper.FilesystemObjectStoreType && maxLookBackConfig.Nanoseconds() != 0 {
		return 0, errors.New("it is an error to specify a non zero `query_store_max_look_back_period` value when using any object store other than `filesystem`")
	}
	// When using shipper, limit max look back for query to MaxChunkAge + upload interval by shipper + 15 mins to query only data whose index is not pushed yet
	defaultMaxLookBack := maxChunkAge + shipper.UploadInterval + (15 * time.Minute)

	if maxLookBackConfig == 0 {
		// If the QueryStoreMaxLookBackPeriod is still it's default value of 0, set it to the default calculated value.
		return defaultMaxLookBack, nil
	} else if maxLookBackConfig > 0 && maxLookBackConfig < defaultMaxLookBack {
		// If the QueryStoreMaxLookBackPeriod is > 0 (-1 is allowed for infinite), make sure it's at least greater than the default or throw an error
		return 0, fmt.Errorf("the configured query_store_max_look_back_period of '%v' is less than the calculated default of '%v' "+
			"which is calculated based on the max_chunk_age + 15 minute boltdb-shipper interval + 15 min additional buffer.  Increase this value"+
			"greater than the default or remove it from the configuration to use the default", maxLookBackConfig, defaultMaxLookBack)

	}
	return maxLookBackConfig, nil
}
