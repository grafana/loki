package loki

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/querier/frontend"
	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/ring/kv/memberlist"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/runtimeconfig"
	"github.com/cortexproject/cortex/pkg/util/services"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/weaveworks/common/middleware"
	"github.com/weaveworks/common/server"
	"google.golang.org/grpc"

	"github.com/grafana/loki/pkg/distributor"
	"github.com/grafana/loki/pkg/ingester"
	"github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/querier"
	"github.com/grafana/loki/pkg/querier/queryrange"
	"github.com/grafana/loki/pkg/storage"
	serverutil "github.com/grafana/loki/pkg/util/server"
	"github.com/grafana/loki/pkg/util/validation"
)

// Config is the root config for Loki.
type Config struct {
	Target      moduleName `yaml:"target,omitempty"`
	AuthEnabled bool       `yaml:"auth_enabled,omitempty"`
	HTTPPrefix  string     `yaml:"http_prefix"`

	Server           server.Config               `yaml:"server,omitempty"`
	Distributor      distributor.Config          `yaml:"distributor,omitempty"`
	Querier          querier.Config              `yaml:"querier,omitempty"`
	IngesterClient   client.Config               `yaml:"ingester_client,omitempty"`
	Ingester         ingester.Config             `yaml:"ingester,omitempty"`
	StorageConfig    storage.Config              `yaml:"storage_config,omitempty"`
	ChunkStoreConfig chunk.StoreConfig           `yaml:"chunk_store_config,omitempty"`
	SchemaConfig     chunk.SchemaConfig          `yaml:"schema_config,omitempty"`
	LimitsConfig     validation.Limits           `yaml:"limits_config,omitempty"`
	TableManager     chunk.TableManagerConfig    `yaml:"table_manager,omitempty"`
	Worker           frontend.WorkerConfig       `yaml:"frontend_worker,omitempty"`
	Frontend         frontend.Config             `yaml:"frontend,omitempty"`
	QueryRange       queryrange.Config           `yaml:"query_range,omitempty"`
	RuntimeConfig    runtimeconfig.ManagerConfig `yaml:"runtime_config,omitempty"`
	MemberlistKV     memberlist.KVConfig         `yaml:"memberlist"`
}

// RegisterFlags registers flag.
func (c *Config) RegisterFlags(f *flag.FlagSet) {
	c.Server.MetricsNamespace = "loki"
	c.Target = All
	c.Server.ExcludeRequestInLog = true
	f.Var(&c.Target, "target", "target module (default All)")
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
	c.Worker.RegisterFlags(f)
	c.QueryRange.RegisterFlags(f)
	c.RuntimeConfig.RegisterFlags(f)
}

// Validate the config and returns an error if the validation
// doesn't pass
func (c *Config) Validate(log log.Logger) error {
	if err := c.SchemaConfig.Validate(); err != nil {
		return errors.Wrap(err, "invalid schema config")
	}
	if err := c.StorageConfig.Validate(); err != nil {
		return errors.Wrap(err, "invalid storage config")
	}
	if err := c.QueryRange.Validate(log); err != nil {
		return errors.Wrap(err, "invalid queryrange config")
	}
	if err := c.TableManager.Validate(); err != nil {
		return errors.Wrap(err, "invalid tablemanager config")
	}
	return nil
}

// Loki is the root datastructure for Loki.
type Loki struct {
	cfg Config

	// set during initialization
	serviceMap map[moduleName]services.Service

	server        *server.Server
	ring          *ring.Ring
	overrides     *validation.Overrides
	distributor   *distributor.Distributor
	ingester      *ingester.Ingester
	querier       *querier.Querier
	store         storage.Store
	tableManager  *chunk.TableManager
	frontend      *frontend.Frontend
	stopper       queryrange.Stopper
	runtimeConfig *runtimeconfig.Manager
	memberlistKV  *memberlist.KVInit

	httpAuthMiddleware middleware.Interface
}

