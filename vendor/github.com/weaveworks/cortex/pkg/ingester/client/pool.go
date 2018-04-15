package client

import (
	fmt "fmt"
	io "io"
	"sync"
	"time"

	"github.com/go-kit/kit/log/level"
	"github.com/weaveworks/common/user"
	"github.com/weaveworks/cortex/pkg/util"
	context "golang.org/x/net/context"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// Factory defines the signature for an ingester client factory
type Factory func(addr string) (grpc_health_v1.HealthClient, error)

// IngesterPool holds a cache of ingester clients
type IngesterPool struct {
	sync.RWMutex
	clients map[string]grpc_health_v1.HealthClient

	ingesterClientFactory Factory
	ingesterClientConfig  Config
	healthCheckTimeout    time.Duration
}

// NewIngesterPool creates a new cache
func NewIngesterPool(factory Factory, healthCheckTimeout time.Duration) *IngesterPool {
	return &IngesterPool{
		clients:               map[string]grpc_health_v1.HealthClient{},
		ingesterClientFactory: factory,
		healthCheckTimeout:    healthCheckTimeout,
	}
}

func (pool *IngesterPool) fromCache(addr string) (grpc_health_v1.HealthClient, bool) {
	pool.RLock()
	defer pool.RUnlock()
	client, ok := pool.clients[addr]
	return client, ok
}

// GetClientFor gets the client for the specified address. If it does not exist it will make a new client
// at that address
func (pool *IngesterPool) GetClientFor(addr string) (grpc_health_v1.HealthClient, error) {
	client, ok := pool.fromCache(addr)
	if ok {
		return client, nil
	}

	pool.Lock()
	defer pool.Unlock()
	client, ok = pool.clients[addr]
	if ok {
		return client, nil
	}

	client, err := pool.ingesterClientFactory(addr)
	if err != nil {
		return nil, err
	}
	pool.clients[addr] = client
	return client, nil
}

// RemoveClientFor removes the client with the specified address
func (pool *IngesterPool) RemoveClientFor(addr string) {
	pool.Lock()
	defer pool.Unlock()
	client, ok := pool.clients[addr]
	if ok {
		delete(pool.clients, addr)
		// Close in the background since this operation may take awhile and we have a mutex
		go func(addr string, closer io.Closer) {
			if err := closer.Close(); err != nil {
				level.Error(util.Logger).Log("msg", "error closing connection to ingester", "ingester", addr, "err", err)
			}
		}(addr, client.(io.Closer))
	}
}

// RegisteredAddresses returns all the addresses that a client is cached for
func (pool *IngesterPool) RegisteredAddresses() []string {
	result := []string{}
	pool.RLock()
	defer pool.RUnlock()
	for addr := range pool.clients {
		result = append(result, addr)
	}
	return result
}

// Count returns how many clients are in the cache
func (pool *IngesterPool) Count() int {
	pool.RLock()
	defer pool.RUnlock()
	return len(pool.clients)
}

// CleanUnhealthy loops through all ingesters and deletes any that fails a healtcheck.
func (pool *IngesterPool) CleanUnhealthy() {
	for _, addr := range pool.RegisteredAddresses() {
		client, ok := pool.fromCache(addr)
		// not ok means someone removed a client between the start of this loop and now
		if ok {
			err := healthCheck(client, pool.healthCheckTimeout)
			if err != nil {
				level.Warn(util.Logger).Log("msg", "removing ingester failing healtcheck", "addr", addr, "reason", err)
				pool.RemoveClientFor(addr)
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
