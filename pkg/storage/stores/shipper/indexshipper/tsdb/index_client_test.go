package tsdb

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/seriesvolume"
	shipperindex "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/index"
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
