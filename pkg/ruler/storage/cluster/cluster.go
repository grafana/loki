package cluster

import (
	"context"
	"fmt"
	"sync"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/gorilla/mux"
	"github.com/grafana/agent/pkg/agentproto"
	"github.com/grafana/agent/pkg/metrics/instance"
	"github.com/grafana/agent/pkg/metrics/instance/configstore"
	"github.com/grafana/agent/pkg/util"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
)

// Cluster connects an Agent to other Agents and allows them to distribute
// workload.
type Cluster struct {
	mut sync.RWMutex

	log            log.Logger
	cfg            Config
	baseValidation ValidationFunc

	//
	// Internally, Cluster glues together four separate pieces of logic.
	// See comments below to get an understanding of what is going on.
	//

	// node manages membership in the cluster and performs cluster-wide reshards.
	node *node

	// store connects to a configstore for changes. storeAPI is an HTTP API for it.
	store    *configstore.Remote
	storeAPI *configstore.API

	// watcher watches the store and applies changes to an instance.Manager,
	// triggering metrics to be collected and sent. configWatcher also does a
	// complete refresh of its state on an interval.
	watcher *configWatcher
}

// New creates a new Cluster.
func New(
	l log.Logger,
	reg prometheus.Registerer,
	cfg Config,
	im instance.Manager,
	validate ValidationFunc,
) (*Cluster, error) {
	l = log.With(l, "component", "cluster")

	var (
		c   = &Cluster{log: l, cfg: cfg, baseValidation: validate}
		err error
	)

	// Hold the lock for the initialization. This is necessary since newNode will
	// eventually call Reshard, and we want c.watcher to be initialized when that
	// happens.
	c.mut.Lock()
	defer c.mut.Unlock()

	c.node, err = newNode(reg, l, cfg, c)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize node membership: %w", err)
	}

	c.store, err = configstore.NewRemote(l, reg, cfg.KVStore, cfg.Enabled)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize configstore: %w", err)
	}
	c.storeAPI = configstore.NewAPI(l, c.store, c.storeValidate)
	reg.MustRegister(c.storeAPI)

	c.watcher, err = newConfigWatcher(l, cfg, c.store, im, c.node.Owns, validate)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize configwatcher: %w", err)
	}

	// NOTE(rfratto): ApplyConfig isn't necessary for the initialization but must
	// be called for any changes to the configuration.
	return c, nil
}

func (c *Cluster) storeValidate(cfg *instance.Config) error {
	c.mut.RLock()
	defer c.mut.RUnlock()

	if err := c.baseValidation(cfg); err != nil {
		return err
	}

	if c.cfg.DangerousAllowReadingFiles {
		return nil
	}

	// If configs aren't allowed to read from the store, we need to make sure no
	// configs coming in from the API set files for passwords.
	return validateNofiles(cfg)
}

// Reshard implements agentproto.ScrapingServiceServer, and syncs the state of
// configs with the configstore.
func (c *Cluster) Reshard(ctx context.Context, _ *agentproto.ReshardRequest) (*empty.Empty, error) {
	c.mut.RLock()
	defer c.mut.RUnlock()

	level.Info(c.log).Log("msg", "received reshard notification, requesting refresh")
	c.watcher.RequestRefresh()
	return &empty.Empty{}, nil
}

// ApplyConfig applies configuration changes to Cluster.
func (c *Cluster) ApplyConfig(
	cfg Config,
) error {
	c.mut.Lock()
	defer c.mut.Unlock()

	if util.CompareYAML(c.cfg, cfg) {
		return nil
	}

	if err := c.node.ApplyConfig(cfg); err != nil {
		return fmt.Errorf("failed to apply config to node membership: %w", err)
	}

	if err := c.store.ApplyConfig(cfg.Lifecycler.RingConfig.KVStore, cfg.Enabled); err != nil {
		return fmt.Errorf("failed to apply config to config store: %w", err)
	}

	if err := c.watcher.ApplyConfig(cfg); err != nil {
		return fmt.Errorf("failed to apply config to watcher: %w", err)
	}

	c.cfg = cfg

	// Force a refresh so all the configs get updated with new defaults.
	level.Info(c.log).Log("msg", "cluster config changed, queueing refresh")
	c.watcher.RequestRefresh()
	return nil
}

// WireAPI injects routes into the provided mux router for the config
// management API.
func (c *Cluster) WireAPI(r *mux.Router) {
	c.storeAPI.WireAPI(r)
	c.node.WireAPI(r)
}

// WireGRPC injects gRPC server handlers into the provided gRPC server.
func (c *Cluster) WireGRPC(srv *grpc.Server) {
	agentproto.RegisterScrapingServiceServer(srv, c)
}

// Stop stops the cluster and all of its dependencies.
func (c *Cluster) Stop() {
	c.mut.Lock()
	defer c.mut.Unlock()

	deps := []struct {
		name   string
		closer func() error
	}{
		{"node", c.node.Stop},
		{"config store", c.store.Close},
		{"config watcher", c.watcher.Stop},
	}
	for _, dep := range deps {
		err := dep.closer()
		if err != nil {
			level.Error(c.log).Log("msg", "failed to stop dependency", "dependency", dep.name, "err", err)
		}
	}
}
