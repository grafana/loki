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
	"github.com/pkg/errors"
	prom_storage "github.com/prometheus/prometheus/storage"
	"github.com/weaveworks/common/middleware"
	"github.com/weaveworks/common/server"
	"google.golang.org/grpc"
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
	"github.com/cortexproject/cortex/pkg/storage/tsdb"
	"github.com/cortexproject/cortex/pkg/storegateway"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/grpc/healthcheck"
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
	Target      ModuleName `yaml:"target"`
	AuthEnabled bool       `yaml:"auth_enabled"`
	PrintConfig bool       `yaml:"-"`
	HTTPPrefix  string     `yaml:"http_prefix"`

	API              api.Config               `yaml:"api"`
	Server           server.Config            `yaml:"server"`
	Distributor      distributor.Config       `yaml:"distributor"`
	Querier          querier.Config           `yaml:"querier"`
	IngesterClient   client.Config            `yaml:"ingester_client"`
	Ingester         ingester.Config          `yaml:"ingester"`
	Flusher          flusher.Config           `yaml:"flusher"`
	Storage          storage.Config           `yaml:"storage"`
	ChunkStore       chunk.StoreConfig        `yaml:"chunk_store"`
	Schema           chunk.SchemaConfig       `yaml:"schema" doc:"hidden"` // Doc generation tool doesn't support it because part of the SchemaConfig doesn't support CLI flags (needs manual documentation)
	LimitsConfig     validation.Limits        `yaml:"limits"`
	Prealloc         client.PreallocConfig    `yaml:"prealloc" doc:"hidden"`
	Worker           frontend.WorkerConfig    `yaml:"frontend_worker"`
	Frontend         frontend.Config          `yaml:"frontend"`
	QueryRange       queryrange.Config        `yaml:"query_range"`
	TableManager     chunk.TableManagerConfig `yaml:"table_manager"`
	Encoding         encoding.Config          `yaml:"-"` // No yaml for this, it only works with flags.
	TSDB             tsdb.Config              `yaml:"tsdb"`
	Compactor        compactor.Config         `yaml:"compactor"`
	StoreGateway     storegateway.Config      `yaml:"store_gateway"`
	DataPurgerConfig purger.Config            `yaml:"purger"`

	Ruler         ruler.Config                               `yaml:"ruler"`
	Configs       configs.Config                             `yaml:"configs"`
	Alertmanager  alertmanager.MultitenantAlertmanagerConfig `yaml:"alertmanager"`
	RuntimeConfig runtimeconfig.ManagerConfig                `yaml:"runtime_config"`
	MemberlistKV  memberlist.KVConfig                        `yaml:"memberlist"`
}

