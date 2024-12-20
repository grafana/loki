package loki

import (
	"bytes"
	"context"
	stdlib_errors "errors"
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
	"github.com/grafana/dskit/middleware"
	"github.com/grafana/dskit/modules"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/runtimeconfig"
	"github.com/grafana/dskit/server"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/signals"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/v3/pkg/analytics"
	blockbuilder "github.com/grafana/loki/v3/pkg/blockbuilder/builder"
	blockscheduler "github.com/grafana/loki/v3/pkg/blockbuilder/scheduler"
	"github.com/grafana/loki/v3/pkg/bloombuild"
	"github.com/grafana/loki/v3/pkg/bloomgateway"
	"github.com/grafana/loki/v3/pkg/compactor"
	compactorclient "github.com/grafana/loki/v3/pkg/compactor/client"
	"github.com/grafana/loki/v3/pkg/compactor/deletion"
	"github.com/grafana/loki/v3/pkg/distributor"
	"github.com/grafana/loki/v3/pkg/indexgateway"
	"github.com/grafana/loki/v3/pkg/ingester"
	ingester_client "github.com/grafana/loki/v3/pkg/ingester/client"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/loghttp/push"
	"github.com/grafana/loki/v3/pkg/loki/common"
	"github.com/grafana/loki/v3/pkg/lokifrontend"
	"github.com/grafana/loki/v3/pkg/lokifrontend/frontend/transport"
	"github.com/grafana/loki/v3/pkg/pattern"
	"github.com/grafana/loki/v3/pkg/querier"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/querier/worker"
	"github.com/grafana/loki/v3/pkg/ruler"
	base_ruler "github.com/grafana/loki/v3/pkg/ruler/base"
	"github.com/grafana/loki/v3/pkg/ruler/rulestore"
	"github.com/grafana/loki/v3/pkg/runtime"
	"github.com/grafana/loki/v3/pkg/scheduler"
	internalserver "github.com/grafana/loki/v3/pkg/server"
	"github.com/grafana/loki/v3/pkg/storage"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/series/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/v3/pkg/tracing"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/util/fakeauth"
	"github.com/grafana/loki/v3/pkg/util/limiter"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	lokiring "github.com/grafana/loki/v3/pkg/util/ring"
	serverutil "github.com/grafana/loki/v3/pkg/util/server"
	"github.com/grafana/loki/v3/pkg/validation"
)

