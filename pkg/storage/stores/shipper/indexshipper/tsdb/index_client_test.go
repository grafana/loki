package tsdb

import (
	"context"
	"fmt"
	"math"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/querier/astmapper"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/seriesvolume"
	shipperindex "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/sharding"
)

type mockIndexShipperIndexIterator struct {
	tables map[string][]*TSDBFile
}

func (m mockIndexShipperIndexIterator) ForEachConcurrent(ctx context.Context, tableName, userID string, callback shipperindex.ForEachIndexCallback) error {
	return m.ForEach(ctx, tableName, userID, callback)
}

func (m mockIndexShipperIndexIterator) ForEach(_ context.Context, tableName, _ string, callback shipperindex.ForEachIndexCallback) error {
	indexes := m.tables[tableName]
	for _, idx := range indexes {
		if err := callback(false, idx); err != nil {
			return err
		}
	}

	return nil
}

func BenchmarkIndexClient_Stats(b *testing.B) {
	tempDir := b.TempDir()
	tableRange := config.TableRange{
		Start: 0,
		End:   math.MaxInt64,
		PeriodConfig: &config.PeriodConfig{
			IndexTables: config.IndexPeriodicTableConfig{
				PeriodicTableConfig: config.PeriodicTableConfig{
					Period: config.ObjectStorageIndexRequiredPeriod,
				}},
		},
	}

	indexStartToday := model.TimeFromUnixNano(time.Now().Truncate(config.ObjectStorageIndexRequiredPeriod).UnixNano())
	indexStartYesterday := indexStartToday.Add(-config.ObjectStorageIndexRequiredPeriod)

	tables := map[string][]*TSDBFile{
		tableRange.PeriodConfig.IndexTables.TableFor(indexStartToday): {
			BuildIndex(b, tempDir, []LoadableSeries{
				{
					Labels: mustParseLabels(`{foo="bar"}`),
					Chunks: buildChunkMetas(int64(indexStartToday), int64(indexStartToday+99)),
				},
			}),
		},

		tableRange.PeriodConfig.IndexTables.TableFor(indexStartYesterday): {
			BuildIndex(b, tempDir, []LoadableSeries{
				{
					Labels: mustParseLabels(`{foo="bar"}`),
					Chunks: buildChunkMetas(int64(indexStartYesterday), int64(indexStartYesterday+99)),
				},
			}),
		},
	}

	idx := newIndexShipperQuerier(mockIndexShipperIndexIterator{tables: tables}, config.TableRange{
		Start:        0,
		End:          math.MaxInt64,
		PeriodConfig: &config.PeriodConfig{},
	})

	indexClient := NewIndexClient(idx, IndexClientOptions{UseBloomFilters: true}, &fakeLimits{})

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		stats, err := indexClient.Stats(context.Background(), "", indexStartYesterday-1000, model.Now()+1000, labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"))
		require.NoError(b, err)
		require.Equal(b, uint64(200), stats.Chunks)
		require.Equal(b, uint64(200), stats.Entries)
	}

}