// RegisterFlags registers flag.
func (c *Config) RegisterFlags(f *flag.FlagSet) {
	c.Server.MetricsNamespace = "cortex"
	c.Target = All
	c.Server.ExcludeRequestInLog = true
	f.Var(&c.Target, "target", "The Cortex service to run. Supported values are: all, distributor, ingester, querier, query-frontend, table-manager, ruler, alertmanager, configs.")
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
	c.TSDB.RegisterFlags(f)
	c.Compactor.RegisterFlags(f)
	c.StoreGateway.RegisterFlags(f)
	c.DataPurgerConfig.RegisterFlags(f)

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
	if err := c.TSDB.Validate(); err != nil {
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
	if err := c.QueryRange.Validate(log); err != nil {
		return errors.Wrap(err, "invalid queryrange config")
	}
	if err := c.TableManager.Validate(); err != nil {
		return errors.Wrap(err, "invalid tablemanager config")
	}
	return nil
}

// Cortex is the root datastructure for Cortex.
type Cortex struct {
	target ModuleName

	// set during initialization
	serviceMap map[ModuleName]services.Service

	api              *api.API
	server           *server.Server
	ring             *ring.Ring
	overrides        *validation.Overrides
	distributor      *distributor.Distributor
	ingester         *ingester.Ingester
	flusher          *flusher.Flusher
	store            chunk.Store
	deletesStore     *purger.DeleteStore
	frontend         *frontend.Frontend
	tableManager     *chunk.TableManager
	cache            cache.Cache
	runtimeConfig    *runtimeconfig.Manager
	dataPurger       *purger.DataPurger
	tombstonesLoader *purger.TombstonesLoader

	ruler        *ruler.Ruler
	configAPI    *configAPI.API
	configDB     db.DB
	alertmanager *alertmanager.MultitenantAlertmanager
	compactor    *compactor.Compactor
	storeGateway *storegateway.StoreGateway
	memberlistKV *memberlist.KVInit

	// Queryable that the querier should use to query the long
	// term storage. It depends on the storage engine used.
	storeQueryable prom_storage.Queryable
}

// New makes a new Cortex.
func New(cfg Config) (*Cortex, error) {
	if cfg.PrintConfig {
		if err := yaml.NewEncoder(os.Stdout).Encode(&cfg); err != nil {
			fmt.Println("Error encoding config:", err)
		}
		os.Exit(0)
	}

	cortex := &Cortex{
		target: cfg.Target,
	}

	cortex.setupAuthMiddleware(&cfg)

	serviceMap, err := cortex.initModuleServices(&cfg, cfg.Target)
	if err != nil {
		return nil, err
	}

	cortex.serviceMap = serviceMap
	cortex.api.RegisterServiceMapHandler(http.HandlerFunc(cortex.servicesHandler))

	return cortex, nil
}

func (t *Cortex) setupAuthMiddleware(cfg *Config) {
	if cfg.AuthEnabled {
		cfg.Server.GRPCMiddleware = []grpc.UnaryServerInterceptor{
			middleware.ServerUserHeaderInterceptor,
		}
		cfg.Server.GRPCStreamMiddleware = []grpc.StreamServerInterceptor{
			func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
				switch info.FullMethod {
				// Don't check auth header on TransferChunks, as we weren't originally
				// sending it and this could cause transfers to fail on update.
				//
				// Also don't check auth /frontend.Frontend/Process, as this handles
				// queries for multiple users.
				case "/cortex.Ingester/TransferChunks", "/frontend.Frontend/Process":
					return handler(srv, ss)
				default:
					return middleware.StreamServerUserHeaderInterceptor(srv, ss, info, handler)
				}
			},
		}
	} else {
		cfg.Server.GRPCMiddleware = []grpc.UnaryServerInterceptor{
			fakeGRPCAuthUniaryMiddleware,
		}
		cfg.Server.GRPCStreamMiddleware = []grpc.StreamServerInterceptor{
			fakeGRPCAuthStreamMiddleware,
		}
		cfg.API.HTTPAuthMiddleware = fakeHTTPAuthMiddleware
	}
}

func (t *Cortex) initModuleServices(cfg *Config, target ModuleName) (map[ModuleName]services.Service, error) {
	servicesMap := map[ModuleName]services.Service{}

	// initialize all of our dependencies first
	deps := orderedDeps(target)
	deps = append(deps, target) // lastly, initialize the requested module

	for ix, n := range deps {
		mod := modules[n]

		var serv services.Service

		if mod.service != nil {
			s, err := mod.service(t, cfg)
			if err != nil {
				return nil, errors.Wrap(err, fmt.Sprintf("error initialising module: %s", n))
			}
			serv = s
		} else if mod.wrappedService != nil {
			s, err := mod.wrappedService(t, cfg)
			if err != nil {
				return nil, errors.Wrap(err, fmt.Sprintf("error initialising module: %s", n))
			}
			if s != nil {
				// We pass servicesMap, which isn't yet finished. By the time service starts,
				// it will be fully built, so there is no need for extra synchronization.
				serv = newModuleServiceWrapper(servicesMap, n, s, mod.deps, findInverseDependencies(n, deps[ix+1:]))
			}
		}

		if serv != nil {
			servicesMap[n] = serv
		}
	}

	return servicesMap, nil
}