// Config is the root config for Loki.
type Config struct {
	Target       flagext.StringSliceCSV `yaml:"target,omitempty"`
	AuthEnabled  bool                   `yaml:"auth_enabled,omitempty"`
	HTTPPrefix   string                 `yaml:"http_prefix" doc:"hidden"`
	BallastBytes int                    `yaml:"ballast_bytes"`

	Server              server.Config              `yaml:"server,omitempty"`
	InternalServer      internalserver.Config      `yaml:"internal_server,omitempty" doc:"hidden"`
	Distributor         distributor.Config         `yaml:"distributor,omitempty"`
	Querier             querier.Config             `yaml:"querier,omitempty"`
	QueryScheduler      scheduler.Config           `yaml:"query_scheduler"`
	Frontend            lokifrontend.Config        `yaml:"frontend,omitempty"`
	QueryRange          queryrange.Config          `yaml:"query_range,omitempty"`
	Ruler               ruler.Config               `yaml:"ruler,omitempty"`
	RulerStorage        rulestore.Config           `yaml:"ruler_storage,omitempty" doc:"hidden"`
	IngesterClient      ingester_client.Config     `yaml:"ingester_client,omitempty"`
	Ingester            ingester.Config            `yaml:"ingester,omitempty"`
	BlockBuilder        blockbuilder.Config        `yaml:"block_builder,omitempty"`
	BlockScheduler      blockscheduler.Config      `yaml:"block_scheduler,omitempty"`
	Pattern             pattern.Config             `yaml:"pattern_ingester,omitempty"`
	IndexGateway        indexgateway.Config        `yaml:"index_gateway"`
	BloomBuild          bloombuild.Config          `yaml:"bloom_build,omitempty" category:"experimental"`
	BloomGateway        bloomgateway.Config        `yaml:"bloom_gateway,omitempty" category:"experimental"`
	StorageConfig       storage.Config             `yaml:"storage_config,omitempty"`
	ChunkStoreConfig    config.ChunkStoreConfig    `yaml:"chunk_store_config,omitempty"`
	SchemaConfig        config.SchemaConfig        `yaml:"schema_config,omitempty"`
	CompactorConfig     compactor.Config           `yaml:"compactor,omitempty"`
	CompactorHTTPClient compactorclient.HTTPConfig `yaml:"compactor_client,omitempty" doc:"hidden"`
	CompactorGRPCClient compactorclient.GRPCConfig `yaml:"compactor_grpc_client,omitempty"`
	LimitsConfig        validation.Limits          `yaml:"limits_config"`
	Worker              worker.Config              `yaml:"frontend_worker,omitempty"`
	TableManager        index.TableManagerConfig   `yaml:"table_manager,omitempty"`
	MemberlistKV        memberlist.KVConfig        `yaml:"memberlist"`
	KafkaConfig         kafka.Config               `yaml:"kafka_config,omitempty" category:"experimental"`

	RuntimeConfig     runtimeconfig.Config `yaml:"runtime_config,omitempty"`
	OperationalConfig runtime.Config       `yaml:"operational_config,omitempty"`
	Tracing           tracing.Config       `yaml:"tracing"`
	Analytics         analytics.Config     `yaml:"analytics"`
	Profiling         ProfilingConfig      `yaml:"profiling,omitempty"`

	LegacyReadTarget bool `yaml:"legacy_read_target,omitempty" doc:"hidden|deprecated"`

	Common common.Config `yaml:"common,omitempty"`

	ShutdownDelay time.Duration `yaml:"shutdown_delay"`

	MetricsNamespace string `yaml:"metrics_namespace"`
}

// RegisterFlags registers flag.
func (c *Config) RegisterFlags(f *flag.FlagSet) {
	c.Server.MetricsNamespace = constants.Loki
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

	f.BoolVar(&c.LegacyReadTarget, "legacy-read-mode", false, "Deprecated. Set to true to enable the legacy read mode which includes the components from the backend target. "+
		"This setting is deprecated and will be removed in the next minor release.")

	f.DurationVar(&c.ShutdownDelay, "shutdown-delay", 0, "How long to wait between SIGTERM and shutdown. After receiving SIGTERM, Loki will report 503 Service Unavailable status via /ready endpoint.")

	f.StringVar(&c.MetricsNamespace, "metrics-namespace", constants.Loki, "Namespace of the metrics that in previous releases had cortex as namespace. This setting is deprecated and will be removed in the next minor release.")

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
	c.BloomGateway.RegisterFlags(f)
	c.ChunkStoreConfig.RegisterFlags(f)
	c.SchemaConfig.RegisterFlags(f)
	c.LimitsConfig.RegisterFlags(f)
	c.TableManager.RegisterFlags(f)
	c.Frontend.RegisterFlags(f)
	c.Ruler.RegisterFlags(f)
	c.RulerStorage.RegisterFlags(f)
	c.Worker.RegisterFlags(f)
	c.QueryRange.RegisterFlags(f)
	c.RuntimeConfig.RegisterFlags(f)
	c.MemberlistKV.RegisterFlags(f)
	c.Tracing.RegisterFlags(f)
	c.CompactorConfig.RegisterFlags(f)
	c.BloomBuild.RegisterFlags(f)
	c.QueryScheduler.RegisterFlags(f)
	c.Analytics.RegisterFlags(f)
	c.OperationalConfig.RegisterFlags(f)
	c.Profiling.RegisterFlags(f)
	c.KafkaConfig.RegisterFlags(f)
	c.BlockBuilder.RegisterFlags(f)
	c.BlockScheduler.RegisterFlags(f)
}