func TestIndexClient_Stats(t *testing.T) {
	tempDir := t.TempDir()
	tableRange := config.TableRange{
		Start: 0,
		End:   math.MaxInt64,
		PeriodConfig: &config.PeriodConfig{
			IndexTables: config.IndexPeriodicTableConfig{
				PeriodicTableConfig: config.PeriodicTableConfig{
					Period: config.ObjectStorageIndexRequiredPeriod,
				}},
		},
	}

	indexStartToday := model.TimeFromUnixNano(time.Now().Truncate(config.ObjectStorageIndexRequiredPeriod).UnixNano())
	indexStartYesterday := indexStartToday.Add(-config.ObjectStorageIndexRequiredPeriod)

	tables := map[string][]*TSDBFile{
		tableRange.PeriodConfig.IndexTables.TableFor(indexStartToday): {
			BuildIndex(t, tempDir, []LoadableSeries{
				{
					Labels: mustParseLabels(`{foo="bar"}`),
					Chunks: buildChunkMetas(int64(indexStartToday), int64(indexStartToday+99), 10),
				},
				{
					Labels: mustParseLabels(`{fizz="buzz"}`),
					Chunks: buildChunkMetas(int64(indexStartToday), int64(indexStartToday+99), 10),
				},
			}),
		},

		tableRange.PeriodConfig.IndexTables.TableFor(indexStartYesterday): {
			BuildIndex(t, tempDir, []LoadableSeries{
				{
					Labels: mustParseLabels(`{foo="bar"}`),
					Chunks: buildChunkMetas(int64(indexStartYesterday), int64(indexStartYesterday+99), 10),
				},
				{
					Labels: mustParseLabels(`{foo="bar", fizz="buzz"}`),
					Chunks: buildChunkMetas(int64(indexStartYesterday), int64(indexStartYesterday+99), 10),
				},
				{
					Labels: mustParseLabels(`{ping="pong"}`),
					Chunks: buildChunkMetas(int64(indexStartYesterday), int64(indexStartYesterday+99), 10),
				},
			}),
		},
	}

	idx := newIndexShipperQuerier(mockIndexShipperIndexIterator{tables: tables}, config.TableRange{
		Start:        0,
		End:          math.MaxInt64,
		PeriodConfig: &config.PeriodConfig{},
	})

	indexClient := NewIndexClient(idx, IndexClientOptions{UseBloomFilters: true}, &fakeLimits{})

	for _, tc := range []struct {
		name               string
		queryInterval      model.Interval
		expectedNumChunks  uint64
		expectedNumEntries uint64
		expectedNumStreams uint64
		expectedNumBytes   uint64
	}{
		{
			name: "request spanning 2 tables",
			queryInterval: model.Interval{
				Start: indexStartYesterday,
				End:   indexStartToday + 1000,
			},
			expectedNumChunks:  30,
			expectedNumEntries: 300,
			expectedNumStreams: 2,
			expectedNumBytes:   300 * 1024,
		},
		{
			name: "request spanning just today",
			queryInterval: model.Interval{
				Start: indexStartToday,
				End:   indexStartToday + 1000,
			},
			expectedNumChunks:  10,
			expectedNumEntries: 100,
			expectedNumStreams: 1,
			expectedNumBytes:   100 * 1024,
		},
		{
			name: "request selecting just few of the chunks from today",
			queryInterval: model.Interval{
				Start: indexStartToday + 50,
				End:   indexStartToday + 60,
			},
			// end time is inclusive because chunks are indexed by their start and end, although the chunk's stats contributions get reduced to zero
			// in integer division due to 1 nanosecond overlap
			expectedNumChunks:  2,
			expectedNumEntries: 10,
			expectedNumStreams: 1,
			expectedNumBytes:   10 * 1024,
		},
		{
			name: "request selecting two chunks partially from today",
			queryInterval: model.Interval{
				Start: indexStartToday + 58,
				End:   indexStartToday + 62,
			},
			expectedNumChunks:  2,
			expectedNumEntries: 20 * 0.2,
			expectedNumStreams: 1,
			expectedNumBytes:   20 * 0.2 * 1024,
		},
		{
			name: "request not touching any chunks",
			queryInterval: model.Interval{
				Start: indexStartToday + 2000,
				End:   indexStartToday + 3000,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stats, err := indexClient.Stats(context.Background(), "", tc.queryInterval.Start, tc.queryInterval.End, labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"))
			require.NoError(t, err)
			require.Equal(t, tc.expectedNumChunks, stats.Chunks)
			require.Equal(t, tc.expectedNumEntries, stats.Entries)
			require.Equal(t, tc.expectedNumStreams, stats.Streams)
			require.Equal(t, tc.expectedNumBytes, stats.Bytes)
		})
	}
}

func TestIndexClient_Volume(t *testing.T) {
	tempDir := t.TempDir()
	tableRange := config.TableRange{
		Start: 0,
		End:   math.MaxInt64,
		PeriodConfig: &config.PeriodConfig{
			IndexTables: config.IndexPeriodicTableConfig{
				PeriodicTableConfig: config.PeriodicTableConfig{
					Period: config.ObjectStorageIndexRequiredPeriod,
				}},
		},
	}

	indexStartToday := model.TimeFromUnixNano(time.Now().Truncate(config.ObjectStorageIndexRequiredPeriod).UnixNano())
	indexStartYesterday := indexStartToday.Add(-config.ObjectStorageIndexRequiredPeriod)

	tables := map[string][]*TSDBFile{
		tableRange.PeriodConfig.IndexTables.TableFor(indexStartToday): {
			BuildIndex(t, tempDir, []LoadableSeries{
				{
					Labels: mustParseLabels(`{foo="bar"}`),
					Chunks: buildChunkMetas(int64(indexStartToday), int64(indexStartToday+99), 10),
				},
				{
					Labels: mustParseLabels(`{fizz="buzz"}`),
					Chunks: buildChunkMetas(int64(indexStartToday), int64(indexStartToday+99), 10),
				},
			}),
		},

		tableRange.PeriodConfig.IndexTables.TableFor(indexStartYesterday): {
			BuildIndex(t, tempDir, []LoadableSeries{
				{
					Labels: mustParseLabels(`{foo="bar"}`),
					Chunks: buildChunkMetas(int64(indexStartYesterday), int64(indexStartYesterday+99), 10),
				},
				{
					Labels: mustParseLabels(`{foo="bar", fizz="buzz"}`),
					Chunks: buildChunkMetas(int64(indexStartYesterday), int64(indexStartYesterday+99), 10),
				},
				{
					Labels: mustParseLabels(`{ping="pong"}`),
					Chunks: buildChunkMetas(int64(indexStartYesterday), int64(indexStartYesterday+99), 10),
				},
			}),
		},
	}

	idx := newIndexShipperQuerier(mockIndexShipperIndexIterator{tables: tables}, config.TableRange{
		Start:        0,
		End:          math.MaxInt64,
		PeriodConfig: &config.PeriodConfig{},
	})

	limits := &fakeLimits{volumeMaxSeries: 5}
	indexClient := NewIndexClient(idx, IndexClientOptions{UseBloomFilters: true}, limits)
	from := indexStartYesterday
	through := indexStartToday + 1000

	t.Run("it returns volumes from the whole index", func(t *testing.T) {
		vol, err := indexClient.Volume(context.Background(), "", from, through, 10, nil, "", nil...)
		require.NoError(t, err)

		require.Equal(t, &logproto.VolumeResponse{
			Volumes: []logproto.Volume{
				{Name: `{foo="bar"}`, Volume: 200 * 1024},
				{Name: `{fizz="buzz", foo="bar"}`, Volume: 100 * 1024},
				{Name: `{fizz="buzz"}`, Volume: 100 * 1024},
				{Name: `{ping="pong"}`, Volume: 100 * 1024},
			},
			Limit: 10,
		}, vol)
	})

	t.Run("it returns largest series from the index", func(t *testing.T) {
		vol, err := indexClient.Volume(context.Background(), "", from, through, 1, nil, "", nil...)
		require.NoError(t, err)

		require.Equal(t, &logproto.VolumeResponse{
			Volumes: []logproto.Volume{
				{Name: `{foo="bar"}`, Volume: 200 * 1024},
			},
			Limit: 1,
		}, vol)
	})

	t.Run("it returns an error when the number of selected series exceeds the limit", func(t *testing.T) {
		limits.volumeMaxSeries = 0
		_, err := indexClient.Volume(context.Background(), "", from, through, 1, nil, "", nil...)
		require.EqualError(t, err, fmt.Sprintf(seriesvolume.ErrVolumeMaxSeriesHit, 0))
	})
}

type fakeLimits struct {
	volumeMaxSeries int
}

func (f *fakeLimits) VolumeMaxSeries(_ string) int {
	return f.volumeMaxSeries
}

func TestIndexShipperQuerier_ConcurrentChunkFilterer(t *testing.T) {
	tempDir := t.TempDir()
	tableRange := config.TableRange{
		Start: 0,
		End:   math.MaxInt64,
		PeriodConfig: &config.PeriodConfig{
			IndexTables: config.IndexPeriodicTableConfig{
				PeriodicTableConfig: config.PeriodicTableConfig{
					Period: config.ObjectStorageIndexRequiredPeriod,
				}},
		},
	}
	indexStart := model.TimeFromUnixNano(time.Now().Truncate(config.ObjectStorageIndexRequiredPeriod).UnixNano())
	tables := map[string][]*TSDBFile{
		tableRange.PeriodConfig.IndexTables.TableFor(indexStart): {
			BuildIndex(t, tempDir, []LoadableSeries{
				{
					Labels: mustParseLabels(`{foo="bar"}`),
					Chunks: buildChunkMetas(int64(indexStart), int64(indexStart+99)),
				},
			}),
		},
	}
	q := newIndexShipperQuerier(mockIndexShipperIndexIterator{tables: tables}, tableRange)
	idx := NewMultiIndex(IndexSlice{q, q})
	matcher := labels.MustNewMatcher(labels.MatchEqual, "foo", "bar")

	// Populate PeriodConfig.schemaInt before concurrent queries.
	_, err := q.GetChunkRefs(context.Background(), "fake", indexStart, indexStart+100, nil, nil, matcher)
	require.NoError(t, err)

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			idx.SetChunkFilterer(&filterAll{})
			_, err := idx.GetChunkRefs(context.Background(), "fake", indexStart, indexStart+100, nil, nil, matcher)
			require.NoError(t, err)
		}()
	}
	wg.Wait()
}

