package cortex

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"github.com/thanos-io/thanos/pkg/tracing"
	"github.com/weaveworks/common/server"
	"github.com/weaveworks/common/signals"
	"google.golang.org/grpc/health/grpc_health_v1"
	"gopkg.in/yaml.v2"

	"github.com/cortexproject/cortex/pkg/alertmanager"
	"github.com/cortexproject/cortex/pkg/api"
	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/cache"
	"github.com/cortexproject/cortex/pkg/chunk/encoding"
	"github.com/cortexproject/cortex/pkg/chunk/purger"
	"github.com/cortexproject/cortex/pkg/chunk/storage"
	chunk_util "github.com/cortexproject/cortex/pkg/chunk/util"
	"github.com/cortexproject/cortex/pkg/compactor"
	"github.com/cortexproject/cortex/pkg/configs"
	configAPI "github.com/cortexproject/cortex/pkg/configs/api"
	"github.com/cortexproject/cortex/pkg/configs/db"
	"github.com/cortexproject/cortex/pkg/distributor"
	"github.com/cortexproject/cortex/pkg/flusher"
	"github.com/cortexproject/cortex/pkg/ingester"
	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/querier"
	"github.com/cortexproject/cortex/pkg/querier/frontend"
	"github.com/cortexproject/cortex/pkg/querier/queryrange"
	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/ring/kv/memberlist"
	"github.com/cortexproject/cortex/pkg/ruler"
	"github.com/cortexproject/cortex/pkg/ruler/rules"
	"github.com/cortexproject/cortex/pkg/storage/tsdb"
	"github.com/cortexproject/cortex/pkg/storegateway"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/fakeauth"
	"github.com/cortexproject/cortex/pkg/util/grpc/healthcheck"
	"github.com/cortexproject/cortex/pkg/util/modules"
	"github.com/cortexproject/cortex/pkg/util/runtimeconfig"
	"github.com/cortexproject/cortex/pkg/util/services"
	"github.com/cortexproject/cortex/pkg/util/validation"
)

// The design pattern for Cortex is a series of config objects, which are
// registered for command line flags, and then a series of components that
// are instantiated and composed.  Some rules of thumb:
// - Config types should only contain 'simple' types (ints, strings, urls etc).
// - Flag validation should be done by the flag; use a flag.Value where
//   appropriate.
// - Config types should map 1:1 with a component type.
// - Config types should define flags with a common prefix.
// - It's fine to nest configs within configs, but this should match the
//   nesting of components within components.
// - Limit as much is possible sharing of configuration between config types.
//   Where necessary, use a pointer for this - avoid repetition.
// - Where a nesting of components its not obvious, it's fine to pass
//   references to other components constructors to compose them.
// - First argument for a components constructor should be its matching config
//   object.

// Config is the root config for Cortex.
type Config struct {
	Target      string `yaml:"target"`
	AuthEnabled bool   `yaml:"auth_enabled"`
	PrintConfig bool   `yaml:"-"`
	HTTPPrefix  string `yaml:"http_prefix"`
	ListModules bool   `yaml:"-"` // No yaml for this, it only works with flags.

	API            api.Config               `yaml:"api"`
	Server         server.Config            `yaml:"server"`
	Distributor    distributor.Config       `yaml:"distributor"`
	Querier        querier.Config           `yaml:"querier"`
	IngesterClient client.Config            `yaml:"ingester_client"`
	Ingester       ingester.Config          `yaml:"ingester"`
	Flusher        flusher.Config           `yaml:"flusher"`
	Storage        storage.Config           `yaml:"storage"`
	ChunkStore     chunk.StoreConfig        `yaml:"chunk_store"`
	Schema         chunk.SchemaConfig       `yaml:"schema" doc:"hidden"` // Doc generation tool doesn't support it because part of the SchemaConfig doesn't support CLI flags (needs manual documentation)
	LimitsConfig   validation.Limits        `yaml:"limits"`
	Prealloc       client.PreallocConfig    `yaml:"prealloc" doc:"hidden"`
	Worker         frontend.WorkerConfig    `yaml:"frontend_worker"`
	Frontend       frontend.Config          `yaml:"frontend"`
	QueryRange     queryrange.Config        `yaml:"query_range"`
	TableManager   chunk.TableManagerConfig `yaml:"table_manager"`
	Encoding       encoding.Config          `yaml:"-"` // No yaml for this, it only works with flags.
	BlocksStorage  tsdb.BlocksStorageConfig `yaml:"blocks_storage"`
	Compactor      compactor.Config         `yaml:"compactor"`
	StoreGateway   storegateway.Config      `yaml:"store_gateway"`
	PurgerConfig   purger.Config            `yaml:"purger"`

	Ruler         ruler.Config                               `yaml:"ruler"`
	Configs       configs.Config                             `yaml:"configs"`
	Alertmanager  alertmanager.MultitenantAlertmanagerConfig `yaml:"alertmanager"`
	RuntimeConfig runtimeconfig.ManagerConfig                `yaml:"runtime_config"`
	MemberlistKV  memberlist.KVConfig                        `yaml:"memberlist"`
}