func (c *Config) registerServerFlagsWithChangedDefaultValues(fs *flag.FlagSet) {
	throwaway := flag.NewFlagSet("throwaway", flag.PanicOnError)

	// Register to throwaway flags first. Default values are remembered during registration and cannot be changed,
	// but we can take values from throwaway flag set and reregister into supplied flags with new default values.
	c.Server.RegisterFlags(throwaway)
	c.InternalServer.RegisterFlags(throwaway)
	c.Pattern.RegisterFlags(throwaway)

	throwaway.VisitAll(func(f *flag.Flag) {
		// Ignore errors when setting new values. We have a test to verify that it works.
		switch f.Name {
		case "server.grpc.keepalive.min-time-between-pings":
			_ = f.Value.Set("10s")

		case "server.grpc.keepalive.ping-without-stream-allowed":
			_ = f.Value.Set("true")

		case "server.http-listen-port":
			_ = f.Value.Set("3100")

		case "pattern-ingester.distributor.replication-factor":
			_ = f.Value.Set("1")
		case "kafka-ingester.distributor.replication-factor":
			_ = f.Value.Set("1")
		}

		fs.Var(f.Value, f.Name, f.Usage)
	})

	c.Server.DisableRequestSuccessLog = true
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
	var errs []error

	if err := c.SchemaConfig.Validate(); err != nil {
		errs = append(errs, errors.Wrap(err, "CONFIG ERROR: invalid schema config"))
	}
	if err := c.StorageConfig.Validate(); err != nil {
		errs = append(errs, errors.Wrap(err, "CONFIG ERROR: invalid storage_config config"))
	}
	if err := c.QueryRange.Validate(); err != nil {
		errs = append(errs, errors.Wrap(err, "CONFIG ERROR: invalid query_range config"))
	}
	if err := c.Querier.Validate(); err != nil {
		errs = append(errs, errors.Wrap(err, "CONFIG ERROR: invalid querier config"))
	}
	if err := c.QueryScheduler.Validate(); err != nil {
		errs = append(errs, errors.Wrap(err, "CONFIG ERROR: invalid query_scheduler config"))
	}
	if err := c.TableManager.Validate(); err != nil {
		errs = append(errs, errors.Wrap(err, "CONFIG ERROR: invalid table_manager config"))
	}
	if err := c.Ruler.Validate(); err != nil {
		errs = append(errs, errors.Wrap(err, "CONFIG ERROR: invalid ruler config"))
	}
	if err := c.RulerStorage.Validate(); err != nil {
		errs = append(errs, errors.Wrap(err, "CONFIG ERROR: invalid ruler_storage config"))
	}
	if err := c.Ingester.Validate(); err != nil {
		errs = append(errs, errors.Wrap(err, "CONFIG ERROR: invalid ingester config"))
	}
	if err := c.BlockBuilder.Validate(); err != nil {
		errs = append(errs, errors.Wrap(err, "CONFIG ERROR: invalid block_builder config"))
	}
	if err := c.BlockScheduler.Validate(); err != nil {
		errs = append(errs, errors.Wrap(err, "CONFIG ERROR: invalid block_scheduler config"))
	}
	if err := c.LimitsConfig.Validate(); err != nil {
		errs = append(errs, errors.Wrap(err, "CONFIG ERROR: invalid limits_config config"))
	}
	if err := c.Worker.Validate(); err != nil {
		errs = append(errs, errors.Wrap(err, "CONFIG ERROR: invalid frontend_worker config"))
	}
	if err := c.StorageConfig.BoltDBShipperConfig.Validate(); err != nil {
		errs = append(errs, errors.Wrap(err, "CONFIG ERROR: invalid boltdb_shipper config"))
	}
	if err := c.IndexGateway.Validate(); err != nil {
		errs = append(errs, errors.Wrap(err, "CONFIG ERROR: invalid index_gateway config"))
	}
	if err := c.CompactorConfig.Validate(); err != nil {
		errs = append(errs, errors.Wrap(err, "CONFIG ERROR: invalid compactor config"))
	}
	if err := c.ChunkStoreConfig.Validate(); err != nil {
		errs = append(errs, errors.Wrap(err, "CONFIG ERROR: invalid chunk_store_config config"))
	}
	if err := c.QueryRange.Validate(); err != nil {
		errs = append(errs, errors.Wrap(err, "CONFIG ERROR: invalid query_range config"))
	}
	if err := c.BloomBuild.Validate(); err != nil {
		errs = append(errs, errors.Wrap(err, "CONFIG ERROR: invalid bloom_build config"))
	}
	if err := c.BloomGateway.Validate(); err != nil {
		errs = append(errs, errors.Wrap(err, "CONFIG ERROR: invalid bloom_gateway config"))
	}
	if err := c.Pattern.Validate(); err != nil {
		errs = append(errs, errors.Wrap(err, "CONFIG ERROR: invalid pattern_ingester config"))
	}
	if c.Ingester.KafkaIngestion.Enabled {
		if err := c.KafkaConfig.Validate(); err != nil {
			errs = append(errs, errors.Wrap(err, "CONFIG ERROR: invalid kafka_config config"))
		}
	}
	if err := c.Distributor.Validate(); err != nil {
		errs = append(errs, errors.Wrap(err, "CONFIG ERROR: invalid distributor config"))
	}

	errs = append(errs, validateSchemaValues(c)...)
	errs = append(errs, ValidateConfigCompatibility(*c)...)
	errs = append(errs, validateBackendAndLegacyReadMode(c)...)
	errs = append(errs, validateSchemaRequirements(c)...)
	errs = append(errs, validateDirectoriesExist(c)...)

	// The output format isn't great for this, so try to get the operators attention if there are multiple errors
	if len(errs) > 1 {
		errs = append([]error{fmt.Errorf("MULTIPLE CONFIG ERRORS FOUND, PLEASE READ CAREFULLY")}, errs...)
		return stdlib_errors.Join(errs...)
	} else if len(errs) == 1 {
		return errs[0]
	}

	return nil
}