type labelValuesIndex struct {
	Index
	labelValues func(...*labels.Matcher) ([]string, error)
}

func (i labelValuesIndex) LabelValues(_ context.Context, _ string, _, _ model.Time, _ string, matchers ...*labels.Matcher) ([]string, error) {
	return i.labelValues(matchers...)
}

func TestIndexClient_LabelValuesForMetricName(t *testing.T) {
	metric := labels.MustNewMatcher(labels.MatchEqual, model.MetricNameLabel, "logs")
	shard := labels.MustNewMatcher(labels.MatchEqual, astmapper.ShardLabel, "0_of_2")
	appMatcher := labels.MustNewMatcher(labels.MatchEqual, "app", "loki")
	for _, tc := range []struct {
		name      string
		matchers  []*labels.Matcher
		want      []*labels.Matcher
		wantError bool
	}{
		{name: "no matchers"},
		{name: "metric only", matchers: []*labels.Matcher{metric}},
		{name: "shard only", matchers: []*labels.Matcher{shard}},
		{name: "metric and shard", matchers: []*labels.Matcher{metric, shard}},
		{name: "real matcher", matchers: []*labels.Matcher{appMatcher}, want: []*labels.Matcher{appMatcher}},
		{name: "strip metric and shard", matchers: []*labels.Matcher{metric, shard, appMatcher}, want: []*labels.Matcher{appMatcher}},
		{name: "invalid shard", matchers: []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, astmapper.ShardLabel, "invalid")}, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			idx := labelValuesIndex{labelValues: func(matchers ...*labels.Matcher) ([]string, error) {
				called = true
				require.Len(t, matchers, len(tc.want))
				for n, matcher := range tc.want {
					require.Equal(t, matcher, matchers[n])
				}
				return []string{"loki"}, nil
			}}
			client := NewIndexClient(idx, IndexClientOptions{}, &fakeLimits{})
			values, err := client.LabelValuesForMetricName(context.Background(), "tenant", 0, 100, "logs", "app", tc.matchers...)
			if tc.wantError {
				require.Error(t, err)
				require.False(t, called)
				return
			}
			require.NoError(t, err)
			require.True(t, called)
			require.Equal(t, []string{"loki"}, values)
		})
	}
}

