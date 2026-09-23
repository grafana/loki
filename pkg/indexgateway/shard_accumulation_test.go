package indexgateway

import (
	"fmt"
	"math/rand"
	"sort"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/sharding"
)

// accumulateChunksBaseline groups chunk refs before sizing them, providing a
// reference for the direct accumulation implementation.
func accumulateChunksBaseline(
	req *logproto.ShardsRequest,
	filtered []logproto.ChunkRefWithSizingInfo,
) ([]logproto.Shard, error) {
	// map for looking up post-filtered chunks in O(n) while iterating the index again for sizing info
	filteredM := make(map[model.Fingerprint][]logproto.ChunkRefWithSizingInfo, 1024)
	for _, ref := range filtered {
		filteredM[model.Fingerprint(ref.Fingerprint)] = append(filteredM[model.Fingerprint(ref.Fingerprint)], ref)
	}

	collectedSeries := sharding.SizedFPs(sharding.SizedFPsPool.Get(len(filteredM)))
	defer func() { sharding.SizedFPsPool.Put(collectedSeries) }()

	for fp, chks := range filteredM {
		x := sharding.SizedFP{Fp: fp}
		x.Stats.Chunks = uint64(len(chks))

		for _, chk := range chks {
			x.Stats.Entries += uint64(chk.Entries)
			x.Stats.Bytes += uint64(chk.KB << 10)
		}
		collectedSeries = append(collectedSeries, x)
	}
	sort.Sort(collectedSeries)

	return collectedSeries.ShardsFor(req.TargetBytesPerShard), nil
}

func TestShardAccumulationMatchesGroupedChunks(t *testing.T) {
	for _, tc := range []struct{ series, chunks int }{{0, 0}, {1, 1}, {10, 100}, {1000, 10}} {
		t.Run(fmt.Sprintf("series=%d/chunks=%d", tc.series, tc.chunks), func(t *testing.T) {
			refs := buildChunkRefs(tc.series, tc.chunks)
			rand.New(rand.NewSource(42)).Shuffle(len(refs), func(i, j int) { refs[i], refs[j] = refs[j], refs[i] })
			for _, target := range []uint64{1, 600 << 20, 1 << 40} {
				req := &logproto.ShardsRequest{TargetBytesPerShard: target}
				want, err := accumulateChunksBaseline(req, refs)
				require.NoError(t, err)
				got, err := accumulateChunksToShards(req, refs)
				require.NoError(t, err)
				require.Equal(t, want, got)
			}
		})
	}
}

func TestShardAccumulationSizingEdges(t *testing.T) {
	refs := []logproto.ChunkRefWithSizingInfo{
		{ChunkRef: logproto.ChunkRef{Fingerprint: 0}, KB: 0, Entries: 0},
		{ChunkRef: logproto.ChunkRef{Fingerprint: ^uint64(0)}, KB: ^uint32(0), Entries: ^uint32(0)},
		{ChunkRef: logproto.ChunkRef{Fingerprint: 0}, KB: 1 << 22, Entries: 1},
	}
	// Duplicates still count, and the existing uint32 KB shift semantics are preserved.
	refs = append(refs, refs...)
	for _, target := range []uint64{0, 1, 600 << 20, ^uint64(0)} {
		req := &logproto.ShardsRequest{TargetBytesPerShard: target}
		want, err := accumulateChunksBaseline(req, refs)
		require.NoError(t, err)
		got, err := accumulateChunksToShards(req, refs)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
}
