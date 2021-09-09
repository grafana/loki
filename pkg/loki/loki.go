package loki

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"net/http"

	cortex_tripper "github.com/cortexproject/cortex/pkg/querier/queryrange"
	"github.com/cortexproject/cortex/pkg/querier/worker"
	"github.com/cortexproject/cortex/pkg/ring"
	cortex_ruler "github.com/cortexproject/cortex/pkg/ruler"
	"github.com/cortexproject/cortex/pkg/ruler/rulestore"
	"github.com/cortexproject/cortex/pkg/scheduler"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/fakeauth"
	"github.com/cortexproject/cortex/pkg/util/grpc/healthcheck"
	"github.com/felixge/fgprof"
	"github.com/go-kit/kit/log/level"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/kv/memberlist"
	"github.com/grafana/dskit/modules"
	"github.com/grafana/dskit/runtimeconfig"
	"github.com/grafana/dskit/services"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/weaveworks/common/middleware"
	"github.com/weaveworks/common/server"
	"github.com/weaveworks/common/signals"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/pkg/distributor"
	"github.com/grafana/loki/pkg/ingester"
	"github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/lokifrontend"
	"github.com/grafana/loki/pkg/querier"
	"github.com/grafana/loki/pkg/querier/queryrange"
	"github.com/grafana/loki/pkg/ruler"
	"github.com/grafana/loki/pkg/runtime"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/stores/shipper/compactor"
	"github.com/grafana/loki/pkg/tracing"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/validation"
)

// Config is the root config for Loki.
type Config struct {
	Target      flagext.StringSliceCSV `yaml:"target,omitempty"`
	AuthEnabled bool                   `yaml:"auth_enabled,omitempty"`
	HTTPPrefix  string                 `yaml:"http_prefix"`

	Server           server.Config            `yaml:"server,omitempty"`
	Distributor      distributor.Config       `yaml:"distributor,omitempty"`
	Querier          querier.Config           `yaml:"querier,omitempty"`
	IngesterClient   client.Config            `yaml:"ingester_client,omitempty"`
	Ingester         ingester.Config          `yaml:"ingester,omitempty"`
	StorageConfig    storage.Config           `yaml:"storage_config,omitempty"`
	ChunkStoreConfig storage.ChunkStoreConfig `yaml:"chunk_store_config,omitempty"`
	SchemaConfig     storage.SchemaConfig     `yaml:"schema_config,omitempty"`
	LimitsConfig     validation.Limits        `yaml:"limits_config,omitempty"`
	TableManager     chunk.TableManagerConfig `yaml:"table_manager,omitempty"`
	Worker           worker.Config            `yaml:"frontend_worker,omitempty"`
	Frontend         lokifrontend.Config      `yaml:"frontend,omitempty"`
	Ruler            ruler.Config             `yaml:"ruler,omitempty"`
	QueryRange       queryrange.Config        `yaml:"query_range,omitempty"`
	RuntimeConfig    runtimeconfig.Config     `yaml:"runtime_config,omitempty"`
	MemberlistKV     memberlist.KVConfig      `yaml:"memberlist"`
	Tracing          tracing.Config           `yaml:"tracing"`
	CompactorConfig  compactor.Config         `yaml:"compactor,omitempty"`
	QueryScheduler   scheduler.Config         `yaml:"query_scheduler"`
}

// RegisterFlags registers flag.
func (c *Config) RegisterFlags(f *flag.FlagSet) {
	c.Server.MetricsNamespace = "loki"
	c.Server.ExcludeRequestInLog = true

	// Set the default module list to 'all'
	c.Target = []string{All}
	f.Var(&c.Target, "target", "Comma-separated list of Loki modules to load. "+
		"The alias 'all' can be used in the list to load a number of core modules and will enable single-binary mode. ")
	f.BoolVar(&c.AuthEnabled, "auth.enabled", true, "Set to false to disable auth.")

	c.Server.RegisterFlags(f)
	c.Distributor.RegisterFlags(f)
	c.Querier.RegisterFlags(f)
	c.IngesterClient.RegisterFlags(f)
	c.Ingester.RegisterFlags(f)
	c.StorageConfig.RegisterFlags(f)
	c.ChunkStoreConfig.RegisterFlags(f)
	c.SchemaConfig.RegisterFlags(f)
	c.LimitsConfig.RegisterFlags(f)
	c.TableManager.RegisterFlags(f)
	c.Frontend.RegisterFlags(f)
	c.Ruler.RegisterFlags(f)
	c.Worker.RegisterFlags(f)
	c.QueryRange.RegisterFlags(f)
	c.RuntimeConfig.RegisterFlags(f)
	c.MemberlistKV.RegisterFlags(f)
	c.Tracing.RegisterFlags(f)
	c.CompactorConfig.RegisterFlags(f)
	c.QueryScheduler.RegisterFlags(f)
}