func (c *Config) isTarget(m string) bool {
	return util.StringsContain(c.Target, m)
}

type Frontend interface {
	services.Service
	CheckReady(_ context.Context) error
}

// Codec defines methods to encode and decode requests from HTTP, httpgrpc and Protobuf.
type Codec interface {
	transport.Codec
	worker.RequestCodec
}

// Loki is the root datastructure for Loki.
type Loki struct {
	Cfg Config

	// set during initialization
	ModuleManager *modules.Manager
	serviceMap    map[string]services.Service
	deps          map[string][]string
	SignalHandler *signals.Handler

	Server                    *server.Server
	InternalServer            *server.Server
	ring                      *ring.Ring
	Overrides                 limiter.CombinedLimits
	tenantConfigs             *runtime.TenantConfigs
	TenantLimits              validation.TenantLimits
	distributor               *distributor.Distributor
	Ingester                  ingester.Interface
	PatternIngester           *pattern.Ingester
	PatternRingClient         pattern.RingClient
	Querier                   querier.Querier
	cacheGenerationLoader     queryrangebase.CacheGenNumberLoader
	querierAPI                *querier.QuerierAPI
	ingesterQuerier           *querier.IngesterQuerier
	Store                     storage.Store
	BloomStore                bloomshipper.Store
	tableManager              *index.TableManager
	frontend                  Frontend
	ruler                     *base_ruler.Ruler
	ruleEvaluator             ruler.Evaluator
	RulerStorage              rulestore.RuleStore
	rulerAPI                  *base_ruler.API
	stopper                   queryrange.Stopper
	runtimeConfig             *runtimeconfig.Manager
	MemberlistKV              *memberlist.KVInitService
	compactor                 *compactor.Compactor
	QueryFrontEndMiddleware   queryrangebase.Middleware
	queryScheduler            *scheduler.Scheduler
	querySchedulerRingManager *lokiring.RingManager
	usageReport               *analytics.Reporter
	indexGatewayRingManager   *lokiring.RingManager
	partitionRingWatcher      *ring.PartitionRingWatcher
	partitionRing             *ring.PartitionInstanceRing
	blockBuilder              *blockbuilder.BlockBuilder
	blockScheduler            *blockscheduler.BlockScheduler

	ClientMetrics       storage.ClientMetrics
	deleteClientMetrics *deletion.DeleteRequestClientMetrics

	Tee                distributor.Tee
	PushParserWrapper  push.RequestParserWrapper
	HTTPAuthMiddleware middleware.Interface

	Codec   Codec
	Metrics *server.Metrics

	UsageTracker push.UsageTracker
}

