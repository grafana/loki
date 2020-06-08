package querier

import (
	"context"

	"github.com/go-kit/kit/log"
	"github.com/oklog/ulid"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/ring/client"
	cortex_tsdb "github.com/cortexproject/cortex/pkg/storage/tsdb"
	"github.com/cortexproject/cortex/pkg/util/services"
	"github.com/cortexproject/cortex/pkg/util/tls"
)

// BlocksStoreSet implementation used when the blocks are sharded and replicated across
// a set of store-gateway instances.
type blocksStoreReplicationSet struct {
	services.Service

	storesRing  *ring.Ring
	clientsPool *client.Pool

	// Subservices manager.
	subservices        *services.Manager
	subservicesWatcher *services.FailureWatcher
}

func newBlocksStoreReplicationSet(storesRing *ring.Ring, tlsCfg tls.ClientConfig, logger log.Logger, reg prometheus.Registerer) (*blocksStoreReplicationSet, error) {
	s := &blocksStoreReplicationSet{
		storesRing:  storesRing,
		clientsPool: newStoreGatewayClientPool(client.NewRingServiceDiscovery(storesRing), tlsCfg, logger, reg),
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

func (s *blocksStoreReplicationSet) GetClientsFor(blockIDs []ulid.ULID) ([]BlocksStoreClient, error) {
	var sets []ring.ReplicationSet

	// Find the replication set of each block we need to query.
	for _, blockID := range blockIDs {
		// Buffer internally used by the ring (give extra room for a JOINING + LEAVING instance).
		// Do not reuse the same buffer across multiple Get() calls because we do retain the
		// returned replication set.
		buf := make([]ring.IngesterDesc, 0, s.storesRing.ReplicationFactor()+2)

		set, err := s.storesRing.Get(cortex_tsdb.HashBlockID(blockID), ring.BlocksRead, buf)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to get store-gateway replication set owning the block %s", blockID.String())
		}

		sets = append(sets, set)
	}

	var clients []BlocksStoreClient

	// Get the client for each store-gateway.
	for _, addr := range findSmallestInstanceSet(sets) {
		c, err := s.clientsPool.GetClientFor(addr)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to get store-gateway client for %s", addr)
		}

		clients = append(clients, c.(BlocksStoreClient))
	}

	return clients, nil
}

// findSmallestInstanceSet returns the minimal set of store-gateway instances including all required blocks.
// Blocks may be replicated across store-gateway instances, but we want to query the lowest number of instances
// possible, so this function tries to find the smallest set of store-gateway instances containing all blocks
// we need to query.
func findSmallestInstanceSet(sets []ring.ReplicationSet) []string {
	addr := findHighestInstanceOccurrences(sets)
	if addr == "" {
		return nil
	}

	// Remove any replication set containing the selected instance address.
	for i := 0; i < len(sets); {
		if sets[i].Includes(addr) {
			sets = append(sets[:i], sets[i+1:]...)
		} else {
			i++
		}
	}

	return append([]string{addr}, findSmallestInstanceSet(sets)...)
}

func findHighestInstanceOccurrences(sets []ring.ReplicationSet) string {
	var highestAddr string
	var highestCount int

	occurrences := map[string]int{}

	for _, set := range sets {
		for _, i := range set.Ingesters {
			if v, ok := occurrences[i.Addr]; ok {
				occurrences[i.Addr] = v + 1
			} else {
				occurrences[i.Addr] = 1
			}

			if occurrences[i.Addr] > highestCount {
				highestAddr = i.Addr
				highestCount = occurrences[i.Addr]
			}
		}
	}

	return highestAddr
}
