package frontend

import (
	"time"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/jellydator/ttlcache/v3"
)

type PartitionConsumersCache = *ttlcache.Cache[string, logproto.GetAssignedPartitionsResponse]

func NewPartitionConsumerCache(ttl time.Duration) PartitionConsumersCache {
	return ttlcache.New(
		ttlcache.WithTTL[string, logproto.GetAssignedPartitionsResponse](ttl),
		ttlcache.WithDisableTouchOnHit[string, logproto.GetAssignedPartitionsResponse](),
	)
}
