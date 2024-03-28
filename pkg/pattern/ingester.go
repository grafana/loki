package pattern

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"sync"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	ring_client "github.com/grafana/dskit/ring/client"
	"github.com/grafana/loki/pkg/distributor"
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
	lifecycler        *ring.Lifecycler
	ring              *ring.Ring
	pool              *ring_client.Pool
	lifecyclerWatcher *services.FailureWatcher

	cfg        Config
	registerer prometheus.Registerer
	logger     log.Logger

	ingesterAppends *prometheus.CounterVec
	instancesMtx    sync.RWMutex
	instances       map[string]*instance
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
		ingesterAppends: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Name: "pattern_ingester_appends_total",
			Help: "The total number of batch appends sent to pattern ingesters.",
		}, []string{"ingester", "status"}),
	}
	i.Service = services.NewBasicService(i.starting, i.running, i.stopping)
	var err error
	i.lifecycler, err = ring.NewLifecycler(cfg.LifecyclerConfig, i, "pattern-ingester", "pattern-ring", true, i.logger, registerer)
	if err != nil {
		return nil, err
	}
	i.ring, err = ring.New(cfg.LifecyclerConfig.RingConfig, "pattern-ingester", "pattern-ring", i.logger, registerer)
	if err != nil {
		return nil, err
	}
	factory := cfg.factory
	if factory == nil {
		factory = ring_client.PoolAddrFunc(func(addr string) (ring_client.PoolClient, error) {
			return clientpool.NewClient(cfg.ClientConfig, addr)
		})
	}
	i.pool = clientpool.NewPool("pattern-ingester", cfg.ClientConfig.PoolConfig, i.ring, cfg.factory, logger, metricsNamespace)

	i.lifecyclerWatcher = services.NewFailureWatcher()
	i.lifecyclerWatcher.WatchService(i.lifecycler)
	return i, nil
}

// ServeHTTP implements the pattern ring status page.
func (i *Ingester) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	i.lifecycler.ServeHTTP(w, r)
}

// Duplicate Implements distributor.Tee which is used to tee distributor requests to pattern ingesters.
func (i *Ingester) Duplicate(tenant string, streams []distributor.KeyedStream) {
	for idx := range streams {
		go func(stream distributor.KeyedStream) {
			if err := i.sendStream(stream); err != nil {
				level.Error(i.logger).Log("msg", "failed to send stream to pattern ingester", "err", err)
			}
		}(streams[idx])
	}
}

func (i *Ingester) sendStream(stream distributor.KeyedStream) error {
	var descs [1]ring.InstanceDesc
	replicationSet, err := i.ring.Get(stream.HashKey, ring.WriteNoExtend, descs[:0], nil, nil)
	if err != nil {
		return err
	}
	if replicationSet.Instances == nil {
		return errors.New("no instances found")
	}
	addr := replicationSet.Instances[0].Addr
	client, err := i.pool.GetClientFor(addr)
	if err != nil {
		return err
	}
	req := &logproto.PushRequest{
		Streams: []logproto.Stream{
			stream.Stream,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), i.cfg.ClientConfig.RemoteTimeout)
	defer cancel()
	_, err = client.(logproto.PatternClient).Push(ctx, req)
	if err != nil {
		i.ingesterAppends.WithLabelValues(addr, "fail").Inc()
		return err
	}
	i.ingesterAppends.WithLabelValues(addr, "success").Inc()
	return nil
}

func (i *Ingester) starting(ctx context.Context) error {
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
