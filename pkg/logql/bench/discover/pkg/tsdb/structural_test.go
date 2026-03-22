package tsdb

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
	tsdbindex "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

func TestStructuralMergeDeduplicatesAcrossIndexes(t *testing.T) {
	base := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)

	shared := map[string]string{"service_name": "querier", "namespace": "loki", "cluster": "prod"}
	unique := map[string]string{"service_name": "ingester", "namespace": "loki", "cluster": "prod"}

	pathA := buildStructuralIndexFixture(t, []structuralFixtureSeries{
		{Labels: shared, Chunks: []tsdbindex.ChunkMeta{newStructuralChunk(base, 1, 10)}},
		{Labels: unique, Chunks: []tsdbindex.ChunkMeta{newStructuralChunk(base.Add(20*time.Minute), 2, 8)}},
	})
	pathB := buildStructuralIndexFixture(t, []structuralFixtureSeries{
		{Labels: shared, Chunks: []tsdbindex.ChunkMeta{newStructuralChunk(base.Add(45*time.Minute), 3, 12)}},
	})

	indexes := openStructuralIndexes(t, pathA, pathB)

	result, err := RunStructuralDiscovery(StructuralConfig{}, indexes)
	require.NoError(t, err)

	sharedSelector := loghttp.LabelSet(shared).String()
	uniqueSelector := loghttp.LabelSet(unique).String()

	require.Equal(t, 3, result.TotalRaw)
	require.Equal(t, 2, result.TotalUnique)
	require.Equal(t, 2, result.TotalSelected)
	require.Equal(t, []string{uniqueSelector, sharedSelector}, result.AllSelectors)
	require.Equal(t, loghttp.LabelSet(shared), result.LabelSets[sharedSelector])

	mergedShared, ok := result.MergedStreams[sharedSelector]
	require.True(t, ok)
	require.Equal(t, 2, mergedShared.SourceCount)
	require.Len(t, mergedShared.ChunkMetas, 2)
}

func TestStructuralIndexesBuildBoundedSortedMaps(t *testing.T) {
	base := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)

	streamA := map[string]string{"service_name": "svc-api", "namespace": "loki", "cluster": "prod", "pod": "pod-b"}
	streamB := map[string]string{"service_name": "svc-api", "namespace": "loki", "cluster": "prod", "pod": "pod-a"}
	streamC := map[string]string{"service_name": "svc-db", "namespace": "db", "container": "pg", "custom_tag": "x"}

	path := buildStructuralIndexFixture(t, []structuralFixtureSeries{
		{Labels: streamA, Chunks: []tsdbindex.ChunkMeta{newStructuralChunk(base, 1, 3)}},
		{Labels: streamB, Chunks: []tsdbindex.ChunkMeta{newStructuralChunk(base.Add(5*time.Minute), 2, 4)}},
		{Labels: streamC, Chunks: []tsdbindex.ChunkMeta{newStructuralChunk(base.Add(10*time.Minute), 3, 5)}},
	})

	indexes := openStructuralIndexes(t, path)

	result, err := RunStructuralDiscovery(StructuralConfig{}, indexes)
	require.NoError(t, err)

	selA := loghttp.LabelSet(streamA).String()
	selB := loghttp.LabelSet(streamB).String()
	selC := loghttp.LabelSet(streamC).String()

	require.Equal(t, []string{selB, selA}, result.ByServiceName["svc-api"])
	require.Equal(t, []string{selC}, result.ByServiceName["svc-db"])

	require.Equal(t, []string{selB, selA}, result.ByLabelKey["pod"])
	require.Equal(t, []string{selB, selA, selC}, result.ByLabelKey["namespace"])
	require.Equal(t, []string{selB, selA}, result.ByLabelKey["cluster"])
	require.Equal(t, []string{selC}, result.ByLabelKey["container"])
	require.NotContains(t, result.ByLabelKey, "custom_tag")

	for _, selectors := range result.ByServiceName {
		require.True(t, sort.StringsAreSorted(selectors))
	}
	for _, selectors := range result.ByLabelKey {
		require.True(t, sort.StringsAreSorted(selectors))
	}
}

