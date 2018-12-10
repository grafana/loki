package loki

import (
	"flag"
	"fmt"

	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"google.golang.org/grpc"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/storage"
	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/validation"
	"github.com/weaveworks/common/middleware"
	"github.com/weaveworks/common/server"

	"github.com/grafana/loki/pkg/distributor"
	"github.com/grafana/loki/pkg/ingester"
	"github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/querier"
)

// Config is the root config for Loki.
type Config struct {
	Target      moduleName `yaml:"target,omitempty"`
	AuthEnabled bool       `yaml:"auth_enabled,omitempty"`

	Server           server.Config      `yaml:"server,omitempty"`
	Distributor      distributor.Config `yaml:"distributor,omitempty"`
	Querier          querier.Config     `yaml:"querier,omitempty"`
	IngesterClient   client.Config      `yaml:"ingester_client,omitempty"`
	Ingester         ingester.Config    `yaml:"ingester,omitempty"`
	StorageConfig    storage.Config     `yaml:"storage_config,omitempty"`
	ChunkStoreConfig chunk.StoreConfig  `yaml:"chunk_store_config,omitempty"`
	SchemaConfig     chunk.SchemaConfig `yaml:"schema_config,omitempty"`
	LimitsConfig     validation.Limits  `yaml:"limits_config,omitempty"`
}

// RegisterFlags registers flag.
func (c *Config) RegisterFlags(f *flag.FlagSet) {
	c.Server.MetricsNamespace = "loki"
	c.Target = All
	f.Var(&c.Target, "target", "target module (default All)")
	f.BoolVar(&c.AuthEnabled, "auth.enabled", true, "Set to false to disable auth.")

	c.Server.RegisterFlags(f)
	c.Distributor.RegisterFlags(f)
	c.Querier.RegisterFlags(f)
	c.IngesterClient.RegisterFlags(f)
	c.Ingester.RegisterFlags(f)
	c.ChunkStoreConfig.RegisterFlags(f)
	c.SchemaConfig.RegisterFlags(f)
	c.LimitsConfig.RegisterFlags(f)
}

// Loki is the root datastructure for Loki.
type Loki struct {
	cfg Config

	server      *server.Server
	ring        *ring.Ring
	distributor *distributor.Distributor
	ingester    *ingester.Ingester
	querier     *querier.Querier
	store       chunk.Store

	httpAuthMiddleware middleware.Interface

	inited map[moduleName]struct{}
}

// New makes a new Loki.
func New(cfg Config) (*Loki, error) {
	loki := &Loki{
		cfg:    cfg,
		inited: map[moduleName]struct{}{},
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
	if _, ok := t.inited[m]; ok {
		return nil
	}

	for _, dep := range modules[m].deps {
		if err := t.init(dep); err != nil {
			return err
		}
	}

	level.Info(util.Logger).Log("msg", "initialising", "module", m)
	if modules[m].init != nil {
		if err := modules[m].init(t); err != nil {
			return errors.Wrap(err, fmt.Sprintf("error initialising module: %s", m))
		}
	}

	t.inited[m] = struct{}{}
	return nil
}

// Run starts Loki running, and blocks until a signal is received.
func (t *Loki) Run() error {
	return t.server.Run()
}

// Stop gracefully stops a Loki.
func (t *Loki) Stop() error {
	t.server.Shutdown()
	t.stop(t.cfg.Target)
	return nil
}

func (t *Loki) stop(m moduleName) {
	if _, ok := t.inited[m]; !ok {
		return
	}
	delete(t.inited, m)

	for _, dep := range modules[m].deps {
		t.stop(dep)
	}

	if modules[m].stop == nil {
		return
	}

	level.Info(util.Logger).Log("msg", "stopping", "module", m)
	if err := modules[m].stop(t); err != nil {
		level.Error(util.Logger).Log("msg", "error stopping", "module", m, "err", err)
	}
}
