package client

import (
	"context"
	"flag"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/weaveworks/common/user"
)

var clients = promauto.NewGauge(prometheus.GaugeOpts{
	Namespace: "cortex",
	Name:      "distributor_ingester_clients",
	Help:      "The current number of ingester clients.",
})

// Factory defines the signature for an ingester client factory.
type Factory func(addr string) (grpc_health_v1.HealthClient, error)

// PoolConfig is config for creating a Pool.
type PoolConfig struct {
	ClientCleanupPeriod  time.Duration `yaml:"client_cleanup_period,omitempty"`
	HealthCheckIngesters bool          `yaml:"health_check_ingesters,omitempty"`
	RemoteTimeout        time.Duration
}

// RegisterFlags adds the flags required to config this to the given FlagSet.
func (cfg *PoolConfig) RegisterFlags(f *flag.FlagSet) {
	f.DurationVar(&cfg.ClientCleanupPeriod, "distributor.client-cleanup-period", 15*time.Second, "How frequently to clean up clients for ingesters that have gone away.")
	f.BoolVar(&cfg.HealthCheckIngesters, "distributor.health-check-ingesters", false, "Run a health check on each ingester client during periodic cleanup.")
}

// Pool holds a cache of grpc_health_v1 clients.
type Pool struct {
	cfg     PoolConfig
	ring    ring.ReadRing
	factory Factory
	logger  log.Logger

	quit chan struct{}
	done sync.WaitGroup

	sync.RWMutex
	clients map[string]grpc_health_v1.HealthClient
}

// NewPool creates a new Pool.
func NewPool(cfg PoolConfig, ring ring.ReadRing, factory Factory, logger log.Logger) *Pool {
	p := &Pool{
		cfg:     cfg,
		ring:    ring,
		factory: factory,
		logger:  logger,
		quit:    make(chan struct{}),

		clients: map[string]grpc_health_v1.HealthClient{},
	}

	p.done.Add(1)
	go p.loop()
	return p
}

func (p *Pool) loop() {
	defer p.done.Done()

	cleanupClients := time.NewTicker(p.cfg.ClientCleanupPeriod)
	defer cleanupClients.Stop()

	for {
		select {
		case <-cleanupClients.C:
			p.removeStaleClients()
			if p.cfg.HealthCheckIngesters {
				p.cleanUnhealthy()
			}
		case <-p.quit:
			return
		}
	}
}

// Stop the pool's background cleanup goroutine.
func (p *Pool) Stop() {
	close(p.quit)
	p.done.Wait()
}

func (p *Pool) fromCache(addr string) (grpc_health_v1.HealthClient, bool) {
	p.RLock()
	defer p.RUnlock()
	client, ok := p.clients[addr]
	return client, ok
}

// GetClientFor gets the client for the specified address. If it does not exist it will make a new client
// at that address
func (p *Pool) GetClientFor(addr string) (grpc_health_v1.HealthClient, error) {
	client, ok := p.fromCache(addr)
	if ok {
		return client, nil
	}

	p.Lock()
	defer p.Unlock()
	client, ok = p.clients[addr]
	if ok {
		return client, nil
	}

	client, err := p.factory(addr)
	if err != nil {
		return nil, err
	}
	p.clients[addr] = client
	clients.Add(1)
	return client, nil
}

// RemoveClientFor removes the client with the specified address
func (p *Pool) RemoveClientFor(addr string) {
	p.Lock()
	defer p.Unlock()
	client, ok := p.clients[addr]
	if ok {
		delete(p.clients, addr)
		clients.Add(-1)
		// Close in the background since this operation may take awhile and we have a mutex
		go func(addr string, closer io.Closer) {
			if err := closer.Close(); err != nil {
				level.Error(p.logger).Log("msg", "error closing connection to ingester", "ingester", addr, "err", err)
			}
		}(addr, client.(io.Closer))
	}
}

// RegisteredAddresses returns all the addresses that a client is cached for
func (p *Pool) RegisteredAddresses() []string {
	result := []string{}
	p.RLock()
	defer p.RUnlock()
	for addr := range p.clients {
		result = append(result, addr)
	}
	return result
}

// Count returns how many clients are in the cache
func (p *Pool) Count() int {
	p.RLock()
	defer p.RUnlock()
	return len(p.clients)
}

func (p *Pool) removeStaleClients() {
	clients := map[string]struct{}{}
	replicationSet, err := p.ring.GetAll()
	if err != nil {
		level.Error(util.Logger).Log("msg", "error removing stale clients", "err", err)
		return
	}

	for _, ing := range replicationSet.Ingesters {
		clients[ing.Addr] = struct{}{}
	}

	for _, addr := range p.RegisteredAddresses() {
		if _, ok := clients[addr]; ok {
			continue
		}
		level.Info(util.Logger).Log("msg", "removing stale client", "addr", addr)
		p.RemoveClientFor(addr)
	}
}

// cleanUnhealthy loops through all ingesters and deletes any that fails a healthcheck.
func (p *Pool) cleanUnhealthy() {
	for _, addr := range p.RegisteredAddresses() {
		client, ok := p.fromCache(addr)
		// not ok means someone removed a client between the start of this loop and now
		if ok {
			err := healthCheck(client, p.cfg.RemoteTimeout)
			if err != nil {
				level.Warn(util.Logger).Log("msg", "removing ingester failing healthcheck", "addr", addr, "reason", err)
				p.RemoveClientFor(addr)
			}
		}
	}
}

// healthCheck will check if the client is still healthy, returning an error if it is not
func healthCheck(client grpc_health_v1.HealthClient, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	ctx = user.InjectOrgID(ctx, "0")

	resp, err := client.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		return err
	}
	if resp.Status != grpc_health_v1.HealthCheckResponse_SERVING {
		return fmt.Errorf("Failing healthcheck status: %s", resp.Status)
	}
	return nil
}
