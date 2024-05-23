package pattern

import (
	"context"
	"fmt"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/ring"
	ring_client "github.com/grafana/dskit/ring/client"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/pattern/clientpool"
)

type RingClient interface {
	services.Service
	Ring() ring.ReadRing
	Pool() *ring_client.Pool
}

type ringClient struct {
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
) (RingClient, error) {
	var err error
	registerer = prometheus.WrapRegistererWithPrefix(metricsNamespace+"_", registerer)
	ringClient := &ringClient{
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

func (r *ringClient) starting(ctx context.Context) error {
	return services.StartManagerAndAwaitHealthy(ctx, r.subservices)
}

func (r *ringClient) running(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return nil
	case err := <-r.subservicesWatcher.Chan():
		return fmt.Errorf("pattern tee subservices failed: %w", err)
	}
}

func (r *ringClient) stopping(_ error) error {
	return services.StopManagerAndAwaitStopped(context.Background(), r.subservices)
}

func (r *ringClient) Ring() ring.ReadRing {
	return r.ring
}

func (r *ringClient) Pool() *ring_client.Pool {
	return r.pool
}

// StartAsync starts Service asynchronously. Service must be in New State, otherwise error is returned.
// Context is used as a parent context for service own context.
func (r *ringClient) StartAsync(ctx context.Context) error {
	return r.StartAsync(ctx)
}

// AwaitRunning waits until service gets into Running state.
// If service is in New or Starting state, this method is blocking.
// If service is already in Running state, returns immediately with no error.
// If service is in a state, from which it cannot get into Running state, error is returned immediately.
func (r *ringClient) AwaitRunning(ctx context.Context) error {
	return r.AwaitRunning(ctx)
}

// StopAsync tell the service to stop. This method doesn't block and can be called multiple times.
// If Service is New, it is Terminated without having been started nor stopped.
// If Service is in Starting or Running state, this initiates shutdown and returns immediately.
// If Service has already been stopped, this method returns immediately, without taking action.
func (r *ringClient) StopAsync() {
	r.StopAsync()
}

// AwaitTerminated waits for the service to reach Terminated or Failed state. If service is already in one of these states,
// when method is called, method returns immediately.
// If service enters Terminated state, this method returns nil.
// If service enters Failed state, or context is finished before reaching Terminated or Failed, error is returned.
func (r *ringClient) AwaitTerminated(ctx context.Context) error {
	return r.AwaitTerminated(ctx)
}

// FailureCase returns error if Service is in Failed state.
// If Service is not in Failed state, this method returns nil.
func (r *ringClient) FailureCase() error {
	return r.FailureCase()
}

// State returns current state of the service.
func (r *ringClient) State() services.State {
	return r.State()
}

// AddListener adds listener to this service. Listener will be notified on subsequent state transitions
// of the service. Previous state transitions are not replayed, so it is suggested to add listeners before
// service is started.
//
// AddListener guarantees execution ordering across calls to a given listener but not across calls to
// multiple listeners. Specifically, a given listener will have its callbacks invoked in the same order
// as the service enters those states. Additionally, at most one of the listener's callbacks will execute
// at once. However, multiple listeners' callbacks may execute concurrently, and listeners may execute
// in an order different from the one in which they were registered.
func (r *ringClient) AddListener(listener services.Listener) {
	r.AddListener(listener)
}
