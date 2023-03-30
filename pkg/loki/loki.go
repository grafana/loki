package loki

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	rt "runtime"
	"time"

	"go.uber.org/atomic"

	"github.com/fatih/color"
	"github.com/felixge/fgprof"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/grpcutil"
	"github.com/grafana/dskit/kv/memberlist"
	"github.com/grafana/dskit/modules"
	"github.com/grafana/dskit/ring"
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
	ingester_client "github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/loki/common"
	"github.com/grafana/loki/pkg/lokifrontend"
	"github.com/grafana/loki/pkg/querier"
	"github.com/grafana/loki/pkg/querier/queryrange"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	basetripper "github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/querier/worker"
	"github.com/grafana/loki/pkg/ruler"
	base_ruler "github.com/grafana/loki/pkg/ruler/base"
	"github.com/grafana/loki/pkg/ruler/rulestore"
	"github.com/grafana/loki/pkg/runtime"
	"github.com/grafana/loki/pkg/scheduler"
	internalserver "github.com/grafana/loki/pkg/server"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/compactor"
	compactor_client "github.com/grafana/loki/pkg/storage/stores/indexshipper/compactor/client"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/compactor/deletion"
	"github.com/grafana/loki/pkg/storage/stores/series/index"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexgateway"
	"github.com/grafana/loki/pkg/tracing"
	"github.com/grafana/loki/pkg/usagestats"
	"github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/util/fakeauth"
	"github.com/grafana/loki/pkg/util/limiter"
	util_log "github.com/grafana/loki/pkg/util/log"
	serverutil "github.com/grafana/loki/pkg/util/server"
	"github.com/grafana/loki/pkg/validation"
)

// Config is the root config for Loki.
type Config struct {
	Target       flagext.StringSliceCSV `yaml:"target,omitempty"`
	AuthEnabled  bool                   `yaml:"auth_enabled,omitempty"`
	HTTPPrefix   string                 `yaml:"http_prefix" doc:"hidden"`
	BallastBytes int                    `yaml:"ballast_bytes"`

	// TODO(dannyk): Remove these config options before next release; they don't need to be configurable.
	//				 These are only here to allow us to test the new functionality.
	UseBufferedLogger bool `yaml:"use_buffered_logger" doc:"hidden"`
	UseSyncLogger     bool `yaml:"use_sync_logger" doc:"hidden"`

	Server              server.Config               `yaml:"server,omitempty"`
	InternalServer      internalserver.Config       `yaml:"internal_server,omitempty" doc:"hidden"`
	Distributor         distributor.Config          `yaml:"distributor,omitempty"`
	Querier             querier.Config              `yaml:"querier,omitempty"`
	QueryScheduler      scheduler.Config            `yaml:"query_scheduler"`
	Frontend            lokifrontend.Config         `yaml:"frontend,omitempty"`
	QueryRange          queryrange.Config           `yaml:"query_range,omitempty"`
	Ruler               ruler.Config                `yaml:"ruler,omitempty"`
	IngesterClient      ingester_client.Config      `yaml:"ingester_client,omitempty"`
	Ingester            ingester.Config             `yaml:"ingester,omitempty"`
	IndexGateway        indexgateway.Config         `yaml:"index_gateway"`
	StorageConfig       storage.Config              `yaml:"storage_config,omitempty"`
	ChunkStoreConfig    config.ChunkStoreConfig     `yaml:"chunk_store_config,omitempty"`
	SchemaConfig        config.SchemaConfig         `yaml:"schema_config,omitempty"`
	CompactorConfig     compactor.Config            `yaml:"compactor,omitempty"`
	CompactorHTTPClient compactor_client.HTTPConfig `yaml:"compactor_client,omitempty" doc:"hidden"`
	CompactorGRPCClient compactor_client.GRPCConfig `yaml:"compactor_grpc_client,omitempty" doc:"hidden"`
	LimitsConfig        validation.Limits           `yaml:"limits_config,omitempty"`
	Worker              worker.Config               `yaml:"frontend_worker,omitempty"`
	TableManager        index.TableManagerConfig    `yaml:"table_manager,omitempty"`
	MemberlistKV        memberlist.KVConfig         `yaml:"memberlist" doc:"hidden"`

	RuntimeConfig runtimeconfig.Config `yaml:"runtime_config,omitempty"`
	Tracing       tracing.Config       `yaml:"tracing"`
	UsageReport   usagestats.Config    `yaml:"analytics"`

	LegacyReadTarget bool `yaml:"legacy_read_target,omitempty" doc:"hidden"`

	Common common.Config `yaml:"common,omitempty"`

	ShutdownDelay time.Duration `yaml:"shutdown_delay" category:"experimental"`
}

