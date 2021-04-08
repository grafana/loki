package retention

import (
	"context"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"
	"go.etcd.io/bbolt"
)

func Test_ChunkIterator(t *testing.T) {
	store := newTestStore(t)
	defer store.cleanup()
	c1 := createChunk(t, "1", labels.Labels{labels.Label{Name: "foo", Value: "bar"}}, model.Earliest, model.Earliest.Add(1*time.Hour))
	c2 := createChunk(t, "2", labels.Labels{labels.Label{Name: "foo", Value: "buzz"}, labels.Label{Name: "bar", Value: "foo"}}, model.Earliest, model.Earliest.Add(1*time.Hour))

	require.NoError(t, store.Put(context.TODO(), []chunk.Chunk{
		c1, c2,
	}))

	store.Stop()

	tables := store.indexTables()
	require.Len(t, tables, 1)
	var actual []*ChunkRef
	err := tables[0].DB.Update(func(tx *bbolt.Tx) error {
		it := newBoltdbChunkIndexIterator(tx.Bucket(bucketName))
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
	require.Equal(t, []*ChunkRef{
		refFromChunk(c1),
		refFromChunk(c2),
	}, actual)

	// second pass we delete c2
	actual = actual[:0]
	err = tables[0].DB.Update(func(tx *bbolt.Tx) error {
		it := newBoltdbChunkIndexIterator(tx.Bucket(bucketName))
		for it.Next() {
			actual = append(actual, it.Entry())
		}
		return it.Err()
	})
	require.NoError(t, err)
	require.Equal(t, []*ChunkRef{
		refFromChunk(c1),
	}, actual)
}

func refFromChunk(c chunk.Chunk) *ChunkRef {
	return &ChunkRef{
		UserID:   []byte(c.UserID),
		SeriesID: labelsSeriesID(c.Metric),
		ChunkID:  []byte(c.ExternalKey()),
		From:     c.From,
		Through:  c.Through,
	}
}