// Run starts Cortex running, and blocks until a Cortex stops.
func (t *Cortex) Run() error {
	// get all services, create service manager and tell it to start
	servs := []services.Service(nil)
	for _, s := range t.serviceMap {
		servs = append(servs, s)
	}

	sm, err := services.NewManager(servs...)
	if err != nil {
		return err
	}

	// before starting servers, register /ready handler and gRPC health check service.
	// It should reflect entire Cortex.
	t.server.HTTP.Path("/ready").Handler(t.readyHandler(sm))
	grpc_health_v1.RegisterHealthServer(t.server.GRPC, healthcheck.New(sm))

	// Let's listen for events from this manager, and log them.
	healthy := func() { level.Info(util.Logger).Log("msg", "Cortex started") }
	stopped := func() { level.Info(util.Logger).Log("msg", "Cortex stopped") }
	serviceFailed := func(service services.Service) {
		// if any service fails, stop entire Cortex
		sm.StopAsync()

		// let's find out which module failed
		for m, s := range t.serviceMap {
			if s == service {
				if service.FailureCase() == util.ErrStopProcess {
					level.Info(util.Logger).Log("msg", "received stop signal via return error", "module", m, "error", service.FailureCase())
				} else {
					level.Error(util.Logger).Log("msg", "module failed", "module", m, "error", service.FailureCase())
				}
				return
			}
		}

		level.Error(util.Logger).Log("msg", "module failed", "module", "unknown", "error", service.FailureCase())
	}

	sm.AddListener(services.NewManagerListener(healthy, stopped, serviceFailed))

	// Currently it's the Server that reacts on signal handler,
	// so get Server service, and wait until it gets to Stopping state.
	// It will also be stopped via service manager if any service fails (see attached service listener)
	// Attach listener before starting services, or we may miss the notification.
	serverStopping := make(chan struct{})
	t.serviceMap[Server].AddListener(services.NewListener(nil, nil, func(from services.State) {
		close(serverStopping)
	}, nil, nil))

	// Start all services. This can really only fail if some service is already
	// in other state than New, which should not be the case.
	err = sm.StartAsync(context.Background())
	if err == nil {
		// no error starting the services, now let's just wait until Server module
		// transitions to Stopping (after SIGTERM or when some service fails),
		// and then initiate shutdown
		<-serverStopping
	}

	// Stop all the services, and wait until they are all done.
	// We don't care about this error, as it cannot really fail.
	_ = services.StopManagerAndAwaitStopped(context.Background(), sm)

	// if any service failed, report that as an error to caller
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
		if t.ingester != nil {
			if err := t.ingester.CheckReady(r.Context()); err != nil {
				http.Error(w, "Ingester not ready: "+err.Error(), http.StatusServiceUnavailable)
				return
			}
		}

		http.Error(w, "ready", http.StatusOK)
	}
}

// listDeps recursively gets a list of dependencies for a passed moduleName
func listDeps(m ModuleName) []ModuleName {
	deps := modules[m].deps
	for _, d := range modules[m].deps {
		deps = append(deps, listDeps(d)...)
	}
	return deps
}

// orderedDeps gets a list of all dependencies ordered so that items are always after any of their dependencies.
func orderedDeps(m ModuleName) []ModuleName {
	deps := listDeps(m)

	// get a unique list of moduleNames, with a flag for whether they have been added to our result
	uniq := map[ModuleName]bool{}
	for _, dep := range deps {
		uniq[dep] = false
	}

	result := make([]ModuleName, 0, len(uniq))

	// keep looping through all modules until they have all been added to the result.

	for len(result) < len(uniq) {
	OUTER:
		for name, added := range uniq {
			if added {
				continue
			}
			for _, dep := range modules[name].deps {
				// stop processing this module if one of its dependencies has
				// not been added to the result yet.
				if !uniq[dep] {
					continue OUTER
				}
			}

			// if all of the module's dependencies have been added to the result slice,
			// then we can safely add this module to the result slice as well.
			uniq[name] = true
			result = append(result, name)
		}
	}
	return result
}

// find modules in the supplied list, that depend on mod
func findInverseDependencies(mod ModuleName, mods []ModuleName) []ModuleName {
	result := []ModuleName(nil)

	for _, n := range mods {
		for _, d := range modules[n].deps {
			if d == mod {
				result = append(result, n)
				break
			}
		}
	}

	return result
}
