package querier

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/thanos/pkg/block/metadata"
	"github.com/thanos-io/thanos/pkg/discovery/dns"
	"github.com/thanos-io/thanos/pkg/extprom"

	"github.com/cortexproject/cortex/pkg/ring/client"
	"github.com/cortexproject/cortex/pkg/storegateway/storegatewaypb"
	"github.com/cortexproject/cortex/pkg/util/services"
)

// BlocksStoreSet implementation used when the blocks are not sharded in the store-gateway
// and so requests are balanced across the set of store-gateway instances.
type blocksStoreBalancedSet struct {
	services.Service

	serviceAddresses []string
	clientsPool      *client.Pool
	dnsProvider      *dns.Provider
}

func newBlocksStoreBalancedSet(serviceAddresses []string, logger log.Logger, reg prometheus.Registerer) *blocksStoreBalancedSet {
	const dnsResolveInterval = 10 * time.Second

	dnsProviderReg := extprom.WrapRegistererWithPrefix("cortex_storegateway_client_", reg)

	s := &blocksStoreBalancedSet{
		serviceAddresses: serviceAddresses,
		dnsProvider:      dns.NewProvider(logger, dnsProviderReg, dns.GolangResolverType),
		clientsPool:      newStoreGatewayClientPool(nil, logger, reg),
	}

	s.Service = services.NewTimerService(dnsResolveInterval, s.starting, s.resolve, nil)
	return s
}

func (s *blocksStoreBalancedSet) starting(ctx context.Context) error {
	// Initial DNS resolution.
	return s.resolve(ctx)
}

func (s *blocksStoreBalancedSet) resolve(ctx context.Context) error {
	s.dnsProvider.Resolve(ctx, s.serviceAddresses)
	return nil
}

func (s *blocksStoreBalancedSet) GetClientsFor(_ []*metadata.Meta) ([]storegatewaypb.StoreGatewayClient, error) {
	addresses := s.dnsProvider.Addresses()
	if len(addresses) == 0 {
		return nil, fmt.Errorf("no address resolved for the store-gateway service addresses %s", strings.Join(s.serviceAddresses, ","))
	}

	// Pick a random address and return its client from the pool.
	addr := addresses[rand.Intn(len(addresses))]
	c, err := s.clientsPool.GetClientFor(addr)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get store-gateway client for %s", addr)
	}

	return []storegatewaypb.StoreGatewayClient{c.(storegatewaypb.StoreGatewayClient)}, nil
}
