package indexgateway_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/indexgateway"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

func BenchmarkOrderedShardPlanning(b *testing.B) {
	b.Run("mmap", func(b *testing.B) { benchmarkFileShardPlanning(b, index.MmapOptions{}) })
	b.Run("stream", func(b *testing.B) { benchmarkFileShardPlanning(b, index.DefaultStreamOptions()) })
}

func benchmarkFileShardPlanning(b *testing.B, opts index.ReaderOptions) {
	b.Helper()
	// Two indexes overlap in both streams and chunks.
	const streamsPerFile, sharedStreams = 90000, 60000
	const chunksPerStream, sharedChunks = 24, 12
	dir := b.TempDir()
	var indexes tsdb.IndexSlice
	for file := range 2 {
		builder := tsdb.NewBuilder(index.FormatV3)
		var chunks [chunksPerStream]index.ChunkMeta
		for i := range chunks {
			id := i + file*(chunksPerStream-sharedChunks)
			start := int64(id*100 + 1)
			chunks[i] = index.ChunkMeta{MinTime: start, MaxTime: start + 50, Checksum: uint32(id + 1), KB: 1024, Entries: 100}
		}
		firstStream := file * (streamsPerFile - sharedStreams)
		for stream := firstStream; stream < firstStream+streamsPerFile; stream++ {
			ls := labels.FromStrings("app", "synthetic", "stream", strconv.Itoa(stream))
			builder.AddSeries(ls, model.Fingerprint(labels.StableHash(ls)), chunks[:])
		}
		id, err := builder.Build(b.Context(), dir, func(from, through model.Time, checksum uint32) tsdb.Identifier {
			return tsdb.NewPrefixedIdentifier(tsdb.SingleTenantTSDBIdentifier{
				TS: time.Unix(int64(file+1), 0), From: from, Through: through, Checksum: checksum,
			}, dir, dir)
		})
		require.NoError(b, err)
		idx, err := tsdb.NewShippableTSDBFile(id, opts)
		require.NoError(b, err)
		b.Cleanup(func() { require.NoError(b, idx.Close()) })
		indexes = append(indexes, idx)
	}
	req := &logproto.ShardsRequest{From: 1, Through: 5000, TargetBytesPerShard: 600 << 20}
	client := tsdb.NewIndexClient(tsdb.NewMultiIndex(indexes), tsdb.IndexClientOptions{}, nil)
	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "synthetic")}
	const uniqueRefs = 2*streamsPerFile*chunksPerStream - sharedStreams*sharedChunks
	cursors, err := client.OpenChunkCursors(b.Context(), "synthetic", req.From, req.Through, matchers...)
	require.NoError(b, err)
	_, err = indexgateway.PlanShardsFromCursorsForTest(b.Context(), req, false, cursors)
	require.NoError(b, err)
	b.ReportAllocs()
	for b.Loop() {
		cursors, err := client.OpenChunkCursors(b.Context(), "synthetic", req.From, req.Through, matchers...)
		if err != nil {
			b.Fatal(err)
		}
		resp, err := indexgateway.PlanShardsFromCursorsForTest(b.Context(), req, false, cursors)
		if err != nil {
			b.Fatal(err)
		}
		if got := resp.Statistics.Index.TotalChunks; got != uniqueRefs {
			b.Fatalf("resolved %d refs, want %d", got, uniqueRefs)
		}
	}
}
