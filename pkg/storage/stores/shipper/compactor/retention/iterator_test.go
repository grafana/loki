package retention

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	shipper_util "github.com/grafana/loki/pkg/storage/stores/shipper/util"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"
	"go.etcd.io/bbolt"
)

func Test_ChunkIterator(t *testing.T) {
	for _, tt := range allSchemas {
		tt := tt
		t.Run(tt.schema, func(t *testing.T) {
			store := newTestStore(t)
			defer store.cleanup()
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
				it, err := newChunkIndexIterator(tx.Bucket(bucketName), tt.config)
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
				entryFromChunk(c1),
				entryFromChunk(c2),
			}, actual)

			// second pass we delete c2
			actual = actual[:0]
			err = tables[0].DB.Update(func(tx *bbolt.Tx) error {
				it, err := newChunkIndexIterator(tx.Bucket(bucketName), tt.config)
				require.NoError(t, err)
				for it.Next() {
					actual = append(actual, it.Entry())
				}
				return it.Err()
			})
			require.NoError(t, err)
			require.Equal(t, []ChunkEntry{
				entryFromChunk(c1),
			}, actual)
		})
	}
}

func entryFromChunk(c chunk.Chunk) ChunkEntry {
	return ChunkEntry{
		ChunkRef: ChunkRef{
			UserID:   []byte(c.UserID),
			SeriesID: labelsSeriesID(c.Metric),
			ChunkID:  []byte(c.ExternalKey()),
			From:     c.From,
			Through:  c.Through,
		},
		Labels: c.Metric.WithoutLabels("__name__"),
	}
}

var chunkEntry ChunkEntry

func Benchmark_ChunkIterator(b *testing.B) {
	b.ReportAllocs()

	db, err := shipper_util.SafeOpenBoltdbFile("/Users/ctovena/Downloads/index_loki_ops_index_18669_compactor-1617841099")
	require.NoError(b, err)
	t, err := time.Parse("2006-01-02", "2020-07-31")
	require.NoError(b, err)
	var total int64
	db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bucketName)
		for n := 0; n < b.N; n++ {
			it, err := newChunkIndexIterator(bucket, chunk.PeriodConfig{
				From:      chunk.DayTime{Time: model.TimeFromUnix(t.Unix())},
				Schema:    "v11",
				RowShards: 16,
			})
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
