package indexgateway

import (
	"context"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/validation"
)

func TestCoalesceTableShardPlans(t *testing.T) {
	shard := func(from, through model.Fingerprint, chunks uint64) logproto.Shard {
		return logproto.Shard{Bounds: logproto.FPBounds{Min: from, Max: through}, Stats: &logproto.IndexStatsResponse{Streams: 1, Chunks: chunks, Bytes: chunks * 1024, Entries: chunks * 10}}
	}
	for _, tc := range []struct {
		name   string
		plans  [][]logproto.Shard
		bounds []logproto.FPBounds
		chunks []uint64
	}{
		{"different boundaries coalesce", [][]logproto.Shard{{shard(0, 4, 1), shard(5, 9, 2)}, {shard(0, 6, 3), shard(7, 9, 4)}}, []logproto.FPBounds{{Min: 0, Max: 9}}, []uint64{10}},
		{"shared boundaries remain", [][]logproto.Shard{{shard(0, 4, 1), shard(5, 9, 2)}, {shard(0, 4, 3), shard(5, 9, 4)}}, []logproto.FPBounds{{Min: 0, Max: 4}, {Min: 5, Max: 9}}, []uint64{4, 6}},
		{"maximum fingerprint", [][]logproto.Shard{{shard(0, math.MaxUint64, 1)}, {shard(math.MaxUint64, math.MaxUint64, 2)}}, []logproto.FPBounds{{Min: 0, Max: math.MaxUint64}}, []uint64{3}},
		{"empty", nil, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var plans []*logproto.ShardsResponse
			for _, ranges := range tc.plans {
				plans = append(plans, &logproto.ShardsResponse{Shards: ranges, ChunkGroups: make([]logproto.ChunkRefGroup, len(ranges))})
			}
			got := coalesceTableShardPlans(plans)
			require.Len(t, got.Shards, len(tc.bounds))
			require.Empty(t, got.ChunkGroups)
			for i, s := range got.Shards {
				require.Equal(t, tc.bounds[i], s.Bounds)
				require.Equal(t, tc.chunks[i], s.Stats.Chunks)
			}
			// Merging must not mutate responses reused by retries or callers.
			again := coalesceTableShardPlans(plans)
			require.Equal(t, got, again)
		})
	}
}

func TestMultiTableGetShardsBoundsConcurrency(t *testing.T) {
	f := newFanoutFixtureWithLimits(t, func(c *ClientConfig) { c.PerIndexMaxConcurrency = 2 }, validation.Limits{TSDBIndexShardFactor: 4})
	block := make(chan struct{})
	f.gateways.block = block
	var release sync.Once
	defer release.Do(func() { close(block) })
	ctx := user.InjectOrgID(context.Background(), "tenant")
	done := make(chan error, 1)
	go func() {
		_, err := f.client.GetShards(ctx, &logproto.ShardsRequest{From: dayStart(20000), Through: dayStart(20002) - 1, Query: `{a="b"}`})
		done <- err
	}()
	require.Eventually(t, func() bool { return len(f.gateways.recorded()) == 2 }, 5*time.Second, time.Millisecond)
	release.Do(func() { close(block) })
	require.NoError(t, <-done)
	require.Len(t, f.gateways.recorded(), 8)
	require.Equal(t, 2, f.gateways.maxInFlight)
}

func TestMultiTableGetShardsFailsWithoutPartialResults(t *testing.T) {
	f := newFanoutFixtureWithLimits(t, nil, validation.Limits{TSDBIndexShardFactor: 4})
	all, err := f.client.ring.GetAllHealthy(IndexOwnersRead)
	require.NoError(t, err)
	f.gateways.failing = map[string]bool{}
	for _, addr := range all.GetAddresses() {
		f.gateways.failing[addr] = true
	}
	ctx := user.InjectOrgID(context.Background(), "tenant")
	response, err := f.client.GetShards(ctx, &logproto.ShardsRequest{From: dayStart(20000), Through: dayStart(20002) - 1, Query: `{a="b"}`})
	require.Error(t, err)
	require.Nil(t, response)
}

func TestConcatShardsDiscardsIncompleteChunkGroups(t *testing.T) {
	plans := []*logproto.ShardsResponse{
		{Shards: []logproto.Shard{{Bounds: logproto.FPBounds{Min: 0, Max: math.MaxUint64}, Stats: &logproto.IndexStatsResponse{Streams: 1, Chunks: 1}}}, ChunkGroups: []logproto.ChunkRefGroup{{Refs: []*logproto.ChunkRef{{Fingerprint: 1}}}}},
		{Shards: []logproto.Shard{{Bounds: logproto.FPBounds{Min: 0, Max: math.MaxUint64}, Stats: &logproto.IndexStatsResponse{Streams: 1, Chunks: 1}}}},
	}
	got := concatShardsResponses(plans, IndexShards(2))
	require.Len(t, got.Shards, 2)
	require.Empty(t, got.ChunkGroups)
}