// New makes a new Loki.
func New(cfg Config) (*Loki, error) {
	loki := &Loki{
		cfg: cfg,
	}

	loki.setupAuthMiddleware()
	storage.RegisterCustomIndexClients(cfg.StorageConfig, prometheus.DefaultRegisterer)

	serviceMap, err := loki.initModuleServices(cfg.Target)
	if err != nil {
		return nil, err
	}

	loki.serviceMap = serviceMap
	loki.server.HTTP.Handle("/services", http.HandlerFunc(loki.servicesHandler))

	return loki, nil
}

func (t *Loki) setupAuthMiddleware() {
	t.cfg.Server.GRPCMiddleware = []grpc.UnaryServerInterceptor{serverutil.RecoveryGRPCUnaryInterceptor}
	t.cfg.Server.GRPCStreamMiddleware = []grpc.StreamServerInterceptor{serverutil.RecoveryGRPCStreamInterceptor}
	if t.cfg.AuthEnabled {
		t.cfg.Server.GRPCMiddleware = append(t.cfg.Server.GRPCMiddleware, middleware.ServerUserHeaderInterceptor)
		t.cfg.Server.GRPCStreamMiddleware = append(t.cfg.Server.GRPCStreamMiddleware, GRPCStreamAuthInterceptor)
		t.httpAuthMiddleware = middleware.AuthenticateUser
	} else {
		t.cfg.Server.GRPCMiddleware = append(t.cfg.Server.GRPCMiddleware, fakeGRPCAuthUnaryMiddleware)
		t.cfg.Server.GRPCStreamMiddleware = append(t.cfg.Server.GRPCStreamMiddleware, fakeGRPCAuthStreamMiddleware)
		t.httpAuthMiddleware = fakeHTTPAuthMiddleware
	}
}

var GRPCStreamAuthInterceptor = func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	switch info.FullMethod {
	// Don't check auth header on TransferChunks, as we weren't originally
	// sending it and this could cause transfers to fail on update.
	//
	// Also don't check auth /frontend.Frontend/Process, as this handles
	// queries for multiple users.
	case "/logproto.Ingester/TransferChunks", "/frontend.Frontend/Process":
		return handler(srv, ss)
	default:
		return middleware.StreamServerUserHeaderInterceptor(srv, ss, info, handler)
	}
}

func (t *Loki) initModuleServices(target moduleName) (map[moduleName]services.Service, error) {
	servicesMap := map[moduleName]services.Service{}

	// initialize all of our dependencies first
	deps := orderedDeps(target)
	deps = append(deps, target) // lastly, initialize the requested module

	for ix, n := range deps {
		mod := modules[n]

		var serv services.Service

		if mod.service != nil {
			s, err := mod.service(t)
			if err != nil {
				return nil, errors.Wrap(err, fmt.Sprintf("error initialising module: %s", n))
			}
			serv = s
		} else if mod.wrappedService != nil {
			s, err := mod.wrappedService(t)
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

// Run starts Loki running, and blocks until a Loki stops.
func (t *Loki) Run() error {
	// get all services, create service manager and tell it to start
	var servs []services.Service
	for _, s := range t.serviceMap {
		servs = append(servs, s)
	}

	sm, err := services.NewManager(servs...)
	if err != nil {
		return err
	}

	// before starting servers, register /ready handler. It should reflect entire Loki.
	t.server.HTTP.Path("/ready").Handler(t.readyHandler(sm))

	// Let's listen for events from this manager, and log them.
	healthy := func() { level.Info(util.Logger).Log("msg", "Loki started") }
	stopped := func() { level.Info(util.Logger).Log("msg", "Loki stopped") }
	serviceFailed := func(service services.Service) {
		// if any service fails, stop entire Loki
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
		if t.ingester != nil {
			if err := t.ingester.CheckReady(r.Context()); err != nil {
				http.Error(w, "Ingester not ready: "+err.Error(), http.StatusServiceUnavailable)
				return
			}
		}

		http.Error(w, "ready", http.StatusOK)
	}
}
