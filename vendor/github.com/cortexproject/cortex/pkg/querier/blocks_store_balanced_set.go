package querier

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/oklog/ulid"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/thanos/pkg/discovery/dns"
	"github.com/thanos-io/thanos/pkg/extprom"

	"github.com/cortexproject/cortex/pkg/ring/client"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/services"
)

// BlocksStoreSet implementation used when the blocks are not sharded in the store-gateway
// and so requests are balanced across the set of store-gateway instances.
type blocksStoreBalancedSet struct {
	services.Service

	serviceAddresses []string
	clientsPool      *client.Pool
	dnsProvider      *dns.Provider

	logger log.Logger
}

func newBlocksStoreBalancedSet(serviceAddresses []string, clientConfig ClientConfig, logger log.Logger, reg prometheus.Registerer) *blocksStoreBalancedSet {
	const dnsResolveInterval = 10 * time.Second

	dnsProviderReg := extprom.WrapRegistererWithPrefix("cortex_storegateway_client_", reg)

	s := &blocksStoreBalancedSet{
		serviceAddresses: serviceAddresses,
		dnsProvider:      dns.NewProvider(logger, dnsProviderReg, dns.GolangResolverType),
		clientsPool:      newStoreGatewayClientPool(nil, clientConfig, logger, reg),
		logger:           logger,
	}

	s.Service = services.NewTimerService(dnsResolveInterval, s.starting, s.resolve, nil)
	return s
}

func (s *blocksStoreBalancedSet) starting(ctx context.Context) error {
	// Initial DNS resolution.
	return s.resolve(ctx)
}

func (s *blocksStoreBalancedSet) resolve(ctx context.Context) error {
	if err := s.dnsProvider.Resolve(ctx, s.serviceAddresses); err != nil {
		level.Error(s.logger).Log("msg", "failed to resolve store-gateway addresses", "err", err, "addresses", s.serviceAddresses)
	}
	return nil
}

func (s *blocksStoreBalancedSet) GetClientsFor(_ string, blockIDs []ulid.ULID, exclude map[ulid.ULID][]string) (map[BlocksStoreClient][]ulid.ULID, error) {
	addresses := s.dnsProvider.Addresses()
	if len(addresses) == 0 {
		return nil, fmt.Errorf("no address resolved for the store-gateway service addresses %s", strings.Join(s.serviceAddresses, ","))
	}

	// Randomize the list of addresses to not always query the same address.
	rand.Shuffle(len(addresses), func(i, j int) {
		addresses[i], addresses[j] = addresses[j], addresses[i]
	})

	// Pick a non excluded client for each block.
	clients := map[BlocksStoreClient][]ulid.ULID{}

	for _, blockID := range blockIDs {
		// Pick the first non excluded store-gateway instance.
		addr := getFirstNonExcludedAddr(addresses, exclude[blockID])
		if addr == "" {
			return nil, fmt.Errorf("no store-gateway instance left after filtering out excluded instances for block %s", blockID.String())
		}

		c, err := s.clientsPool.GetClientFor(addr)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to get store-gateway client for %s", addr)
		}

		clients[c.(BlocksStoreClient)] = append(clients[c.(BlocksStoreClient)], blockID)
	}

	return clients, nil
}

func getFirstNonExcludedAddr(addresses, exclude []string) string {
	for _, addr := range addresses {
		if !util.StringsContain(exclude, addr) {
			return addr
		}
	}

	return ""
}
