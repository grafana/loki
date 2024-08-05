package querier

import (
	"fmt"
	"sort"
	"strings"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/logproto"
)

const streamShardLabel = "__stream_shard__"

type Shard struct {
	ID     string
	Volume uint64
}

type Bucket struct {
	Shards []string
	Volume uint64
}

type AdaptiveShardDistributor struct {
	Shards  []Shard
	Buckets []Bucket
}

func NewAdaptiveShardDistributor(volumes []Shard) *AdaptiveShardDistributor {
	return &AdaptiveShardDistributor{
		Shards: volumes,
	}
}

func (d *AdaptiveShardDistributor) createBuckets(bucketCount int) {
	totalVolume := uint64(0)
	for _, shard := range d.Shards {
		totalVolume += shard.Volume
	}

	targetVolumePerBucket := totalVolume / uint64(bucketCount)
	d.Buckets = make([]Bucket, bucketCount)

	bucketIndex := 0
	for _, shard := range d.Shards {
		if bucketIndex >= bucketCount {
			bucketIndex = bucketCount - 1
		}
		d.Buckets[bucketIndex].Shards = append(d.Buckets[bucketIndex].Shards, shard.ID)
		d.Buckets[bucketIndex].Volume += shard.Volume

		if d.Buckets[bucketIndex].Volume >= targetVolumePerBucket && bucketIndex < bucketCount-1 {
			bucketIndex++
		}
	}
}

func (d *AdaptiveShardDistributor) DistributeShards(query string, start, end model.Time, bucketCount int) []*logproto.SubQueryResult {
	sort.Slice(d.Shards, func(i, j int) bool {
		return d.Shards[i].Volume > d.Shards[j].Volume
	})
	d.createBuckets(bucketCount)

	var subqueries []*logproto.SubQueryResult
	for i, bucket := range d.Buckets {
		subquery := d.createSubquery(query, start, end, bucket.Shards, i, bucket.Volume)
		subqueries = append(subqueries, subquery)
	}

	return subqueries
}

func (d *AdaptiveShardDistributor) createSubquery(originalQuery string, start, end model.Time, shards []string, bucketIndex int, vol uint64) *logproto.SubQueryResult {
	// Convert shard IDs to strings and join them with '|'
	//shardValues := make([]string, len(shards))
	//for i, shard := range shards {
	//	shardValues[i] = shard
	//}
	shardRegex := strings.Join(shards, "|")

	// Parse the original query to insert the __stream_shard__ label matcher
	var modifiedQuery string
	if strings.Contains(originalQuery, "{") && strings.Contains(originalQuery, "}") {
		// If the query already has label matchers, insert the __stream_shard__ matcher
		parts := strings.SplitN(originalQuery, "}", 2)
		modifiedQuery = fmt.Sprintf("%s, __stream_shard__=~\"%s\"}%s", parts[0], shardRegex, parts[1])
	} else {
		// If the query doesn't have label matchers, add them with the __stream_shard__ matcher
		parts := strings.SplitN(originalQuery, "[", 2)
		modifiedQuery = fmt.Sprintf("%s{__stream_shard__=~\"%s\"}[%s", parts[0], shardRegex, parts[1])
	}

	return &logproto.SubQueryResult{
		Query:  modifiedQuery,
		Start:  start.Time(),
		End:    end.Time(),
		Shards: shards,
		Volume: vol,
		Id:     fmt.Sprintf("sub%d", bucketIndex+1)}
}
