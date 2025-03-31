package frontend

import (
	"time"

	"github.com/jellydator/ttlcache/v3"
)

type PartitionAssignments map[int32]int64
type PartitionConsumersCache = *ttlcache.Cache[string, PartitionAssignments]

func NewPartitionConsumerCache(ttl time.Duration) PartitionConsumersCache {
	return ttlcache.New(
		ttlcache.WithTTL[string, PartitionAssignments](ttl),
		ttlcache.WithDisableTouchOnHit[string, PartitionAssignments](),
	)
}
