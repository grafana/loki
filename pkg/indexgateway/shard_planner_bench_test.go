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

func BenchmarkBoundedShardPlanning(b *testing.B) {
	// Two mmap-backed indexes overlap in both streams and chunks. Build
	// them once so each batch size reads the same synthetic data.
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
		idx, _, err := tsdb.NewTSDBIndexFromFile(id.Path(), index.MmapOptions{})
		require.NoError(b, err)
		b.Cleanup(func() { require.NoError(b, idx.Close()) })
		indexes = append(indexes, idx)
	}
	req := &logproto.ShardsRequest{From: 1, Through: 5000, TargetBytesPerShard: 600 << 20}
	client := tsdb.NewIndexClient(tsdb.NewMultiIndex(indexes), tsdb.IndexClientOptions{}, nil)
	series, ok := client.HasForSeries(req.From, req.Through)
	require.True(b, ok)
	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "synthetic")}
	const uniqueRefs = 2*streamsPerFile*chunksPerStream - sharedStreams*sharedChunks
	indexgateway.MeasureShardPlanning(b, series, req, matchers, uniqueRefs)
}