// RegisterFlags registers flag.
func (c *Config) RegisterFlags(f *flag.FlagSet) {
	c.Server.MetricsNamespace = "cortex"
	c.Server.ExcludeRequestInLog = true
	f.StringVar(&c.Target, "target", All, "The Cortex service to run. Use \"-modules\" command line flag to get a list of available options.")
	f.BoolVar(&c.ListModules, "modules", false, "List available values to be use as target. Cannot be used in YAML config.")
	f.BoolVar(&c.AuthEnabled, "auth.enabled", true, "Set to false to disable auth.")
	f.BoolVar(&c.PrintConfig, "print.config", false, "Print the config and exit.")
	f.StringVar(&c.HTTPPrefix, "http.prefix", "/api/prom", "HTTP path prefix for Cortex API.")

	c.API.RegisterFlags(f)
	c.Server.RegisterFlags(f)
	c.Distributor.RegisterFlags(f)
	c.Querier.RegisterFlags(f)
	c.IngesterClient.RegisterFlags(f)
	c.Ingester.RegisterFlags(f)
	c.Flusher.RegisterFlags(f)
	c.Storage.RegisterFlags(f)
	c.ChunkStore.RegisterFlags(f)
	c.Schema.RegisterFlags(f)
	c.LimitsConfig.RegisterFlags(f)
	c.Prealloc.RegisterFlags(f)
	c.Worker.RegisterFlags(f)
	c.Frontend.RegisterFlags(f)
	c.QueryRange.RegisterFlags(f)
	c.TableManager.RegisterFlags(f)
	c.Encoding.RegisterFlags(f)
	c.BlocksStorage.RegisterFlags(f)
	c.Compactor.RegisterFlags(f)
	c.StoreGateway.RegisterFlags(f)
	c.PurgerConfig.RegisterFlags(f)

	c.Ruler.RegisterFlags(f)
	c.Configs.RegisterFlags(f)
	c.Alertmanager.RegisterFlags(f)
	c.RuntimeConfig.RegisterFlags(f)
	c.MemberlistKV.RegisterFlags(f, "")

	// These don't seem to have a home.
	flag.IntVar(&chunk_util.QueryParallelism, "querier.query-parallelism", 100, "Max subqueries run in parallel per higher-level query.")
}

// Validate the cortex config and returns an error if the validation
// doesn't pass
func (c *Config) Validate(log log.Logger) error {
	if err := c.Schema.Validate(); err != nil {
		return errors.Wrap(err, "invalid schema config")
	}
	if err := c.Encoding.Validate(); err != nil {
		return errors.Wrap(err, "invalid encoding config")
	}
	if err := c.Storage.Validate(); err != nil {
		return errors.Wrap(err, "invalid storage config")
	}
	if err := c.ChunkStore.Validate(); err != nil {
		return errors.Wrap(err, "invalid chunk store config")
	}
	if err := c.Ruler.Validate(); err != nil {
		return errors.Wrap(err, "invalid ruler config")
	}
	if err := c.BlocksStorage.Validate(); err != nil {
		return errors.Wrap(err, "invalid TSDB config")
	}
	if err := c.LimitsConfig.Validate(c.Distributor.ShardByAllLabels); err != nil {
		return errors.Wrap(err, "invalid limits config")
	}
	if err := c.Distributor.Validate(); err != nil {
		return errors.Wrap(err, "invalid distributor config")
	}
	if err := c.Querier.Validate(); err != nil {
		return errors.Wrap(err, "invalid querier config")
	}
	if err := c.IngesterClient.Validate(log); err != nil {
		return errors.Wrap(err, "invalid ingester_client config")
	}
	if err := c.Worker.Validate(log); err != nil {
		return errors.Wrap(err, "invalid frontend_worker config")
	}
	if err := c.QueryRange.Validate(log); err != nil {
		return errors.Wrap(err, "invalid query_range config")
	}
	if err := c.TableManager.Validate(); err != nil {
		return errors.Wrap(err, "invalid table-manager config")
	}
	if err := c.StoreGateway.Validate(c.LimitsConfig); err != nil {
		return errors.Wrap(err, "invalid store-gateway config")
	}

	if c.Storage.Engine == storage.StorageEngineBlocks && c.Querier.SecondStoreEngine != storage.StorageEngineChunks && len(c.Schema.Configs) > 0 {
		level.Warn(log).Log("schema configuration is not used by the blocks storage engine, and will have no effect")
	}

	return nil
}

