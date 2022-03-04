package retention

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/local"
	"github.com/grafana/loki/pkg/storage/chunk/storage"
)

func Test_ChunkIterator(t *testing.T) {
	for _, tt := range allSchemas {
		tt := tt
		t.Run(tt.schema, func(t *testing.T) {
			cm := storage.NewClientMetrics()
			defer cm.Unregister()
			store := newTestStore(t, cm)
			c1 := createChunk(t, "1", labels.Labels{labels.Label{Name: "foo", Value: "bar"}}, tt.from, tt.from.Add(1*time.Hour))
			c2 := createChunk(t, "2", labels.Labels{labels.Label{Name: "foo", Value: "buzz"}, labels.Label{Name: "bar", Value: "foo"}}, tt.from, tt.from.Add(1*time.Hour))

			require.NoError(t, store.Put(context.TODO(), []chunk.Chunk{
				c1, c2,
			}))

			store.Stop()

			tables := store.indexTables()
			require.Len(t, tables, 1)
			var actual []ChunkEntry
			err := tables[0].DB.Update(func(tx *bbolt.Tx) error {
				it, err := NewChunkIndexIterator(tx.Bucket(local.IndexBucketName), tt.config)
				require.NoError(t, err)
				for it.Next() {
					require.NoError(t, it.Err())
					actual = append(actual, it.Entry())
					// delete the last entry
					if len(actual) == 2 {
						require.NoError(t, it.Delete())
					}
				}
				return nil
			})
			require.NoError(t, err)
			require.Equal(t, []ChunkEntry{
				entryFromChunk(store.schemaCfg.SchemaConfig, c1),
				entryFromChunk(store.schemaCfg.SchemaConfig, c2),
			}, actual)

			// second pass we delete c2
			actual = actual[:0]
			err = tables[0].DB.Update(func(tx *bbolt.Tx) error {
				it, err := NewChunkIndexIterator(tx.Bucket(local.IndexBucketName), tt.config)
				require.NoError(t, err)
				for it.Next() {
					actual = append(actual, it.Entry())
				}
				return it.Err()
			})
			require.NoError(t, err)
			require.Equal(t, []ChunkEntry{
				entryFromChunk(store.schemaCfg.SchemaConfig, c1),
			}, actual)
		})
	}
}

func Test_SeriesCleaner(t *testing.T) {
	for _, tt := range allSchemas {
		tt := tt
		t.Run(tt.schema, func(t *testing.T) {
			cm := storage.NewClientMetrics()
			defer cm.Unregister()
			testSchema := chunk.SchemaConfig{Configs: []chunk.PeriodConfig{tt.config}}
			store := newTestStore(t, cm)
			c1 := createChunk(t, "1", labels.Labels{labels.Label{Name: "foo", Value: "bar"}}, tt.from, tt.from.Add(1*time.Hour))
			c2 := createChunk(t, "2", labels.Labels{labels.Label{Name: "foo", Value: "buzz"}, labels.Label{Name: "bar", Value: "foo"}}, tt.from, tt.from.Add(1*time.Hour))
			c3 := createChunk(t, "2", labels.Labels{labels.Label{Name: "foo", Value: "buzz"}, labels.Label{Name: "bar", Value: "buzz"}}, tt.from, tt.from.Add(1*time.Hour))

			require.NoError(t, store.Put(context.TODO(), []chunk.Chunk{
				c1, c2, c3,
			}))

			store.Stop()

			tables := store.indexTables()
			require.Len(t, tables, 1)
			// remove c1, c2 chunk
			err := tables[0].DB.Update(func(tx *bbolt.Tx) error {
				it, err := NewChunkIndexIterator(tx.Bucket(local.IndexBucketName), tt.config)
				require.NoError(t, err)
				for it.Next() {
					require.NoError(t, it.Err())
					if it.Entry().Labels.Get("bar") == "foo" {
						require.NoError(t, it.Delete())
					}
				}
				return nil
			})
			require.NoError(t, err)

			err = tables[0].DB.Update(func(tx *bbolt.Tx) error {
				cleaner := newSeriesCleaner(tx.Bucket(local.IndexBucketName), tt.config, tables[0].name)
				if err := cleaner.Cleanup(entryFromChunk(testSchema, c2).UserID, c2.Metric); err != nil {
					return err
				}

				// remove series for c1 without __name__ label, which should work just fine
				return cleaner.Cleanup(entryFromChunk(testSchema, c1).UserID, c1.Metric.WithoutLabels(labels.MetricName))
			})
			require.NoError(t, err)

			err = tables[0].DB.View(func(tx *bbolt.Tx) error {
				return tx.Bucket(local.IndexBucketName).ForEach(func(k, _ []byte) error {
					c1SeriesID := entryFromChunk(testSchema, c1).SeriesID
					c2SeriesID := entryFromChunk(testSchema, c2).SeriesID
					series, ok, err := parseLabelIndexSeriesID(decodeKey(k))
					if !ok {
						return nil
					}
					if err != nil {
						return err
					}

					if string(c1SeriesID) == string(series) {
						require.Fail(t, "series for c1 should be deleted", c1SeriesID)
					} else if string(c2SeriesID) == string(series) {
						require.Fail(t, "series for c2 should be deleted", c2SeriesID)
					}

					return nil
				})
			})
			require.NoError(t, err)
		})
	}
}

func entryFromChunk(s chunk.SchemaConfig, c chunk.Chunk) ChunkEntry {
	return ChunkEntry{
		ChunkRef: ChunkRef{
			UserID:   []byte(c.UserID),
			SeriesID: labelsSeriesID(c.Metric),
			ChunkID:  []byte(s.ExternalKey(c)),
			From:     c.From,
			Through:  c.Through,
		},
		Labels: c.Metric.WithoutLabels("__name__"),
	}
}

var chunkEntry ChunkEntry

func Benchmark_ChunkIterator(b *testing.B) {
	cm := storage.NewClientMetrics()
	defer cm.Unregister()
	store := newTestStore(b, cm)
	for i := 0; i < 100; i++ {
		require.NoError(b, store.Put(context.TODO(),
			[]chunk.Chunk{
				createChunk(b, "1",
					labels.Labels{labels.Label{Name: "foo", Value: "bar"}, labels.Label{Name: "i", Value: fmt.Sprintf("%d", i)}},
					allSchemas[0].from, allSchemas[0].from.Add(1*time.Hour)),
			},
		))
	}
	store.Stop()
	b.ReportAllocs()
	b.ResetTimer()

	var total int64
	_ = store.indexTables()[0].Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(local.IndexBucketName)
		for n := 0; n < b.N; n++ {
			it, err := NewChunkIndexIterator(bucket, allSchemas[0].config)
			require.NoError(b, err)
			for it.Next() {
				chunkEntry = it.Entry()
				require.NoError(b, it.Delete())
				total++
			}
		}
		return errors.New("don't commit")
	})
	b.Logf("Total chunk ref:%d", total)
}