// RegisterFlags registers flag.
func (c *Config) RegisterFlags(f *flag.FlagSet) {
	c.Server.MetricsNamespace = "loki"
	c.Server.ExcludeRequestInLog = true

	// Set the default module list to 'all'
	c.Target = []string{All}
	f.Var(&c.Target, "target",
		"A comma-separated list of components to run. "+
			"The default value 'all' runs Loki in single binary mode. "+
			"The value 'read' is an alias to run only read-path related components such as the querier and query-frontend, but all in the same process. "+
			"The value 'write' is an alias to run only write-path related components such as the distributor and compactor, but all in the same process. "+
			"Supported values: all, compactor, distributor, ingester, querier, query-scheduler, ingester-querier, query-frontend, index-gateway, ruler, table-manager, read, write. "+
			"A full list of available targets can be printed when running Loki with the '-list-targets' command line flag. ",
	)
	f.BoolVar(&c.AuthEnabled, "auth.enabled", true,
		"Enables authentication through the X-Scope-OrgID header, which must be present if true. "+
			"If false, the OrgID will always be set to 'fake'.",
	)
	f.IntVar(&c.BallastBytes, "config.ballast-bytes", 0,
		"The amount of virtual memory in bytes to reserve as ballast in order to optimize garbage collection. "+
			"Larger ballasts result in fewer garbage collection passes, reducing CPU overhead at the cost of heap size. "+
			"The ballast will not consume physical memory, because it is never read from. "+
			"It will, however, distort metrics, because it is counted as live memory. ",
	)
	f.BoolVar(&c.UseBufferedLogger, "log.use-buffered", true, "Uses a line-buffered logger to improve performance.")
	f.BoolVar(&c.UseSyncLogger, "log.use-sync", true, "Forces all lines logged to hold a mutex to serialize writes.")

	//TODO(trevorwhitney): flip this to false with Loki 3.0
	f.BoolVar(&c.LegacyReadTarget, "legacy-read-mode", true, "Set to false to disable the legacy read mode and use new scalable mode with 3rd backend target. "+
		"The default will be flipped to false in the next Loki release.")

	f.DurationVar(&c.ShutdownDelay, "shutdown-delay", 0, "How long to wait between SIGTERM and shutdown. After receiving SIGTERM, Loki will report 503 Service Unavailable status via /ready endpoint.")

	c.registerServerFlagsWithChangedDefaultValues(f)
	c.Common.RegisterFlags(f)
	c.Distributor.RegisterFlags(f)
	c.Querier.RegisterFlags(f)
	c.CompactorHTTPClient.RegisterFlags(f)
	c.CompactorGRPCClient.RegisterFlags(f)
	c.IngesterClient.RegisterFlags(f)
	c.Ingester.RegisterFlags(f)
	c.StorageConfig.RegisterFlags(f)
	c.IndexGateway.RegisterFlags(f)
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
	c.UsageReport.RegisterFlags(f)
}