// Clone takes advantage of pass-by-value semantics to return a distinct *Config.
// This is primarily used to parse a different flag set without mutating the original *Config.
func (c *Config) Clone() flagext.Registerer {
	return func(c Config) *Config {
		return &c
	}(*c)
}

// Validate the config and returns an error if the validation
// doesn't pass
func (c *Config) Validate() error {
	if err := c.SchemaConfig.Validate(); err != nil {
		return errors.Wrap(err, "invalid schema config")
	}
	if err := c.StorageConfig.Validate(); err != nil {
		return errors.Wrap(err, "invalid storage config")
	}
	if err := c.QueryRange.Validate(); err != nil {
		return errors.Wrap(err, "invalid queryrange config")
	}
	if err := c.TableManager.Validate(); err != nil {
		return errors.Wrap(err, "invalid tablemanager config")
	}
	if err := c.Ruler.Validate(); err != nil {
		return errors.Wrap(err, "invalid ruler config")
	}
	if err := c.Ingester.Validate(); err != nil {
		return errors.Wrap(err, "invalid ingester config")
	}
	if err := c.Worker.Validate(util_log.Logger); err != nil {
		return errors.Wrap(err, "invalid storage config")
	}
	if err := c.StorageConfig.BoltDBShipperConfig.Validate(); err != nil {
		return errors.Wrap(err, "invalid boltdb-shipper config")
	}
	if err := c.CompactorConfig.Validate(); err != nil {
		return errors.Wrap(err, "invalid compactor config")
	}
	if err := c.ChunkStoreConfig.Validate(util_log.Logger); err != nil {
		return errors.Wrap(err, "invalid chunk store config")
	}
	// TODO(cyriltovena): remove when MaxLookBackPeriod in the storage will be fully deprecated.
	if c.ChunkStoreConfig.MaxLookBackPeriod > 0 {
		c.LimitsConfig.MaxQueryLookback = c.ChunkStoreConfig.MaxLookBackPeriod
	}

	for i, sc := range c.SchemaConfig.Configs {
		if sc.RowShards > 0 && c.Ingester.IndexShards%int(sc.RowShards) > 0 {
			return fmt.Errorf(
				"incompatible ingester index shards (%d) and period config row shard factor (%d) for period config at index (%d). The ingester factor must be evenly divisible by all period config factors",
				c.Ingester.IndexShards,
				sc.RowShards,
				i,
			)
		}
	}
	return nil
}

func (c *Config) isModuleEnabled(m string) bool {
	return util.StringsContain(c.Target, m)
}

type Frontend interface {
	services.Service
	CheckReady(_ context.Context) error
}

// Loki is the root datastructure for Loki.
type Loki struct {
	Cfg Config

	// set during initialization
	ModuleManager *modules.Manager
	serviceMap    map[string]services.Service

	Server                   *server.Server
	ring                     *ring.Ring
	overrides                *validation.Overrides
	tenantConfigs            *runtime.TenantConfigs
	distributor              *distributor.Distributor
	Ingester                 *ingester.Ingester
	Querier                  *querier.Querier
	ingesterQuerier          *querier.IngesterQuerier
	Store                    storage.Store
	tableManager             *chunk.TableManager
	frontend                 Frontend
	ruler                    *cortex_ruler.Ruler
	RulerStorage             rulestore.RuleStore
	rulerAPI                 *cortex_ruler.API
	stopper                  queryrange.Stopper
	runtimeConfig            *runtimeconfig.Manager
	MemberlistKV             *memberlist.KVInitService
	compactor                *compactor.Compactor
	QueryFrontEndTripperware cortex_tripper.Tripperware

	HTTPAuthMiddleware middleware.Interface
}

// New makes a new Loki.
func New(cfg Config) (*Loki, error) {
	loki := &Loki{
		Cfg: cfg,
	}

	loki.setupAuthMiddleware()
	if err := loki.setupModuleManager(); err != nil {
		return nil, err
	}
	storage.RegisterCustomIndexClients(&loki.Cfg.StorageConfig, prometheus.DefaultRegisterer)

	return loki, nil
}