func TestIndexClient_OpenChunkCursorsFilters(t *testing.T) {
	const tenant = "fake"
	// The fingerprint of app=a is in the lower half, and app=f is in the upper half.
	appA := labels.FromStrings("app", "a")
	appF := labels.FromStrings("app", "f")
	var files IndexSlice
	for range 2 {
		file := BuildIndex(t, t.TempDir(), []LoadableSeries{
			{Labels: appA, Chunks: index.ChunkMetas{
				{MinTime: 0, MaxTime: 20, Checksum: 1, KB: 3, Entries: 7},
				{MinTime: 10, MaxTime: 30, Checksum: 2, KB: 4, Entries: 9},
				{MinTime: 40, MaxTime: 50, Checksum: 3, KB: 4, Entries: 9}, // Outside the requested time range.
			}},
			{Labels: appF, Chunks: index.ChunkMetas{
				{MinTime: 10, MaxTime: 50, Checksum: 4, KB: 5, Entries: 11},
			}},
		})
		t.Cleanup(func() { require.NoError(t, file.Close()) })
		files = append(files, file)
	}
	client := NewIndexClient(NewMultiIndex(files), IndexClientOptions{}, &fakeLimits{})
	wantA := []logproto.ChunkRefWithSizingInfo{
		{ChunkRef: logproto.ChunkRef{UserID: tenant, Fingerprint: labels.StableHash(appA), From: 0, Through: 20, Checksum: 1}, KB: 3, Entries: 7},
		{ChunkRef: logproto.ChunkRef{UserID: tenant, Fingerprint: labels.StableHash(appA), From: 10, Through: 30, Checksum: 2}, KB: 4, Entries: 9},
	}
	wantF := []logproto.ChunkRefWithSizingInfo{
		{ChunkRef: logproto.ChunkRef{UserID: tenant, Fingerprint: labels.StableHash(appF), From: 10, Through: 50, Checksum: 4}, KB: 5, Entries: 11},
	}
	wantAll := slices.Concat(wantA, wantF)
	metric := labels.MustNewMatcher(labels.MatchEqual, model.MetricNameLabel, "logs")
	shard := labels.MustNewMatcher(labels.MatchEqual, astmapper.ShardLabel, "0_of_2")
	app := labels.MustNewMatcher(labels.MatchEqual, "app", "a")
	for _, tc := range []struct {
		name     string
		matchers []*labels.Matcher
		want     []logproto.ChunkRefWithSizingInfo
		wantErr  bool
	}{
		{name: "all matching chunks", want: wantAll},
		{name: "ignore legacy metric name", matchers: []*labels.Matcher{metric}, want: wantAll},
		{name: "label matcher", matchers: []*labels.Matcher{app}, want: wantA},
		{name: "shard matcher", matchers: []*labels.Matcher{metric, shard}, want: wantA},
		{name: "last shard matcher", matchers: []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, astmapper.ShardLabel, "1_of_2")}, want: wantF},
		{name: "invalid shard", matchers: []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, astmapper.ShardLabel, "invalid")}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := slices.Clone(tc.matchers)
			cursors, err := client.OpenChunkCursors(t.Context(), tenant, 5, 25, tc.matchers...)
			t.Cleanup(func() { require.NoError(t, sharding.CloseChunkCursors(cursors)) })
			require.Equal(t, original, tc.matchers, "the caller's matchers must not be mutated")
			if tc.wantErr {
				require.Error(t, err)
				require.Empty(t, cursors)
				return
			}
			require.NoError(t, err)
			require.Len(t, cursors, 2)
			for _, cursor := range cursors {
				var got []logproto.ChunkRefWithSizingInfo
				for cursor.Next() {
					got = append(got, cursor.At())
				}
				require.NoError(t, cursor.Err())
				require.Equal(t, tc.want, got)
			}
		})
	}
}

