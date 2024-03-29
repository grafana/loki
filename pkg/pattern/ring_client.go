package pattern

import (
	"context"
	"fmt"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/ring"
	ring_client "github.com/grafana/dskit/ring/client"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/pattern/clientpool"
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
		logger: log.With(logger, "component", "pattern-ring-client"),
		cfg:    cfg,
	}
	ringClient.ring, err = ring.New(cfg.LifecyclerConfig.RingConfig, "pattern-ingester", "pattern-ring", ringClient.logger, registerer)
	if err != nil {
		return nil, err
	}
	factory := cfg.factory
	if factory == nil {
		factory = ring_client.PoolAddrFunc(func(addr string) (ring_client.PoolClient, error) {
			return clientpool.NewClient(cfg.ClientConfig, addr)
		})
	}
	ringClient.pool = clientpool.NewPool("pattern-ingester", cfg.ClientConfig.PoolConfig, ringClient.ring, factory, logger, metricsNamespace)

	ringClient.subservices, err = services.NewManager(ringClient.pool, ringClient.ring)
	if err != nil {
		return nil, fmt.Errorf("services manager: %w", err)
	}
	ringClient.subservicesWatcher = services.NewFailureWatcher()
	ringClient.subservicesWatcher.WatchManager(ringClient.subservices)
	ringClient.Service = services.NewBasicService(ringClient.starting, ringClient.running, ringClient.stopping)

	return ringClient, nil
}

func (q *RingClient) starting(ctx context.Context) error {
	return services.StartManagerAndAwaitHealthy(ctx, q.subservices)
}

func (q *RingClient) running(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return nil
	case err := <-q.subservicesWatcher.Chan():
		return fmt.Errorf("pattern tee subservices failed: %w", err)
	}
}

func (q *RingClient) stopping(_ error) error {
	return services.StopManagerAndAwaitStopped(context.Background(), q.subservices)
}
