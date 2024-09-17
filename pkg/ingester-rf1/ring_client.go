package ingesterrf1

import (
	"context"
	"fmt"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/ring"
	ring_client "github.com/grafana/dskit/ring/client"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/ingester-rf1/clientpool"
)

type RingClient struct {
	cfg    Config
	logger log.Logger

	services.Service
	subservices        *services.Manager
	subservicesWatcher *services.FailureWatcher
	ring               *ring.Ring
	pool               *ring_client.Pool
}

func NewRingClient(
	cfg Config,
	metricsNamespace string,
	registerer prometheus.Registerer,
	logger log.Logger,
) (*RingClient, error) {
	var err error
	registerer = prometheus.WrapRegistererWithPrefix(metricsNamespace+"_", registerer)
	ringClient := &RingClient{
		logger: log.With(logger, "component", "ingester-rf1-client"),
		cfg:    cfg,
	}
	ringClient.ring, err = newRing(cfg.LifecyclerConfig.RingConfig, "ingester-rf1", "ingester-rf1-ring", ringClient.logger, registerer)
	if err != nil {
		return nil, err
	}
	factory := cfg.factory
	if factory == nil {
		factory = ring_client.PoolAddrFunc(func(addr string) (ring_client.PoolClient, error) {
			return clientpool.NewClient(cfg.ClientConfig, addr)
		})
	}
	ringClient.pool = clientpool.NewPool("ingester-rf1", cfg.ClientConfig.PoolConfig, ringClient.ring, factory, logger, metricsNamespace)

	ringClient.subservices, err = services.NewManager(ringClient.pool, ringClient.ring)
	if err != nil {
		return nil, fmt.Errorf("services manager: %w", err)
	}
	ringClient.subservicesWatcher = services.NewFailureWatcher()
	ringClient.subservicesWatcher.WatchManager(ringClient.subservices)
	ringClient.Service = services.NewBasicService(ringClient.starting, ringClient.running, ringClient.stopping)

	return ringClient, nil
}

func newRing(cfg ring.Config, name, key string, logger log.Logger, reg prometheus.Registerer) (*ring.Ring, error) {
	codec := ring.GetCodec()
	// Suffix all client names with "-ring" to denote this kv client is used by the ring
	store, err := kv.NewClient(
		cfg.KVStore,
		codec,
		kv.RegistererWithKVName(reg, name+"-ring"),
		logger,
	)
	if err != nil {
		return nil, err
	}

	return ring.NewWithStoreClientAndStrategy(cfg, name, key, store, ring.NewIgnoreUnhealthyInstancesReplicationStrategy(), reg, logger)
}

func (q *RingClient) starting(ctx context.Context) error {
	return services.StartManagerAndAwaitHealthy(ctx, q.subservices)
}

func (q *RingClient) running(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return nil
	case err := <-q.subservicesWatcher.Chan():
		return fmt.Errorf("ingester-rf1 tee subservices failed: %w", err)
	}
}

func (q *RingClient) stopping(_ error) error {
	return services.StopManagerAndAwaitStopped(context.Background(), q.subservices)
}
