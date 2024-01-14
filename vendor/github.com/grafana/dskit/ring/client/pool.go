package client

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/dskit/concurrency"
	"github.com/grafana/dskit/internal/slices"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
)

// PoolClient is the interface that should be implemented by a
// client managed by the pool.
type PoolClient interface {
	grpc_health_v1.HealthClient
	io.Closer
}

// PoolFactory is the interface for creating new clients based on
// the description of an instance in the ring.
type PoolFactory interface {
	FromInstance(inst ring.InstanceDesc) (PoolClient, error)
}

// PoolInstFunc is an implementation of PoolFactory for functions that
// accept ring instance metadata.
type PoolInstFunc func(inst ring.InstanceDesc) (PoolClient, error)

func (f PoolInstFunc) FromInstance(inst ring.InstanceDesc) (PoolClient, error) {
	return f(inst)
}

// PoolAddrFunc is an implementation of PoolFactory for functions that
// accept an instance address.
type PoolAddrFunc func(addr string) (PoolClient, error)

func (f PoolAddrFunc) FromInstance(inst ring.InstanceDesc) (PoolClient, error) {
	return f(inst.Addr)
}

// PoolServiceDiscovery defines the signature of a function returning the list
// of known service endpoints. This function is used to remove stale clients from
// the pool (a stale client is a client connected to a service endpoint no more
// active).
type PoolServiceDiscovery func() ([]string, error)

// PoolConfig is config for creating a Pool.
type PoolConfig struct {
	CheckInterval             time.Duration
	HealthCheckEnabled        bool
	HealthCheckTimeout        time.Duration
	MaxConcurrentHealthChecks int // defaults to 16
}

// Pool holds a cache of grpc_health_v1 clients.
type Pool struct {
	services.Service

	cfg        PoolConfig
	discovery  PoolServiceDiscovery
	factory    PoolFactory
	logger     log.Logger
	clientName string

	sync.RWMutex
	clients map[string]PoolClient

	clientsMetric prometheus.Gauge
}

// NewPool creates a new Pool.
func NewPool(clientName string, cfg PoolConfig, discovery PoolServiceDiscovery, factory PoolFactory, clientsMetric prometheus.Gauge, logger log.Logger) *Pool {
	if cfg.MaxConcurrentHealthChecks == 0 {
		cfg.MaxConcurrentHealthChecks = 16
	}

	p := &Pool{
		cfg:           cfg,
		discovery:     discovery,
		factory:       factory,
		logger:        logger,
		clientName:    clientName,
		clients:       map[string]PoolClient{},
		clientsMetric: clientsMetric,
	}

	p.Service = services.
		NewTimerService(cfg.CheckInterval, nil, p.iteration, nil).
		WithName(fmt.Sprintf("%s client pool", p.clientName))
	return p
}

func (p *Pool) iteration(_ context.Context) error {
	p.removeStaleClients()
	if p.cfg.HealthCheckEnabled {
		p.cleanUnhealthy()
	}
	return nil
}

func (p *Pool) fromCache(addr string) (PoolClient, bool) {
	p.RLock()
	defer p.RUnlock()
	client, ok := p.clients[addr]
	return client, ok
}

// GetClientFor gets the client for the specified address. If it does not exist
// it will make a new client for that address.
func (p *Pool) GetClientFor(addr string) (PoolClient, error) {
	return p.GetClientForInstance(ring.InstanceDesc{Addr: addr})
}

// GetClientForInstance gets the client for the specified ring member. If it does not exist
// it will make a new client for that instance.
func (p *Pool) GetClientForInstance(inst ring.InstanceDesc) (PoolClient, error) {
	client, ok := p.fromCache(inst.Addr)
	if ok {
		return client, nil
	}

	// No client in cache so create one
	p.Lock()
	defer p.Unlock()

	// Check if a client has been created just after checking the cache and before acquiring the lock.
	client, ok = p.clients[inst.Addr]
	if ok {
		return client, nil
	}

	client, err := p.factory.FromInstance(inst)
	if err != nil {
		return nil, err
	}
	p.clients[inst.Addr] = client
	if p.clientsMetric != nil {
		p.clientsMetric.Add(1)
	}
	return client, nil
}

// RemoveClientFor removes the client with the specified address
func (p *Pool) RemoveClientFor(addr string) {
	p.Lock()
	defer p.Unlock()
	client, ok := p.clients[addr]
	if ok {
		delete(p.clients, addr)
		p.closeClient(addr, client)
	}
}

func (p *Pool) closeClient(addr string, client PoolClient) {
	if p.clientsMetric != nil {
		p.clientsMetric.Add(-1)
	}
	// Close in the background since this operation may take awhile and we have a mutex
	go func(addr string, closer PoolClient) {
		if err := closer.Close(); err != nil {
			level.Error(p.logger).Log("msg", fmt.Sprintf("error closing connection to %s", p.clientName), "addr", addr, "err", err)
		}
	}(addr, client)
}

// RemoveClient removes the client instance from the pool if it is still there and not cleaned up by health check.
// The value of client needs to be the same as returned by GetClientForInstance or GetClientFor.
// If addr is not empty and contains the same addr passed when obtaining the client, then the operation is sped up.
func (p *Pool) RemoveClient(client PoolClient, addr string) {
	p.Lock()
	defer p.Unlock()
	if addr != "" {
		if p.clients[addr] != client {
			return
		}
		delete(p.clients, addr)
		p.closeClient(addr, client)
		return
	}
	for addr, cachedClient := range p.clients {
		if cachedClient != client {
			continue
		}
		delete(p.clients, addr)
		p.closeClient(addr, client)
		return
	}
}

// RegisteredAddresses returns all the service addresses for which there's an active client.
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
	// Only if service discovery has been configured.
	if p.discovery == nil {
		return
	}

	serviceAddrs, err := p.discovery()
	if err != nil {
		level.Error(p.logger).Log("msg", "error removing stale clients", "err", err)
		return
	}

	for _, addr := range p.RegisteredAddresses() {
		if slices.Contains(serviceAddrs, addr) {
			continue
		}
		level.Info(p.logger).Log("msg", "removing stale client", "addr", addr)
		p.RemoveClientFor(addr)
	}
}

// cleanUnhealthy loops through all servers and deletes any that fail a healthcheck.
// The health checks are executed concurrently with p.cfg.MaxConcurrentHealthChecks.
func (p *Pool) cleanUnhealthy() {
	addresses := p.RegisteredAddresses()
	_ = concurrency.ForEachJob(context.Background(), len(addresses), p.cfg.MaxConcurrentHealthChecks, func(ctx context.Context, idx int) error {
		addr := addresses[idx]
		client, ok := p.fromCache(addr)
		// not ok means someone removed a client between the start of this loop and now
		if ok {
			err := healthCheck(ctx, client, p.cfg.HealthCheckTimeout)
			if err != nil {
				level.Warn(p.logger).Log("msg", fmt.Sprintf("removing %s failing healthcheck", p.clientName), "addr", addr, "reason", err)
				p.RemoveClientFor(addr)
			}
		}
		// Never return an error, because otherwise the processing would stop and
		// remaining health checks would not been executed.
		return nil
	})
}

// healthCheck will check if the client is still healthy, returning an error if it is not
func healthCheck(ctx context.Context, client PoolClient, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ctx = user.InjectOrgID(ctx, "0")

	resp, err := client.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		return err
	}
	if resp.Status != grpc_health_v1.HealthCheckResponse_SERVING {
		return fmt.Errorf("failing healthcheck status: %s", resp.Status)
	}
	return nil
}