func TestIndexClient_OpenChunkCursorsMaximumFingerprint(t *testing.T) {
	b := NewBuilder(index.FormatV3)
	b.AddSeries(labels.FromStrings("app", "max"), math.MaxUint64, index.ChunkMetas{{MinTime: 0, MaxTime: 10, Checksum: 1}})
	dir := t.TempDir()
	id, err := b.Build(context.Background(), dir, func(from, through model.Time, checksum uint32) Identifier {
		return NewPrefixedIdentifier(SingleTenantTSDBIdentifier{TS: time.Unix(1, 0), From: from, Through: through, Checksum: checksum}, dir, dir)
	})
	require.NoError(t, err)
	file, err := NewShippableTSDBFile(id, index.MmapOptions{})
	require.NoError(t, err)
	defer func() { require.NoError(t, file.Close()) }()
	client := NewIndexClient(NewMultiIndex(IndexSlice{file}), IndexClientOptions{}, &fakeLimits{})
	cursors, err := client.OpenChunkCursors(t.Context(), "fake", 0, 10, labels.MustNewMatcher(labels.MatchEqual, astmapper.ShardLabel, "1_of_2"))
	t.Cleanup(func() { require.NoError(t, sharding.CloseChunkCursors(cursors)) })
	require.NoError(t, err)
	require.Len(t, cursors, 1)
	cursor := cursors[0]
	require.True(t, cursor.Next())
	require.EqualValues(t, uint64(math.MaxUint64), cursor.At().Fingerprint)
	require.False(t, cursor.Next())
	require.NoError(t, cursor.Err())
}
