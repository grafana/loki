package client

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/weaveworks/common/user"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/cortexproject/cortex/pkg/util"
	util_log "github.com/cortexproject/cortex/pkg/util/log"
	"github.com/cortexproject/cortex/pkg/util/services"
)

// PoolClient is the interface that should be implemented by a
// client managed by the pool.
type PoolClient interface {
	grpc_health_v1.HealthClient
	io.Closer
}

// PoolFactory defines the signature for a client factory.
type PoolFactory func(addr string) (PoolClient, error)

// PoolServiceDiscovery defines the signature of a function returning the list
// of known service endpoints. This function is used to remove stale clients from
// the pool (a stale client is a client connected to a service endpoint no more
// active).
type PoolServiceDiscovery func() ([]string, error)

// PoolConfig is config for creating a Pool.
type PoolConfig struct {
	CheckInterval      time.Duration
	HealthCheckEnabled bool
	HealthCheckTimeout time.Duration
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

func (p *Pool) iteration(ctx context.Context) error {
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

// GetClientFor gets the client for the specified address. If it does not exist it will make a new client
// at that address
func (p *Pool) GetClientFor(addr string) (PoolClient, error) {
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
		level.Error(util_log.Logger).Log("msg", "error removing stale clients", "err", err)
		return
	}

	for _, addr := range p.RegisteredAddresses() {
		if util.StringsContain(serviceAddrs, addr) {
			continue
		}
		level.Info(util_log.Logger).Log("msg", "removing stale client", "addr", addr)
		p.RemoveClientFor(addr)
	}
}

// cleanUnhealthy loops through all servers and deletes any that fails a healthcheck.
func (p *Pool) cleanUnhealthy() {
	for _, addr := range p.RegisteredAddresses() {
		client, ok := p.fromCache(addr)
		// not ok means someone removed a client between the start of this loop and now
		if ok {
			err := healthCheck(client, p.cfg.HealthCheckTimeout)
			if err != nil {
				level.Warn(util_log.Logger).Log("msg", fmt.Sprintf("removing %s failing healthcheck", p.clientName), "addr", addr, "reason", err)
				p.RemoveClientFor(addr)
			}
		}
	}
}

// healthCheck will check if the client is still healthy, returning an error if it is not
func healthCheck(client PoolClient, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
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
