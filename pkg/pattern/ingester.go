package pattern

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"sync"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc/health/grpc_health_v1"

	ring_client "github.com/grafana/dskit/ring/client"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/pattern/clientpool"
	util_log "github.com/grafana/loki/pkg/util/log"
)

type Config struct {
	Enabled          bool                  `yaml:"enabled,omitempty" doc:"description=Whether the pattern ingester is enabled."`
	LifecyclerConfig ring.LifecyclerConfig `yaml:"lifecycler,omitempty" doc:"description=Configures how the lifecycle of the pattern ingester will operate and where it will register for discovery."`
	ClientConfig     clientpool.Config     `yaml:"client_config,omitempty" doc:"description=Configures how the pattern ingester will connect to the ingesters."`

	// For testing.
	factory ring_client.PoolFactory `yaml:"-"`
}

// RegisterFlags registers pattern ingester related flags.
func (cfg *Config) RegisterFlags(fs *flag.FlagSet) {
	cfg.LifecyclerConfig.RegisterFlagsWithPrefix("pattern-ingester.", fs, util_log.Logger)
	cfg.ClientConfig.RegisterFlags(fs)
	fs.BoolVar(&cfg.Enabled, "pattern-ingester.enabled", false, "Flag to enable or disable the usage of the pattern-ingester component.")
}

func (cfg *Config) Validate() error {
	if cfg.LifecyclerConfig.RingConfig.ReplicationFactor != 1 {
		return errors.New("pattern ingester replication factor must be 1")
	}
	return cfg.LifecyclerConfig.Validate()
}

type Ingester struct {
	services.Service
	lifecycler *ring.Lifecycler

	lifecyclerWatcher *services.FailureWatcher

	cfg        Config
	registerer prometheus.Registerer
	logger     log.Logger

	instancesMtx sync.RWMutex
	instances    map[string]*instance
}

func New(
	cfg Config,
	metricsNamespace string,
	registerer prometheus.Registerer,
	logger log.Logger,
) (*Ingester, error) {
	registerer = prometheus.WrapRegistererWithPrefix(metricsNamespace+"_", registerer)

	i := &Ingester{
		cfg:        cfg,
		logger:     log.With(logger, "component", "pattern-ingester"),
		registerer: registerer,
		instances:  make(map[string]*instance),
	}
	i.Service = services.NewBasicService(i.starting, i.running, i.stopping)
	var err error
	i.lifecycler, err = ring.NewLifecycler(cfg.LifecyclerConfig, i, "pattern-ingester", "pattern-ring", true, i.logger, registerer)
	if err != nil {
		return nil, err
	}

	i.lifecyclerWatcher = services.NewFailureWatcher()
	i.lifecyclerWatcher.WatchService(i.lifecycler)

	return i, nil
}

// ServeHTTP implements the pattern ring status page.
func (i *Ingester) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	i.lifecycler.ServeHTTP(w, r)
}

func (i *Ingester) starting(ctx context.Context) error {
	// pass new context to lifecycler, so that it doesn't stop automatically when Ingester's service context is done
	err := i.lifecycler.StartAsync(context.Background())
	if err != nil {
		return err
	}

	err = i.lifecycler.AwaitRunning(ctx)
	if err != nil {
		return err
	}
	// todo: start flush queue.
	return nil
}

func (i *Ingester) running(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

func (i *Ingester) stopping(_ error) error {
	// todo: stop flush queue
	return nil
}

func (i *Ingester) TransferOut(_ context.Context) error {
	// todo may be.
	return ring.ErrTransferDisabled
}

// Watch implements grpc_health_v1.HealthCheck.
func (*Ingester) Watch(*grpc_health_v1.HealthCheckRequest, grpc_health_v1.Health_WatchServer) error {
	return nil
}

// ReadinessHandler is used to indicate to k8s when the ingesters are ready for
// the addition removal of another ingester. Returns 204 when the ingester is
// ready, 500 otherwise.
func (i *Ingester) CheckReady(ctx context.Context) error {
	if s := i.State(); s != services.Running && s != services.Stopping {
		return fmt.Errorf("ingester not ready: %v", s)
	}
	return i.lifecycler.CheckReady(ctx)
}

func (i *Ingester) Flush() {
	// todo flush or use transfer out
}

func (i *Ingester) Push(ctx context.Context, req *logproto.PushRequest) (*logproto.PushResponse, error) {
	instanceID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}
	instance, err := i.GetOrCreateInstance(instanceID)
	if err != nil {
		return &logproto.PushResponse{}, err
	}
	return &logproto.PushResponse{}, instance.Push(ctx, req)
}

func (i *Ingester) Query(req *logproto.QueryPatternsRequest, stream logproto.Pattern_QueryServer) error {
	ctx := stream.Context()
	instanceID, err := tenant.TenantID(ctx)
	if err != nil {
		return err
	}
	instance, err := i.GetOrCreateInstance(instanceID)
	if err != nil {
		return err
	}
	iterator, err := instance.Iterator(ctx, req)
	if err != nil {
		return err
	}
	defer iterator.Close()
	// todo batch send patterns.
	return nil
}

func (i *Ingester) GetOrCreateInstance(instanceID string) (*instance, error) { //nolint:revive
	inst, ok := i.getInstanceByID(instanceID)
	if ok {
		return inst, nil
	}

	i.instancesMtx.Lock()
	defer i.instancesMtx.Unlock()
	inst, ok = i.instances[instanceID]
	if !ok {
		var err error
		inst, err = newInstance(instanceID, i.logger)
		if err != nil {
			return nil, err
		}
		i.instances[instanceID] = inst
	}
	return inst, nil
}

func (i *Ingester) getInstanceByID(id string) (*instance, bool) {
	i.instancesMtx.RLock()
	defer i.instancesMtx.RUnlock()

	inst, ok := i.instances[id]
	return inst, ok
}
