package querier

import (
	"context"
	"fmt"
	"math/rand"

	"github.com/go-kit/kit/log"
	"github.com/oklog/ulid"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/ring/client"
	cortex_tsdb "github.com/cortexproject/cortex/pkg/storage/tsdb"
	"github.com/cortexproject/cortex/pkg/storegateway"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/services"
)

type loadBalancingStrategy int

const (
	noLoadBalancing = loadBalancingStrategy(iota)
	randomLoadBalancing
)

// BlocksStoreSet implementation used when the blocks are sharded and replicated across
// a set of store-gateway instances.
type blocksStoreReplicationSet struct {
	services.Service

	storesRing        *ring.Ring
	clientsPool       *client.Pool
	shardingStrategy  string
	balancingStrategy loadBalancingStrategy
	limits            BlocksStoreLimits

	// Subservices manager.
	subservices        *services.Manager
	subservicesWatcher *services.FailureWatcher
}

func newBlocksStoreReplicationSet(
	storesRing *ring.Ring,
	shardingStrategy string,
	balancingStrategy loadBalancingStrategy,
	limits BlocksStoreLimits,
	clientConfig ClientConfig,
	logger log.Logger,
	reg prometheus.Registerer,
) (*blocksStoreReplicationSet, error) {
	s := &blocksStoreReplicationSet{
		storesRing:        storesRing,
		clientsPool:       newStoreGatewayClientPool(client.NewRingServiceDiscovery(storesRing), clientConfig, logger, reg),
		shardingStrategy:  shardingStrategy,
		balancingStrategy: balancingStrategy,
		limits:            limits,
	}

	var err error
	s.subservices, err = services.NewManager(s.storesRing, s.clientsPool)
	if err != nil {
		return nil, err
	}

	s.Service = services.NewBasicService(s.starting, s.running, s.stopping)

	return s, nil
}

func (s *blocksStoreReplicationSet) starting(ctx context.Context) error {
	s.subservicesWatcher.WatchManager(s.subservices)

	if err := services.StartManagerAndAwaitHealthy(ctx, s.subservices); err != nil {
		return errors.Wrap(err, "unable to start blocks store set subservices")
	}

	return nil
}

func (s *blocksStoreReplicationSet) running(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-s.subservicesWatcher.Chan():
			return errors.Wrap(err, "blocks store set subservice failed")
		}
	}
}

func (s *blocksStoreReplicationSet) stopping(_ error) error {
	return services.StopManagerAndAwaitStopped(context.Background(), s.subservices)
}

func (s *blocksStoreReplicationSet) GetClientsFor(userID string, blockIDs []ulid.ULID, exclude map[ulid.ULID][]string) (map[BlocksStoreClient][]ulid.ULID, error) {
	shards := map[string][]ulid.ULID{}

	// If shuffle sharding is enabled, we should build a subring for the user,
	// otherwise we just use the full ring.
	var userRing ring.ReadRing
	if s.shardingStrategy == util.ShardingStrategyShuffle {
		userRing = storegateway.GetShuffleShardingSubring(s.storesRing, userID, s.limits)
	} else {
		userRing = s.storesRing
	}

	// Find the replication set of each block we need to query.
	for _, blockID := range blockIDs {
		// Do not reuse the same buffer across multiple Get() calls because we do retain the
		// returned replication set.
		bufDescs, bufHosts, bufZones := ring.MakeBuffersForGet()

		set, err := userRing.Get(cortex_tsdb.HashBlockID(blockID), storegateway.BlocksRead, bufDescs, bufHosts, bufZones)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to get store-gateway replication set owning the block %s", blockID.String())
		}

		// Pick a non excluded store-gateway instance.
		addr := getNonExcludedInstanceAddr(set, exclude[blockID], s.balancingStrategy)
		if addr == "" {
			return nil, fmt.Errorf("no store-gateway instance left after checking exclude for block %s", blockID.String())
		}

		shards[addr] = append(shards[addr], blockID)
	}

	clients := map[BlocksStoreClient][]ulid.ULID{}

	// Get the client for each store-gateway.
	for addr, blockIDs := range shards {
		c, err := s.clientsPool.GetClientFor(addr)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to get store-gateway client for %s", addr)
		}

		clients[c.(BlocksStoreClient)] = blockIDs
	}

	return clients, nil
}

func getNonExcludedInstanceAddr(set ring.ReplicationSet, exclude []string, balancingStrategy loadBalancingStrategy) string {
	if balancingStrategy == randomLoadBalancing {
		// Randomize the list of instances to not always query the same one.
		rand.Shuffle(len(set.Instances), func(i, j int) {
			set.Instances[i], set.Instances[j] = set.Instances[j], set.Instances[i]
		})
	}

	for _, instance := range set.Instances {
		if !util.StringsContain(exclude, instance.Addr) {
			return instance.Addr
		}
	}

	return ""
}
