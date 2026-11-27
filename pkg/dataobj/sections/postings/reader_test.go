package postings_test

import (
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/memory"
)

func TestReader_ReadRecordBatch(t *testing.T) {
	t.Run("returns an error when the reader is not open", func(t *testing.T) {
		r := postings.NewReader(postings.ReaderOptions{})

		batch, err := r.ReadRecordBatch(t.Context(), newAllocator(t), 128)
		require.Nil(t, batch)
		require.EqualError(t, err, "reader not opened")
	})

	t.Run("returns an error when the batch size is not positive", func(t *testing.T) {
		r := openReader(t, 1)

		for _, batchSize := range []int{0, -1} {
			batch, err := r.ReadRecordBatch(t.Context(), newAllocator(t), batchSize)
			require.Nil(t, batch)
			require.EqualError(t, err, fmt.Sprintf("reading record batch: batch size must be positive, got %d", batchSize))
		}
	})

	t.Run("returns an error when the allocator is nil", func(t *testing.T) {
		r := openReader(t, 1)

		batch, err := r.ReadRecordBatch(t.Context(), nil, 128)
		require.Nil(t, batch)
		require.EqualError(t, err, "reading record batch: allocator is nil")
	})

	t.Run("returns every row in batches of one to the batch size rows, then nil and io.EOF on every later call", func(t *testing.T) {
		const (
			rows      = 5
			batchSize = 2
		)
		r := openReader(t, rows)
		alloc := newAllocator(t)

		var total int
		for total < rows {
			alloc.Reclaim()
			batch, err := r.ReadRecordBatch(t.Context(), alloc, batchSize)
			require.NoError(t, err)
			require.GreaterOrEqual(t, batch.Rows, 1)
			require.LessOrEqual(t, batch.Rows, batchSize)
			total += batch.Rows
		}
		require.Equal(t, rows, total)

		for range 2 {
			batch, err := r.ReadRecordBatch(t.Context(), alloc, batchSize)
			require.Nil(t, batch)
			require.Equal(t, io.EOF, err)
		}
	})

	t.Run("returns the value of every column of a label row", func(t *testing.T) {
		ts := time.Unix(0, 1234).UTC()
		b := postings.NewBuilder(nil, 0, 0, 1<<20)
		b.ObserveLabelPosting(postings.LabelObservation{
			ObjectPath:       "/obj",
			ShardBuckets:     16,
			SectionIndex:     3,
			ColumnName:       "env",
			LabelValue:       "prod",
			StreamID:         1,
			Time:             postings.At(ts),
			UncompressedSize: 100,
			ShardBucket:      5,
		})
		secs, closer := openPostingsSections(t.Context(), t, b)
		t.Cleanup(closer)
		require.Len(t, secs, 1)

		r := postings.NewReader(postings.ReaderOptions{Columns: secs[0].Columns()})
		require.NoError(t, r.Open(t.Context()))
		t.Cleanup(func() { require.NoError(t, r.Close()) })

		batch, err := r.ReadRecordBatch(t.Context(), newAllocator(t), 128)
		require.NoError(t, err)
		require.Equal(t, 1, batch.Rows)

		require.Equal(t, int64(postings.KindLabel), batch.Kind.Get(0))
		require.Equal(t, "/obj", string(batch.ObjectPath.Get(0)))
		require.Equal(t, int64(16), batch.ShardBuckets.Get(0))
		require.Equal(t, int64(3), batch.SectionIndex.Get(0))
		require.Equal(t, "env", string(batch.ColumnName.Get(0)))
		require.Equal(t, "prod", string(batch.LabelValue.Get(0)))
		require.True(t, batch.BloomFilter.IsNull(0))
		require.NotEmpty(t, batch.StreamIDBitmap.Get(0))
		require.Equal(t, int64(100), batch.UncompressedSize.Get(0))
		require.Equal(t, ts.UnixNano(), batch.MinTimestamp.Get(0))
		require.Equal(t, ts.UnixNano(), batch.MaxTimestamp.Get(0))
		require.Equal(t, int64(5), batch.MinShardBucket.Get(0))
		require.Equal(t, int64(5), batch.MaxShardBucket.Get(0))
	})

	t.Run("returns the arrays in the order of the columns when the caller changes its slice after Open", func(t *testing.T) {
		minTS := time.Unix(0, 100).UTC()
		maxTS := time.Unix(0, 200).UTC()
		b := postings.NewBuilder(nil, 0, 0, 1<<20)
		b.ObserveLabelPosting(postings.LabelObservation{
			ObjectPath:   "/obj",
			SectionIndex: 0,
			ColumnName:   "env",
			LabelValue:   "prod",
			StreamID:     1,
			Time:         postings.At(minTS).Extend(maxTS),
		})
		secs, closer := openPostingsSections(t.Context(), t, b)
		t.Cleanup(closer)
		require.Len(t, secs, 1)

		columns := []*postings.Column{
			getSectionColumn(t, secs[0], postings.ColumnTypeMinTimestamp),
			getSectionColumn(t, secs[0], postings.ColumnTypeMaxTimestamp),
		}
		r := postings.NewReader(postings.ReaderOptions{Columns: columns})
		require.NoError(t, r.Open(t.Context()))
		t.Cleanup(func() { require.NoError(t, r.Close()) })

		columns[0], columns[1] = columns[1], columns[0]

		batch, err := r.ReadRecordBatch(t.Context(), newAllocator(t), 128)
		require.NoError(t, err)
		require.Equal(t, minTS.UnixNano(), batch.MinTimestamp.Get(0))
		require.Equal(t, maxTS.UnixNano(), batch.MaxTimestamp.Get(0))
	})
}

// openReader returns an open Reader over all the columns of a section that holds
// the given number of label rows.
func openReader(t *testing.T, rows int) *postings.Reader {
	t.Helper()

	b := postings.NewBuilder(nil, 0, 0, 1<<20)
	for i := range rows {
		b.ObserveLabelPosting(postings.LabelObservation{
			ObjectPath:   "/obj",
			SectionIndex: 0,
			ColumnName:   "env",
			LabelValue:   fmt.Sprintf("value-%d", i),
			StreamID:     int64(i),
			Time:         postings.At(time.Unix(0, 0).UTC()),
		})
	}
	secs, closer := openPostingsSections(t.Context(), t, b)
	t.Cleanup(closer)
	require.Len(t, secs, 1)

	r := postings.NewReader(postings.ReaderOptions{Columns: secs[0].Columns()})
	require.NoError(t, r.Open(t.Context()))
	t.Cleanup(func() { require.NoError(t, r.Close()) })
	return r
}

func newAllocator(t *testing.T) *memory.Allocator {
	t.Helper()

	alloc := memory.NewAllocator(nil)
	t.Cleanup(alloc.Free)
	return alloc
}
