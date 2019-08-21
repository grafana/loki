package loki

import (
	"flag"
	"fmt"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/util"

	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/weaveworks/common/middleware"
	"github.com/weaveworks/common/server"
	"google.golang.org/grpc"

	"github.com/grafana/loki/pkg/distributor"
	"github.com/grafana/loki/pkg/ingester"
	"github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/querier"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/util/validation"
)

// Config is the root config for Loki.
type Config struct {
	Target      moduleName `yaml:"target,omitempty"`
	AuthEnabled bool       `yaml:"auth_enabled,omitempty"`

	Server           server.Config            `yaml:"server,omitempty"`
	Distributor      distributor.Config       `yaml:"distributor,omitempty"`
	Querier          querier.Config           `yaml:"querier,omitempty"`
	IngesterClient   client.Config            `yaml:"ingester_client,omitempty"`
	Ingester         ingester.Config          `yaml:"ingester,omitempty"`
	StorageConfig    storage.Config           `yaml:"storage_config,omitempty"`
	ChunkStoreConfig chunk.StoreConfig        `yaml:"chunk_store_config,omitempty"`
	SchemaConfig     chunk.SchemaConfig       `yaml:"schema_config,omitempty"`
	LimitsConfig     validation.Limits        `yaml:"limits_config,omitempty"`
	TableManager     chunk.TableManagerConfig `yaml:"table_manager,omitempty"`
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
}

// Loki is the root datastructure for Loki.
type Loki struct {
	cfg Config

	server       *server.Server
	ring         *ring.Ring
	overrides    *validation.Overrides
	distributor  *distributor.Distributor
	ingester     *ingester.Ingester
	querier      *querier.Querier
	store        storage.Store
	tableManager *chunk.TableManager

	httpAuthMiddleware middleware.Interface
}

// New makes a new Loki.
func New(cfg Config) (*Loki, error) {
	loki := &Loki{
		cfg: cfg,
	}

	loki.setupAuthMiddleware()

	if err := loki.init(cfg.Target); err != nil {
		return nil, err
	}

	return loki, nil
}

func (t *Loki) setupAuthMiddleware() {
	if t.cfg.AuthEnabled {
		t.cfg.Server.GRPCMiddleware = []grpc.UnaryServerInterceptor{
			middleware.ServerUserHeaderInterceptor,
		}
		t.cfg.Server.GRPCStreamMiddleware = []grpc.StreamServerInterceptor{
			middleware.StreamServerUserHeaderInterceptor,
		}
		t.httpAuthMiddleware = middleware.AuthenticateUser
	} else {
		t.cfg.Server.GRPCMiddleware = []grpc.UnaryServerInterceptor{
			fakeGRPCAuthUniaryMiddleware,
		}
		t.cfg.Server.GRPCStreamMiddleware = []grpc.StreamServerInterceptor{
			fakeGRPCAuthStreamMiddleware,
		}
		t.httpAuthMiddleware = fakeHTTPAuthMiddleware
	}
}

func (t *Loki) init(m moduleName) error {
	// initialize all of our dependencies first
	for _, dep := range orderedDeps(m) {
		if err := t.initModule(dep); err != nil {
			return err
		}
	}
	// lastly, initialize the requested module
	return t.initModule(m)
}

func (t *Loki) initModule(m moduleName) error {
	level.Info(util.Logger).Log("msg", "initialising", "module", m)
	if modules[m].init != nil {
		if err := modules[m].init(t); err != nil {
			return errors.Wrap(err, fmt.Sprintf("error initialising module: %s", m))
		}
	}
	return nil
}

// Run starts Loki running, and blocks until a signal is received.
func (t *Loki) Run() error {
	return t.server.Run()
}

// Stop gracefully stops a Loki.
func (t *Loki) Stop() error {
	t.stopping(t.cfg.Target)
	t.stop(t.cfg.Target)
	t.server.Shutdown()
	return nil
}

func (t *Loki) stop(m moduleName) {
	t.stopModule(m)
	deps := orderedDeps(m)
	// iterate over our deps in reverse order and call stopModule
	for i := len(deps) - 1; i >= 0; i-- {
		t.stopModule(deps[i])
	}
}

func (t *Loki) stopModule(m moduleName) {
	level.Info(util.Logger).Log("msg", "stopping", "module", m)
	if modules[m].stop != nil {
		if err := modules[m].stop(t); err != nil {
			level.Error(util.Logger).Log("msg", "error stopping", "module", m, "err", err)
		}
	}
}

func (t *Loki) stopping(m moduleName) {
	t.stoppingModule(m)
	deps := orderedDeps(m)
	// iterate over our deps in reverse order and call stoppingModule
	for i := len(deps) - 1; i >= 0; i-- {
		t.stoppingModule(deps[i])
	}
}

func (t *Loki) stoppingModule(m moduleName) {
	level.Info(util.Logger).Log("msg", "notifying module about stopping", "module", m)
	if modules[m].stopping != nil {
		if err := modules[m].stopping(t); err != nil {
			level.Error(util.Logger).Log("msg", "error stopping", "module", m, "err", err)
		}
	}
}