// New makes a new Loki.
func New(cfg Config) (*Loki, error) {
	loki := &Loki{
		Cfg:                 cfg,
		ClientMetrics:       storage.NewClientMetrics(),
		deleteClientMetrics: deletion.NewDeleteRequestClientMetrics(prometheus.DefaultRegisterer),
		Codec:               queryrange.DefaultCodec,
	}
	analytics.Edition("oss")
	loki.setupAuthMiddleware()
	loki.setupGRPCRecoveryMiddleware()
	if err := loki.setupModuleManager(); err != nil {
		return nil, err
	}

	return loki, nil
}

func (t *Loki) setupAuthMiddleware() {
	t.HTTPAuthMiddleware = fakeauth.SetupAuthMiddleware(&t.Cfg.Server, t.Cfg.AuthEnabled,
		// Also don't check auth for these gRPC methods, since single call is used for multiple users (or no user like health check).
		[]string{
			"/grpc.health.v1.Health/Check",
			"/grpc.health.v1.Health/Watch",
			"/logproto.StreamData/GetStreamRates",
			"/frontend.Frontend/Process",
			"/frontend.Frontend/NotifyClientShutdown",
			"/schedulerpb.SchedulerForFrontend/FrontendLoop",
			"/schedulerpb.SchedulerForQuerier/QuerierLoop",
			"/schedulerpb.SchedulerForQuerier/NotifyQuerierShutdown",
			"/blockbuilder.types.SchedulerService/GetJob",
			"/blockbuilder.types.SchedulerService/CompleteJob",
			"/blockbuilder.types.SchedulerService/SyncJob",
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
	// StartTime is the time at which the main() function started executing.
	// It is used to determine the startup time as well as the running time of the Loki process.
	StartTime time.Time
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
	startTime := time.Now()

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

	t.Server.HTTP.Path("/log_level").Methods("GET", "POST").Handler(util_log.LevelHandler(&t.Cfg.Server.LogLevel))

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
	logHook := func(msg, key string) func() {
		return func() {
			started := startTime
			if opts.StartTime.After(time.Time{}) {
				started = opts.StartTime
			}
			level.Info(util_log.Logger).Log("msg", msg, key, time.Since(started))
			_ = util_log.Flush()
		}
	}
	healthy := logHook("Loki started", "startup_time")
	stopped := logHook("Loki stopped", "running_time")
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

		// Pattern Ingester has a special check that makes sure that it was able to register into the ring,
		// and that all other ring entries are OK too.
		if t.PatternIngester != nil {
			if err := t.PatternIngester.CheckReady(r.Context()); err != nil {
				http.Error(w, "Pattern Ingester not ready: "+err.Error(), http.StatusServiceUnavailable)
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
	mm.RegisterModule(Querier, t.initQuerier)
	mm.RegisterModule(Ingester, t.initIngester)
	mm.RegisterModule(IngesterQuerier, t.initIngesterQuerier, modules.UserInvisibleModule)
	mm.RegisterModule(IngesterGRPCInterceptors, t.initIngesterGRPCInterceptors, modules.UserInvisibleModule)
	mm.RegisterModule(QueryFrontendTripperware, t.initQueryFrontendMiddleware, modules.UserInvisibleModule)
	mm.RegisterModule(QueryFrontend, t.initQueryFrontend)
	mm.RegisterModule(RulerStorage, t.initRulerStorage, modules.UserInvisibleModule)
	mm.RegisterModule(Ruler, t.initRuler)
	mm.RegisterModule(RuleEvaluator, t.initRuleEvaluator, modules.UserInvisibleModule)
	mm.RegisterModule(TableManager, t.initTableManager)
	mm.RegisterModule(Compactor, t.initCompactor)
	mm.RegisterModule(BloomStore, t.initBloomStore, modules.UserInvisibleModule)
	mm.RegisterModule(BloomPlanner, t.initBloomPlanner)
	mm.RegisterModule(BloomBuilder, t.initBloomBuilder)
	mm.RegisterModule(IndexGateway, t.initIndexGateway)
	mm.RegisterModule(IndexGatewayRing, t.initIndexGatewayRing, modules.UserInvisibleModule)
	mm.RegisterModule(IndexGatewayInterceptors, t.initIndexGatewayInterceptors, modules.UserInvisibleModule)
	mm.RegisterModule(BloomGateway, t.initBloomGateway)
	mm.RegisterModule(QueryScheduler, t.initQueryScheduler)
	mm.RegisterModule(QuerySchedulerRing, t.initQuerySchedulerRing, modules.UserInvisibleModule)
	mm.RegisterModule(Analytics, t.initAnalytics, modules.UserInvisibleModule)
	mm.RegisterModule(CacheGenerationLoader, t.initCacheGenerationLoader, modules.UserInvisibleModule)
	mm.RegisterModule(PatternRingClient, t.initPatternRingClient, modules.UserInvisibleModule)
	mm.RegisterModule(PatternIngesterTee, t.initPatternIngesterTee, modules.UserInvisibleModule)
	mm.RegisterModule(PatternIngester, t.initPatternIngester)
	mm.RegisterModule(PartitionRing, t.initPartitionRing, modules.UserInvisibleModule)
	mm.RegisterModule(BlockBuilder, t.initBlockBuilder)
	mm.RegisterModule(BlockScheduler, t.initBlockScheduler)

	mm.RegisterModule(All, nil)
	mm.RegisterModule(Read, nil)
	mm.RegisterModule(Write, nil)
	mm.RegisterModule(Backend, nil)

	// Add dependencies
	deps := map[string][]string{
		Ring:                     {RuntimeConfig, Server, MemberlistKV},
		Analytics:                {},
		Overrides:                {RuntimeConfig},
		OverridesExporter:        {Overrides, Server},
		TenantConfigs:            {RuntimeConfig},
		Distributor:              {Ring, Server, Overrides, TenantConfigs, PatternRingClient, PatternIngesterTee, Analytics, PartitionRing},
		Store:                    {Overrides, IndexGatewayRing},
		Ingester:                 {Store, Server, MemberlistKV, TenantConfigs, Analytics, PartitionRing},
		Querier:                  {Store, Ring, Server, IngesterQuerier, PatternRingClient, Overrides, Analytics, CacheGenerationLoader, QuerySchedulerRing},
		QueryFrontendTripperware: {Server, Overrides, TenantConfigs},
		QueryFrontend:            {QueryFrontendTripperware, Analytics, CacheGenerationLoader, QuerySchedulerRing},
		QueryScheduler:           {Server, Overrides, MemberlistKV, Analytics, QuerySchedulerRing},
		Ruler:                    {Ring, Server, RulerStorage, RuleEvaluator, Overrides, TenantConfigs, Analytics},
		RuleEvaluator:            {Ring, Server, Store, IngesterQuerier, Overrides, TenantConfigs, Analytics},
		TableManager:             {Server, Analytics},
		Compactor:                {Server, Overrides, MemberlistKV, Analytics},
		IndexGateway:             {Server, Store, BloomStore, IndexGatewayRing, IndexGatewayInterceptors, Analytics},
		BloomGateway:             {Server, BloomStore, Analytics},
		BloomPlanner:             {Server, BloomStore, Analytics, Store},
		BloomBuilder:             {Server, BloomStore, Analytics, Store},
		BloomStore:               {IndexGatewayRing},
		PatternRingClient:        {Server, MemberlistKV, Analytics},
		PatternIngesterTee:       {Server, MemberlistKV, Analytics, PatternRingClient},
		PatternIngester:          {Server, MemberlistKV, Analytics, PatternRingClient, PatternIngesterTee, Overrides},
		IngesterQuerier:          {Ring, PartitionRing, Overrides},
		QuerySchedulerRing:       {Overrides, MemberlistKV},
		IndexGatewayRing:         {Overrides, MemberlistKV},
		PartitionRing:            {MemberlistKV, Server, Ring},
		MemberlistKV:             {Server},
		BlockBuilder:             {PartitionRing, Store, Server},
		BlockScheduler:           {Server},

		Read:    {QueryFrontend, Querier},
		Write:   {Ingester, Distributor, PatternIngester},
		Backend: {QueryScheduler, Ruler, Compactor, IndexGateway, BloomPlanner, BloomBuilder, BloomGateway},

		All: {QueryScheduler, QueryFrontend, Querier, Ingester, PatternIngester, Distributor, Ruler, Compactor},
	}

	if t.Cfg.Querier.PerRequestLimitsEnabled {
		level.Debug(util_log.Logger).Log("msg", "per-query request limits support enabled")
		mm.RegisterModule(QueryLimiter, t.initQueryLimiter, modules.UserInvisibleModule)
		mm.RegisterModule(QueryLimitsInterceptors, t.initQueryLimitsInterceptors, modules.UserInvisibleModule)

		// This module is defunct but the target remains for backwards compatibility.
		mm.RegisterModule(QueryLimitsTripperware, func() (services.Service, error) { return nil, nil }, modules.UserInvisibleModule)

		// Ensure query limiter embeds overrides after they've been
		// created.
		deps[QueryLimiter] = []string{Overrides}
		deps[QueryLimitsInterceptors] = []string{}

		deps[Querier] = append(deps[Querier], QueryLimiter)

		// query frontend tripperware uses t.Overrides. Make sure it
		// uses the one wrapped by query limiter.
		deps[QueryFrontendTripperware] = append(deps[QueryFrontendTripperware], QueryLimiter)

		if err := mm.AddDependency(Server, QueryLimitsInterceptors); err != nil {
			return err
		}
	}

	// Add IngesterQuerier as a dependency for store when target is either querier, ruler, read, or backend.
	if t.Cfg.isTarget(Querier) || t.Cfg.isTarget(Ruler) || t.Cfg.isTarget(Read) || t.Cfg.isTarget(Backend) {
		deps[Store] = append(deps[Store], IngesterQuerier)
	}

	// If the query scheduler and querier are running together, make sure the scheduler goes
	// first to initialize the ring that will also be used by the querier
	if (t.Cfg.isTarget(Querier) && t.Cfg.isTarget(QueryScheduler)) || t.Cfg.isTarget(All) {
		deps[Querier] = append(deps[Querier], QueryScheduler)
	}

	// If the query scheduler and query frontend are running together, make sure the scheduler goes
	// first to initialize the ring that will also be used by the query frontend
	if (t.Cfg.isTarget(QueryFrontend) && t.Cfg.isTarget(QueryScheduler)) || t.Cfg.isTarget(All) {
		deps[QueryFrontend] = append(deps[QueryFrontend], QueryScheduler)
	}

	// Initialise query tags interceptors on targets running ingester
	if t.Cfg.isTarget(Ingester) || t.Cfg.isTarget(Write) || t.Cfg.isTarget(All) {
		deps[Server] = append(deps[Server], IngesterGRPCInterceptors)
	}

	if t.Cfg.LegacyReadTarget {
		deps[Read] = append(deps[Read], deps[Backend]...)
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
		if err := mm.AddDependency(Analytics, Ring); err != nil {
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