func (t *Loki) setupAuthMiddleware() {
	// Don't check auth header on TransferChunks, as we weren't originally
	// sending it and this could cause transfers to fail on update.
	t.HTTPAuthMiddleware = fakeauth.SetupAuthMiddleware(&t.Cfg.Server, t.Cfg.AuthEnabled,
		// Also don't check auth for these gRPC methods, since single call is used for multiple users (or no user like health check).
		[]string{
			"/grpc.health.v1.Health/Check",
			"/logproto.Ingester/TransferChunks",
			"/frontend.Frontend/Process",
			"/frontend.Frontend/NotifyClientShutdown",
			"/schedulerpb.SchedulerForFrontend/FrontendLoop",
			"/schedulerpb.SchedulerForQuerier/QuerierLoop",
			"/schedulerpb.SchedulerForQuerier/NotifyQuerierShutdown",
		})
}

func newDefaultConfig() *Config {
	defaultConfig := &Config{}
	defaultFS := flag.NewFlagSet("", flag.PanicOnError)
	defaultConfig.RegisterFlags(defaultFS)
	return defaultConfig
}

// Run starts Loki running, and blocks until a Loki stops.
func (t *Loki) Run() error {
	serviceMap, err := t.ModuleManager.InitModuleServices(t.Cfg.Target...)
	if err != nil {
		return err
	}

	t.serviceMap = serviceMap
	t.Server.HTTP.Handle("/services", http.HandlerFunc(t.servicesHandler))

	// get all services, create service manager and tell it to start
	var servs []services.Service
	for _, s := range serviceMap {
		servs = append(servs, s)
	}

	sm, err := services.NewManager(servs...)
	if err != nil {
		return err
	}

	// before starting servers, register /ready handler. It should reflect entire Loki.
	t.Server.HTTP.Path("/ready").Handler(t.readyHandler(sm))

	grpc_health_v1.RegisterHealthServer(t.Server.GRPC, healthcheck.New(sm))

	// This adds a way to see the config and the changes compared to the defaults
	t.Server.HTTP.Path("/config").HandlerFunc(configHandler(t.Cfg, newDefaultConfig()))

	// Each component serves its version.
	t.Server.HTTP.Path("/loki/api/v1/status/buildinfo").HandlerFunc(versionHandler())

	t.Server.HTTP.Path("/debug/fgprof").Handler(fgprof.Handler())

	// Let's listen for events from this manager, and log them.
	healthy := func() { level.Info(util_log.Logger).Log("msg", "Loki started") }
	stopped := func() { level.Info(util_log.Logger).Log("msg", "Loki stopped") }
	serviceFailed := func(service services.Service) {
		// if any service fails, stop entire Loki
		sm.StopAsync()

		// let's find out which module failed
		for m, s := range serviceMap {
			if s == service {
				if service.FailureCase() == modules.ErrStopProcess {
					level.Info(util_log.Logger).Log("msg", "received stop signal via return error", "module", m, "error", service.FailureCase())
				} else {
					level.Error(util_log.Logger).Log("msg", "module failed", "module", m, "error", service.FailureCase())
				}
				return
			}
		}

		level.Error(util_log.Logger).Log("msg", "module failed", "module", "unknown", "error", service.FailureCase())
	}

	sm.AddListener(services.NewManagerListener(healthy, stopped, serviceFailed))

	// Setup signal handler. If signal arrives, we stop the manager, which stops all the services.
	handler := signals.NewHandler(t.Server.Log)
	go func() {
		handler.Loop()
		sm.StopAsync()
	}()

	// Start all services. This can really only fail if some service is already
	// in other state than New, which should not be the case.
	err = sm.StartAsync(context.Background())
	if err == nil {
		// Wait until service manager stops. It can stop in two ways:
		// 1) Signal is received and manager is stopped.
		// 2) Any service fails.
		err = sm.AwaitStopped(context.Background())
	}

	// If there is no error yet (= service manager started and then stopped without problems),
	// but any service failed, report that failure as an error to caller.
	if err == nil {
		if failed := sm.ServicesByState()[services.Failed]; len(failed) > 0 {
			for _, f := range failed {
				if f.FailureCase() != modules.ErrStopProcess {
					// Details were reported via failure listener before
					err = errors.New("failed services")
					break
				}
			}
		}
	}
	return err
}

