package tsdb

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/config"
	index_shipper "github.com/grafana/loki/pkg/storage/stores/indexshipper/index"
)

type mockIndexShipperIndexIterator struct {
	tables map[string][]*TSDBFile
}

func (m mockIndexShipperIndexIterator) ForEachConcurrent(ctx context.Context, tableName, userID string, callback index_shipper.ForEachIndexCallback) error {
	return m.ForEach(ctx, tableName, userID, callback)
}

func (m mockIndexShipperIndexIterator) ForEach(ctx context.Context, tableName, userID string, callback index_shipper.ForEachIndexCallback) error {
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
	tableRanges := config.TableRanges{
		{
			Start: 0,
			End:   math.MaxInt64,
			PeriodConfig: &config.PeriodConfig{
				IndexTables: config.PeriodicTableConfig{
					Period: config.ObjectStorageIndexRequiredPeriod,
				},
			},
		},
	}

	indexStartToday := model.TimeFromUnixNano(time.Now().Truncate(config.ObjectStorageIndexRequiredPeriod).UnixNano())
	indexStartYesterday := indexStartToday.Add(-config.ObjectStorageIndexRequiredPeriod)

	tables := map[string][]*TSDBFile{
		tableRanges[0].PeriodConfig.IndexTables.TableFor(indexStartToday): {
			BuildIndex(b, tempDir, []LoadableSeries{
				{
					Labels: mustParseLabels(`{foo="bar"}`),
					Chunks: buildChunkMetas(int64(indexStartToday), int64(indexStartToday+99)),
				},
			}),
		},

		tableRanges[0].PeriodConfig.IndexTables.TableFor(indexStartYesterday): {
			BuildIndex(b, tempDir, []LoadableSeries{
				{
					Labels: mustParseLabels(`{foo="bar"}`),
					Chunks: buildChunkMetas(int64(indexStartYesterday), int64(indexStartYesterday+99)),
				},
			}),
		},
	}

	idx := newIndexShipperQuerier(mockIndexShipperIndexIterator{tables: tables}, config.TableRanges{
		{
			Start:        0,
			End:          math.MaxInt64,
			PeriodConfig: &config.PeriodConfig{},
		},
	})

	indexClient := NewIndexClient(idx, IndexClientOptions{UseBloomFilters: true})

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
	tableRanges := config.TableRanges{
		{
			Start: 0,
			End:   math.MaxInt64,
			PeriodConfig: &config.PeriodConfig{
				IndexTables: config.PeriodicTableConfig{
					Period: config.ObjectStorageIndexRequiredPeriod,
				},
			},
		},
	}

	indexStartToday := model.TimeFromUnixNano(time.Now().Truncate(config.ObjectStorageIndexRequiredPeriod).UnixNano())
	indexStartYesterday := indexStartToday.Add(-config.ObjectStorageIndexRequiredPeriod)

	tables := map[string][]*TSDBFile{
		tableRanges[0].PeriodConfig.IndexTables.TableFor(indexStartToday): {
			BuildIndex(t, tempDir, []LoadableSeries{
				{
					Labels: mustParseLabels(`{foo="bar"}`),
					Chunks: buildChunkMetas(int64(indexStartToday), int64(indexStartToday+99)),
				},
				{
					Labels: mustParseLabels(`{fizz="buzz"}`),
					Chunks: buildChunkMetas(int64(indexStartToday), int64(indexStartToday+99)),
				},
			}),
		},

		tableRanges[0].PeriodConfig.IndexTables.TableFor(indexStartYesterday): {
			BuildIndex(t, tempDir, []LoadableSeries{
				{
					Labels: mustParseLabels(`{foo="bar"}`),
					Chunks: buildChunkMetas(int64(indexStartYesterday), int64(indexStartYesterday+99)),
				},
				{
					Labels: mustParseLabels(`{foo="bar", fizz="buzz"}`),
					Chunks: buildChunkMetas(int64(indexStartYesterday), int64(indexStartYesterday+99)),
				},
				{
					Labels: mustParseLabels(`{ping="pong"}`),
					Chunks: buildChunkMetas(int64(indexStartYesterday), int64(indexStartYesterday+99)),
				},
			}),
		},
	}

	idx := newIndexShipperQuerier(mockIndexShipperIndexIterator{tables: tables}, config.TableRanges{
		{
			Start:        0,
			End:          math.MaxInt64,
			PeriodConfig: &config.PeriodConfig{},
		},
	})

	indexClient := NewIndexClient(idx, IndexClientOptions{UseBloomFilters: true})

	for _, tc := range []struct {
		name               string
		queryInterval      model.Interval
		expectedNumChunks  uint64
		expectedNumEntries uint64
		expectedNumStreams uint64
	}{
		{
			name: "request spanning 2 tables",
			queryInterval: model.Interval{
				Start: indexStartYesterday,
				End:   indexStartToday + 1000,
			},
			expectedNumChunks:  300,
			expectedNumEntries: 300,
			expectedNumStreams: 2,
		},
		{
			name: "request spanning just today",
			queryInterval: model.Interval{
				Start: indexStartToday,
				End:   indexStartToday + 1000,
			},
			expectedNumChunks:  100,
			expectedNumEntries: 100,
			expectedNumStreams: 1,
		},
		{
			name: "request selecting just few of the chunks from today",
			queryInterval: model.Interval{
				Start: indexStartToday + 50,
				End:   indexStartToday + 60,
			},
			expectedNumChunks:  10, // end time not inclusive
			expectedNumEntries: 10,
			expectedNumStreams: 1,
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
			require.Equal(t, tc.expectedNumEntries, stats.Chunks)
			require.Equal(t, tc.expectedNumEntries, stats.Entries)
			require.Equal(t, tc.expectedNumStreams, stats.Streams)
		})
	}
}