func (c *Config) registerServerFlagsWithChangedDefaultValues(fs *flag.FlagSet) {
	throwaway := flag.NewFlagSet("throwaway", flag.PanicOnError)

	// Register to throwaway flags first. Default values are remembered during registration and cannot be changed,
	// but we can take values from throwaway flag set and reregister into supplied flags with new default values.
	c.Server.RegisterFlags(throwaway)
	c.InternalServer.RegisterFlags(throwaway)

	throwaway.VisitAll(func(f *flag.Flag) {
		// Ignore errors when setting new values. We have a test to verify that it works.
		switch f.Name {
		case "server.grpc.keepalive.min-time-between-pings":
			_ = f.Value.Set("10s")

		case "server.grpc.keepalive.ping-without-stream-allowed":
			_ = f.Value.Set("true")

		case "server.http-listen-port":
			_ = f.Value.Set("3100")
		}

		fs.Var(f.Value, f.Name, f.Usage)
	})
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
	if err := c.Querier.Validate(); err != nil {
		return errors.Wrap(err, "invalid querier config")
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
	if err := c.LimitsConfig.Validate(); err != nil {
		return errors.Wrap(err, "invalid limits config")
	}
	if err := c.Worker.Validate(util_log.Logger); err != nil {
		return errors.Wrap(err, "invalid frontend-worker config")
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

	if err := c.QueryRange.Validate(); err != nil {
		return errors.Wrap(err, "invalid query_range config")
	}

	if err := ValidateConfigCompatibility(*c); err != nil {
		return err
	}

	if err := AdjustForTimeoutsMigration(c); err != nil {
		return err
	}

	// Honor the legacy scalable deployment topology
	if c.LegacyReadTarget {
		if c.isModuleEnabled(Backend) {
			return fmt.Errorf("invalid target, cannot run backend target with legacy read mode")
		}
	}

	return nil
}

// AdjustForTimeoutsMigration will adjust Loki timeouts configuration to be in accordance with the next major release.
//
// We're preparing to unify the querier:engine:timeout and querier:query_timeout into a single timeout named limits_config:query_timeout.
// The migration encompasses of:
// - If limits_config:query_timeout is explicitly configured, use it everywhere as it is a new configuration and by
// configuring it, users are expressing that they're willing of using it.
// - If none are explicitly configured, use the default engine:timeout everywhere as it is longer than the default limits_config:query_timeout
// and otherwise users would start to experience shorter timeouts without expecting it.
// - If only the querier:engine:timeout was explicitly configured, warn the user and use it everywhere.
func AdjustForTimeoutsMigration(c *Config) error {
	engineTimeoutIsDefault := c.Querier.Engine.Timeout == logql.DefaultEngineTimeout
	perTenantTimeoutIsDefault := c.LimitsConfig.QueryTimeout.String() == validation.DefaultPerTenantQueryTimeout
	if engineTimeoutIsDefault && perTenantTimeoutIsDefault {
		if err := c.LimitsConfig.QueryTimeout.Set(c.Querier.Engine.Timeout.String()); err != nil {
			return fmt.Errorf("couldn't set per-tenant query_timeout as the engine timeout value: %w", err)
		}
		level.Warn(util_log.Logger).Log("msg",
			fmt.Sprintf(
				"per-tenant timeout not configured, using default engine timeout (%q). This behavior will change in the next major to always use the default per-tenant timeout (%q).",
				c.Querier.Engine.Timeout.String(),
				c.LimitsConfig.QueryTimeout.String(),
			),
		)
		return nil
	}

	if !perTenantTimeoutIsDefault && !engineTimeoutIsDefault {
		level.Warn(util_log.Logger).Log("msg",
			fmt.Sprintf(
				"using configured per-tenant timeout (%q) as the default (can be overridden per-tenant in the limits_config). Configured engine timeout (%q) is deprecated and will be ignored.",
				c.LimitsConfig.QueryTimeout.String(),
				c.Querier.Engine.Timeout.String(),
			),
		)
		return nil
	}

	if perTenantTimeoutIsDefault && !engineTimeoutIsDefault {
		if err := c.LimitsConfig.QueryTimeout.Set(c.Querier.Engine.Timeout.String()); err != nil {
			return fmt.Errorf("couldn't set per-tenant query_timeout as the engine timeout value: %w", err)
		}
		level.Warn(util_log.Logger).Log("msg",
			fmt.Sprintf(
				"using configured engine timeout (%q) as the default (can be overridden per-tenant in the limits_config). Be aware that engine timeout (%q) is deprecated and will be removed in the next major version.",
				c.Querier.Engine.Timeout.String(),
				c.LimitsConfig.QueryTimeout.String(),
			),
		)
		return nil
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
	deps          map[string][]string
	SignalHandler *signals.Handler

	Server                   *server.Server
	InternalServer           *server.Server
	ring                     *ring.Ring
	Overrides                limiter.CombinedLimits
	tenantConfigs            *runtime.TenantConfigs
	TenantLimits             validation.TenantLimits
	distributor              *distributor.Distributor
	Ingester                 ingester.Interface
	Querier                  querier.Querier
	cacheGenerationLoader    queryrangebase.CacheGenNumberLoader
	querierAPI               *querier.QuerierAPI
	ingesterQuerier          *querier.IngesterQuerier
	Store                    storage.Store
	tableManager             *index.TableManager
	frontend                 Frontend
	ruler                    *base_ruler.Ruler
	ruleEvaluator            ruler.Evaluator
	RulerStorage             rulestore.RuleStore
	rulerAPI                 *base_ruler.API
	stopper                  queryrange.Stopper
	runtimeConfig            *runtimeconfig.Manager
	MemberlistKV             *memberlist.KVInitService
	compactor                *compactor.Compactor
	QueryFrontEndTripperware basetripper.Tripperware
	queryScheduler           *scheduler.Scheduler
	usageReport              *usagestats.Reporter
	indexGatewayRingManager  *indexgateway.RingManager

	clientMetrics       storage.ClientMetrics
	deleteClientMetrics *deletion.DeleteRequestClientMetrics

	HTTPAuthMiddleware middleware.Interface

	ingesterRelease *atomic.Bool
}

// New makes a new Loki.
func New(cfg Config) (*Loki, error) {
	loki := &Loki{
		Cfg:                 cfg,
		clientMetrics:       storage.NewClientMetrics(),
		deleteClientMetrics: deletion.NewDeleteRequestClientMetrics(prometheus.DefaultRegisterer),
	}
	usagestats.Edition("oss")
	loki.setupAuthMiddleware()
	loki.setupGRPCRecoveryMiddleware()
	if err := loki.setupModuleManager(); err != nil {
		return nil, err
	}

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
			"/logproto.StreamData/GetStreamRates",
			"/frontend.Frontend/Process",
			"/frontend.Frontend/NotifyClientShutdown",
			"/schedulerpb.SchedulerForFrontend/FrontendLoop",
			"/schedulerpb.SchedulerForQuerier/QuerierLoop",
			"/schedulerpb.SchedulerForQuerier/NotifyQuerierShutdown",
		})
}

func (t *Loki) setupGRPCRecoveryMiddleware() {
	t.Cfg.Server.GRPCMiddleware = append(t.Cfg.Server.GRPCMiddleware, serverutil.RecoveryGRPCUnaryInterceptor)
	t.Cfg.Server.GRPCStreamMiddleware = append(t.Cfg.Server.GRPCStreamMiddleware, serverutil.RecoveryGRPCStreamInterceptor)
}

func newDefaultConfig() *Config {
	defaultConfig := &Config{}
	defaultFS := flag.NewFlagSet("", flag.PanicOnError)
	defaultConfig.RegisterFlags(defaultFS)
	return defaultConfig
}

// RunOpts configures custom behavior for running Loki.
type RunOpts struct {
	// CustomConfigEndpointHandlerFn is the handlerFunc to be used by the /config endpoint.
	// If empty, default handlerFunc will be used.
	CustomConfigEndpointHandlerFn func(http.ResponseWriter, *http.Request)
}

func (t *Loki) bindConfigEndpoint(opts RunOpts) {
	configEndpointHandlerFn := configHandler(t.Cfg, newDefaultConfig())
	if opts.CustomConfigEndpointHandlerFn != nil {
		configEndpointHandlerFn = opts.CustomConfigEndpointHandlerFn
	}
	t.Server.HTTP.Path("/config").Methods("GET").HandlerFunc(configEndpointHandlerFn)
}

// ListTargets prints a list of available user visible targets and their
// dependencies
func (t *Loki) ListTargets() {
	green := color.New(color.FgGreen, color.Bold)
	if rt.GOOS == "windows" {
		green.DisableColor()
	}
	for _, m := range t.ModuleManager.UserVisibleModuleNames() {
		fmt.Fprintln(os.Stdout, green.Sprint(m))

		for _, n := range t.ModuleManager.DependenciesForModule(m) {
			if t.ModuleManager.IsUserVisibleModule(n) {
				fmt.Fprintln(os.Stdout, " ", n)
			}
		}
	}
}

// Run starts Loki running, and blocks until a Loki stops.
func (t *Loki) Run(opts RunOpts) error {
	serviceMap, err := t.ModuleManager.InitModuleServices(t.Cfg.Target...)
	if err != nil {
		return err
	}

	t.serviceMap = serviceMap
	t.Server.HTTP.Path("/services").Methods("GET").Handler(http.HandlerFunc(t.servicesHandler))

	// get all services, create service manager and tell it to start
	var servs []services.Service
	for _, s := range serviceMap {
		servs = append(servs, s)
	}

	sm, err := services.NewManager(servs...)
	if err != nil {
		return err
	}

	shutdownRequested := atomic.NewBool(false)

	// before starting servers, register /ready handler. It should reflect entire Loki.
	if t.Cfg.InternalServer.Enable {
		t.InternalServer.HTTP.Path("/ready").Methods("GET").Handler(t.readyHandler(sm, shutdownRequested))
	}
	t.Server.HTTP.Path("/ready").Methods("GET").Handler(t.readyHandler(sm, shutdownRequested))

	grpc_health_v1.RegisterHealthServer(t.Server.GRPC, grpcutil.NewHealthCheck(sm))

	// Config endpoint adds a way to see the config and the changes compared to the defaults.
	t.bindConfigEndpoint(opts)

	// Each component serves its version.
	if t.Cfg.InternalServer.Enable {
		t.InternalServer.HTTP.Path("/loki/api/v1/status/buildinfo").Methods("GET").HandlerFunc(versionHandler())
	}
	t.Server.HTTP.Path("/loki/api/v1/status/buildinfo").Methods("GET").HandlerFunc(versionHandler())

	t.Server.HTTP.Path("/debug/fgprof").Methods("GET", "POST").Handler(fgprof.Handler())
	t.Server.HTTP.Path("/loki/api/v1/format_query").Methods("GET", "POST").HandlerFunc(formatQueryHandler())

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
	t.SignalHandler = signals.NewHandler(t.Server.Log)
	go func() {
		t.SignalHandler.Loop()
		shutdownRequested.Store(true)

		if t.Cfg.ShutdownDelay > 0 {
			level.Info(util_log.Logger).Log("msg", fmt.Sprintf("waiting %v before shutting down services", t.Cfg.ShutdownDelay))
			time.Sleep(t.Cfg.ShutdownDelay)
		}

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

func (t *Loki) readyHandler(sm *services.Manager, shutdownRequested *atomic.Bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if shutdownRequested.Load() {
			level.Debug(util_log.Logger).Log("msg", "application is stopping")
			http.Error(w, "Application is stopping", http.StatusServiceUnavailable)
			return
		}
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

	mm.RegisterModule(Server, t.initServer, modules.UserInvisibleModule)

	if t.Cfg.InternalServer.Enable {
		mm.RegisterModule(InternalServer, t.initInternalServer, modules.UserInvisibleModule)
	}

	mm.RegisterModule(RuntimeConfig, t.initRuntimeConfig, modules.UserInvisibleModule)
	mm.RegisterModule(MemberlistKV, t.initMemberlistKV, modules.UserInvisibleModule)
	mm.RegisterModule(Ring, t.initRing, modules.UserInvisibleModule)
	mm.RegisterModule(Overrides, t.initOverrides, modules.UserInvisibleModule)
	mm.RegisterModule(OverridesExporter, t.initOverridesExporter)
	mm.RegisterModule(TenantConfigs, t.initTenantConfigs, modules.UserInvisibleModule)
	mm.RegisterModule(Distributor, t.initDistributor)
	mm.RegisterModule(Store, t.initStore, modules.UserInvisibleModule)
	mm.RegisterModule(Ingester, t.initIngester)
	mm.RegisterModule(Querier, t.initQuerier)
	mm.RegisterModule(IngesterQuerier, t.initIngesterQuerier)
	mm.RegisterModule(QueryFrontendTripperware, t.initQueryFrontendTripperware, modules.UserInvisibleModule)
	mm.RegisterModule(QueryFrontend, t.initQueryFrontend)
	mm.RegisterModule(RulerStorage, t.initRulerStorage, modules.UserInvisibleModule)
	mm.RegisterModule(Ruler, t.initRuler)
	mm.RegisterModule(RuleEvaluator, t.initRuleEvaluator, modules.UserInvisibleModule)
	mm.RegisterModule(TableManager, t.initTableManager)
	mm.RegisterModule(Compactor, t.initCompactor)
	mm.RegisterModule(IndexGateway, t.initIndexGateway)
	mm.RegisterModule(QueryScheduler, t.initQueryScheduler)
	mm.RegisterModule(IndexGatewayRing, t.initIndexGatewayRing, modules.UserInvisibleModule)
	mm.RegisterModule(UsageReport, t.initUsageReport)
	mm.RegisterModule(CacheGenerationLoader, t.initCacheGenerationLoader)

	mm.RegisterModule(All, nil)
	mm.RegisterModule(Read, nil)
	mm.RegisterModule(Write, nil)
	mm.RegisterModule(Backend, nil)

	// Add dependencies
	deps := map[string][]string{
		Ring:                     {RuntimeConfig, Server, MemberlistKV},
		UsageReport:              {},
		Overrides:                {RuntimeConfig},
		OverridesExporter:        {Overrides, Server},
		TenantConfigs:            {RuntimeConfig},
		Distributor:              {Ring, Server, Overrides, TenantConfigs, UsageReport},
		Store:                    {Overrides, IndexGatewayRing},
		Ingester:                 {Store, Server, MemberlistKV, TenantConfigs, UsageReport},
		Querier:                  {Store, Ring, Server, IngesterQuerier, Overrides, UsageReport, CacheGenerationLoader},
		QueryFrontendTripperware: {Server, Overrides, TenantConfigs},
		QueryFrontend:            {QueryFrontendTripperware, UsageReport, CacheGenerationLoader},
		QueryScheduler:           {Server, Overrides, MemberlistKV, UsageReport},
		Ruler:                    {Ring, Server, RulerStorage, RuleEvaluator, Overrides, TenantConfigs, UsageReport},
		RuleEvaluator:            {Ring, Server, Store, IngesterQuerier, Overrides, TenantConfigs, UsageReport},
		TableManager:             {Server, UsageReport},
		Compactor:                {Server, Overrides, MemberlistKV, UsageReport},
		IndexGateway:             {Server, Store, Overrides, UsageReport, MemberlistKV, IndexGatewayRing},
		IngesterQuerier:          {Ring},
		IndexGatewayRing:         {RuntimeConfig, Server, MemberlistKV},
		All:                      {QueryScheduler, QueryFrontend, Querier, Ingester, Distributor, Ruler, Compactor},
		Read:                     {QueryFrontend, Querier},
		Write:                    {Ingester, Distributor},
		Backend:                  {QueryScheduler, Ruler, Compactor, IndexGateway},
		MemberlistKV:             {Server},
	}

	if t.Cfg.Querier.PerRequestLimitsEnabled {
		level.Debug(util_log.Logger).Log("msg", "per-query request limits support enabled")
		mm.RegisterModule(QueryLimiter, t.initQueryLimiter, modules.UserInvisibleModule)
		mm.RegisterModule(QueryLimitsInterceptors, t.initQueryLimitsInterceptors, modules.UserInvisibleModule)
		mm.RegisterModule(QueryLimitsTripperware, t.initQueryLimitsTripperware, modules.UserInvisibleModule)

		// Ensure query limiter embeds overrides after they've been
		// created.
		deps[QueryLimiter] = []string{Overrides}
		deps[QueryLimitsInterceptors] = []string{}

		// Ensure query limits tripperware embeds the query frontend
		// tripperware after it's been created. Any additional
		// middleware/tripperware you want to add to the querier or
		// frontend must happen inject a dependence on the query limits
		// tripperware.
		deps[QueryLimitsTripperware] = []string{QueryFrontendTripperware}

		deps[Querier] = append(deps[Querier], QueryLimiter)

		// The frontend receives a tripperware. Make sure it uses the
		// wrapped one.
		deps[QueryFrontend] = append(deps[QueryFrontend], QueryLimitsTripperware)

		// query frontend tripperware uses t.Overrides. Make sure it
		// uses the one wrapped by query limiter.
		deps[QueryFrontendTripperware] = append(deps[QueryFrontendTripperware], QueryLimiter)

		if err := mm.AddDependency(Server, QueryLimitsInterceptors); err != nil {
			return err
		}
	}

	// Add IngesterQuerier as a dependency for store when target is either querier, ruler, read, or backend.
	if t.Cfg.isModuleEnabled(Querier) || t.Cfg.isModuleEnabled(Ruler) || t.Cfg.isModuleEnabled(Read) || t.Cfg.isModuleEnabled(Backend) {
		deps[Store] = append(deps[Store], IngesterQuerier)
	}

	// If the query scheduler and querier are running together, make sure the scheduler goes
	// first to initialize the ring that will also be used by the querier
	if (t.Cfg.isModuleEnabled(Querier) && t.Cfg.isModuleEnabled(QueryScheduler)) || t.Cfg.isModuleEnabled(All) {
		deps[Querier] = append(deps[Querier], QueryScheduler)
	}

	// If the query scheduler and query frontend are running together, make sure the scheduler goes
	// first to initialize the ring that will also be used by the query frontend
	if (t.Cfg.isModuleEnabled(QueryFrontend) && t.Cfg.isModuleEnabled(QueryScheduler)) || t.Cfg.isModuleEnabled(All) {
		deps[QueryFrontend] = append(deps[QueryFrontend], QueryScheduler)
	}

	if t.Cfg.LegacyReadTarget {
		deps[Read] = append(deps[Read], QueryScheduler, Ruler, Compactor, IndexGateway)
	}

	if t.Cfg.InternalServer.Enable {
		for key, ds := range deps {
			idx := -1
			for i, v := range ds {
				if v == Server {
					idx = i
					break
				}
			}

			if idx == -1 {
				continue
			}

			a := append(ds[:idx+1], ds[idx:]...)
			a[idx] = InternalServer
			deps[key] = a
		}

	}

	for mod, targets := range deps {
		if err := mm.AddDependency(mod, targets...); err != nil {
			return err
		}
	}

	t.deps = deps
	t.ModuleManager = mm

	if t.isModuleActive(Ingester) {
		if err := mm.AddDependency(UsageReport, Ring); err != nil {
			return err
		}
	}

	return nil
}

func (t *Loki) isModuleActive(m string) bool {
	for _, target := range t.Cfg.Target {
		if target == m {
			return true
		}
		if t.recursiveIsModuleActive(target, m) {
			return true
		}
	}
	return false
}

func (t *Loki) recursiveIsModuleActive(target, m string) bool {
	if targetDeps, ok := t.deps[target]; ok {
		for _, dep := range targetDeps {
			if dep == m {
				return true
			}
			if t.recursiveIsModuleActive(dep, m) {
				return true
			}
		}
	}
	return false
}