func TestStructuralDeterministicOutputAcrossRuns(t *testing.T) {
	base := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)

	shared := map[string]string{"service_name": "frontend", "namespace": "prod", "cluster": "c1"}
	onlyA := map[string]string{"service_name": "backend", "namespace": "prod", "cluster": "c1", "pod": "a"}
	onlyB := map[string]string{"service_name": "backend", "namespace": "prod", "cluster": "c1", "pod": "b"}

	pathA := buildStructuralIndexFixture(t, []structuralFixtureSeries{
		{Labels: onlyA, Chunks: []tsdbindex.ChunkMeta{newStructuralChunk(base, 1, 4)}},
		{Labels: shared, Chunks: []tsdbindex.ChunkMeta{newStructuralChunk(base.Add(30*time.Minute), 2, 6)}},
	})
	pathB := buildStructuralIndexFixture(t, []structuralFixtureSeries{
		{Labels: shared, Chunks: []tsdbindex.ChunkMeta{newStructuralChunk(base.Add(45*time.Minute), 3, 9)}},
		{Labels: onlyB, Chunks: []tsdbindex.ChunkMeta{newStructuralChunk(base.Add(60*time.Minute), 4, 5)}},
	})

	indexesA := openStructuralIndexes(t, pathA, pathB)
	indexesB := openStructuralIndexes(t, pathB, pathA)

	first, err := RunStructuralDiscovery(StructuralConfig{}, indexesA)
	require.NoError(t, err)
	second, err := RunStructuralDiscovery(StructuralConfig{}, indexesB)
	require.NoError(t, err)

	require.Equal(t, first.AllSelectors, second.AllSelectors)
	require.Equal(t, first.LabelSets, second.LabelSets)
	require.Equal(t, first.ByServiceName, second.ByServiceName)
	require.Equal(t, first.ByLabelKey, second.ByLabelKey)
	require.Equal(t, first.TotalRaw, second.TotalRaw)
	require.Equal(t, first.TotalUnique, second.TotalUnique)
	require.Equal(t, first.TotalSelected, second.TotalSelected)
	require.Equal(t, first.MergedStreams, second.MergedStreams)
}

type structuralFixtureSeries struct {
	Labels map[string]string
	Chunks []tsdbindex.ChunkMeta
}

func buildStructuralIndexFixture(t *testing.T, series []structuralFixtureSeries) string {
	t.Helper()

	dir := t.TempDir()
	b := tsdb.NewBuilder(tsdbindex.FormatV3)

	for _, stream := range series {
		lbls := labels.FromMap(stream.Labels)
		b.AddSeries(lbls, model.Fingerprint(labels.StableHash(lbls)), stream.Chunks)
	}

	id, err := b.Build(context.Background(), dir, func(from, through model.Time, checksum uint32) tsdb.Identifier {
		return tsdb.NewPrefixedIdentifier(tsdb.SingleTenantTSDBIdentifier{
			TS:       time.Unix(0, 0),
			From:     from,
			Through:  through,
			Checksum: checksum,
		}, dir, "")
	})
	require.NoError(t, err)

	return id.Path()
}

func openStructuralIndexes(t *testing.T, paths ...string) []tsdb.Index {
	t.Helper()

	res := make([]tsdb.Index, 0, len(paths))
	for _, path := range paths {
		idx, _, err := tsdb.NewTSDBIndexFromFile(path)
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, idx.Close())
		})
		res = append(res, idx)
	}

	return res
}

func newStructuralChunk(start time.Time, checksum, entries uint32) tsdbindex.ChunkMeta {
	from := model.TimeFromUnixNano(start.UnixNano())
	through := model.TimeFromUnixNano(start.Add(5 * time.Minute).UnixNano())

	return tsdbindex.ChunkMeta{
		Checksum: checksum,
		MinTime:  int64(from),
		MaxTime:  int64(through),
		KB:       1,
		Entries:  entries,
	}
}