func (t *Loki) readyHandler(sm *services.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !sm.IsHealthy() {
			msg := bytes.Buffer{}
			msg.WriteString("Some services are not Running:\n")

			byState := sm.ServicesByState()
			for st, ls := range byState {
				msg.WriteString(fmt.Sprintf("%v: %d\n", st, len(ls)))
			}

			http.Error(w, msg.String(), http.StatusServiceUnavailable)
			return
		}

		// Ingester has a special check that makes sure that it was able to register into the ring,
		// and that all other ring entries are OK too.
		if t.Ingester != nil {
			if err := t.Ingester.CheckReady(r.Context()); err != nil {
				http.Error(w, "Ingester not ready: "+err.Error(), http.StatusServiceUnavailable)
				return
			}
		}

		// Query Frontend has a special check that makes sure that a querier is attached before it signals
		// itself as ready
		if t.frontend != nil {
			if err := t.frontend.CheckReady(r.Context()); err != nil {
				http.Error(w, "Query Frontend not ready: "+err.Error(), http.StatusServiceUnavailable)
				return
			}
		}

		http.Error(w, "ready", http.StatusOK)
	}
}

func (t *Loki) setupModuleManager() error {
	mm := modules.NewManager(util_log.Logger)

	mm.RegisterModule(Server, t.initServer)
	mm.RegisterModule(RuntimeConfig, t.initRuntimeConfig)
	mm.RegisterModule(MemberlistKV, t.initMemberlistKV)
	mm.RegisterModule(Ring, t.initRing)
	mm.RegisterModule(Overrides, t.initOverrides)
	mm.RegisterModule(TenantConfigs, t.initTenantConfigs)
	mm.RegisterModule(Distributor, t.initDistributor)
	mm.RegisterModule(Store, t.initStore)
	mm.RegisterModule(Ingester, t.initIngester)
	mm.RegisterModule(Querier, t.initQuerier)
	mm.RegisterModule(IngesterQuerier, t.initIngesterQuerier)
	mm.RegisterModule(QueryFrontendTripperware, t.initQueryFrontendTripperware, modules.UserInvisibleModule)
	mm.RegisterModule(QueryFrontend, t.initQueryFrontend)
	mm.RegisterModule(RulerStorage, t.initRulerStorage, modules.UserInvisibleModule)
	mm.RegisterModule(Ruler, t.initRuler)
	mm.RegisterModule(TableManager, t.initTableManager)
	mm.RegisterModule(Compactor, t.initCompactor)
	mm.RegisterModule(IndexGateway, t.initIndexGateway)
	mm.RegisterModule(QueryScheduler, t.initQueryScheduler)
	mm.RegisterModule(All, nil)

	// Add dependencies
	deps := map[string][]string{
		Ring:                     {RuntimeConfig, Server, MemberlistKV},
		Overrides:                {RuntimeConfig},
		TenantConfigs:            {RuntimeConfig},
		Distributor:              {Ring, Server, Overrides, TenantConfigs},
		Store:                    {Overrides},
		Ingester:                 {Store, Server, MemberlistKV, TenantConfigs},
		Querier:                  {Store, Ring, Server, IngesterQuerier, TenantConfigs},
		QueryFrontendTripperware: {Server, Overrides, TenantConfigs},
		QueryFrontend:            {QueryFrontendTripperware},
		QueryScheduler:           {Server, Overrides},
		Ruler:                    {Ring, Server, Store, RulerStorage, IngesterQuerier, Overrides, TenantConfigs},
		TableManager:             {Server},
		Compactor:                {Server, Overrides},
		IndexGateway:             {Server},
		IngesterQuerier:          {Ring},
		All:                      {Querier, Ingester, Distributor, TableManager, Ruler},
	}

	// Add IngesterQuerier as a dependency for store when target is either ingester or querier.
	if t.Cfg.isModuleEnabled(Querier) || t.Cfg.isModuleEnabled(Ruler) {
		deps[Store] = append(deps[Store], IngesterQuerier)
	}

	// If we are running Loki with boltdb-shipper as a single binary, without clustered mode(which should always be the case when using inmemory ring),
	// we should start compactor as well for better user experience.
	if storage.UsingBoltdbShipper(t.Cfg.SchemaConfig.Configs) && t.Cfg.Ingester.LifecyclerConfig.RingConfig.KVStore.Store == "inmemory" {
		deps[All] = append(deps[All], Compactor)
	}

	for mod, targets := range deps {
		if err := mm.AddDependency(mod, targets...); err != nil {
			return err
		}
	}

	t.ModuleManager = mm

	return nil
}