// Cortex is the root datastructure for Cortex.
type Cortex struct {
	Cfg Config

	// set during initialization
	ServiceMap    map[string]services.Service
	ModuleManager *modules.Manager

	API              *api.API
	Server           *server.Server
	Ring             *ring.Ring
	Overrides        *validation.Overrides
	Distributor      *distributor.Distributor
	Ingester         *ingester.Ingester
	Flusher          *flusher.Flusher
	Store            chunk.Store
	DeletesStore     *purger.DeleteStore
	Frontend         *frontend.Frontend
	TableManager     *chunk.TableManager
	Cache            cache.Cache
	RuntimeConfig    *runtimeconfig.Manager
	Purger           *purger.Purger
	TombstonesLoader *purger.TombstonesLoader

	Ruler        *ruler.Ruler
	RulerStorage rules.RuleStore
	ConfigAPI    *configAPI.API
	ConfigDB     db.DB
	Alertmanager *alertmanager.MultitenantAlertmanager
	Compactor    *compactor.Compactor
	StoreGateway *storegateway.StoreGateway
	MemberlistKV *memberlist.KVInitService

	// Queryables that the querier should use to query the long
	// term storage. It depends on the storage engine used.
	StoreQueryables []querier.QueryableWithFilter
}

// New makes a new Cortex.
func New(cfg Config) (*Cortex, error) {
	if cfg.PrintConfig {
		if err := yaml.NewEncoder(os.Stdout).Encode(&cfg); err != nil {
			fmt.Println("Error encoding config:", err)
		}
		os.Exit(0)
	}

	// Don't check auth header on TransferChunks, as we weren't originally
	// sending it and this could cause transfers to fail on update.
	//
	// Also don't check auth /frontend.Frontend/Process, as this handles
	// queries for multiple users.
	cfg.API.HTTPAuthMiddleware = fakeauth.SetupAuthMiddleware(&cfg.Server, cfg.AuthEnabled,
		[]string{"/cortex.Ingester/TransferChunks", "/frontend.Frontend/Process"})

	cortex := &Cortex{
		Cfg: cfg,
	}

	cortex.setupThanosTracing()

	if err := cortex.setupModuleManager(); err != nil {
		return nil, err
	}

	return cortex, nil
}

// setupThanosTracing appends a gRPC middleware used to inject our tracer into the custom
// context used by Thanos, in order to get Thanos spans correctly attached to our traces.
func (t *Cortex) setupThanosTracing() {
	t.Cfg.Server.GRPCMiddleware = append(t.Cfg.Server.GRPCMiddleware,
		tracing.UnaryServerInterceptor(opentracing.GlobalTracer()),
	)

	t.Cfg.Server.GRPCStreamMiddleware = append(t.Cfg.Server.GRPCStreamMiddleware,
		tracing.StreamServerInterceptor(opentracing.GlobalTracer()),
	)
}

// Run starts Cortex running, and blocks until a Cortex stops.
func (t *Cortex) Run() error {
	if !t.ModuleManager.IsUserVisibleModule(t.Cfg.Target) {
		level.Warn(util.Logger).Log("msg", "selected target is an internal module, is this intended?", "target", t.Cfg.Target)
	}

	serviceMap, err := t.ModuleManager.InitModuleServices(t.Cfg.Target)
	if err != nil {
		return err
	}

	t.ServiceMap = serviceMap
	t.API.RegisterServiceMapHandler(http.HandlerFunc(t.servicesHandler))

	// get all services, create service manager and tell it to start
	servs := []services.Service(nil)
	for _, s := range t.ServiceMap {
		servs = append(servs, s)
	}

	sm, err := services.NewManager(servs...)
	if err != nil {
		return err
	}

	// before starting servers, register /ready handler and gRPC health check service.
	// It should reflect entire Cortex.
	t.Server.HTTP.Path("/ready").Handler(t.readyHandler(sm))
	grpc_health_v1.RegisterHealthServer(t.Server.GRPC, healthcheck.New(sm))

	// Let's listen for events from this manager, and log them.
	healthy := func() { level.Info(util.Logger).Log("msg", "Cortex started") }
	stopped := func() { level.Info(util.Logger).Log("msg", "Cortex stopped") }
	serviceFailed := func(service services.Service) {
		// if any service fails, stop entire Cortex
		sm.StopAsync()

		// let's find out which module failed
		for m, s := range t.ServiceMap {
			if s == service {
				if service.FailureCase() == util.ErrStopProcess {
					level.Info(util.Logger).Log("msg", "received stop signal via return error", "module", m, "err", service.FailureCase())
				} else {
					level.Error(util.Logger).Log("msg", "module failed", "module", m, "err", service.FailureCase())
				}
				return
			}
		}

		level.Error(util.Logger).Log("msg", "module failed", "module", "unknown", "err", service.FailureCase())
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
				if f.FailureCase() != util.ErrStopProcess {
					// Details were reported via failure listener before
					err = errors.New("failed services")
					break
				}
			}
		}
	}
	return err
}

func (t *Cortex) readyHandler(sm *services.Manager) http.HandlerFunc {
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
		if t.Frontend != nil {
			if err := t.Frontend.CheckReady(r.Context()); err != nil {
				http.Error(w, "Query Frontend not ready: "+err.Error(), http.StatusServiceUnavailable)
				return
			}
		}

		http.Error(w, "ready", http.StatusOK)
	}
}
