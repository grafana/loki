package indexpointers_test

import (
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/memory"
)

// TestReader does a basic end-to-end test over a reader with a predicate applied.
func TestReaderWithoutPredicate(t *testing.T) {
	sec := buildSection(t, []indexpointers.IndexPointer{
		{Path: "path1", StartTs: unixTime(10), EndTs: unixTime(20)},
		{Path: "path2", StartTs: unixTime(30), EndTs: unixTime(40)},
		{Path: "path3", StartTs: unixTime(50), EndTs: unixTime(60)},
	})

	var (
		pathCol         = sec.Columns()[0]
		minTimestampCol = sec.Columns()[1]
		maxTimestampCol = sec.Columns()[2]
	)

	require.Equal(t, "path", pathCol.Name)
	require.Equal(t, indexpointers.ColumnTypePath, pathCol.Type)
	require.Equal(t, "min_timestamp", minTimestampCol.Name)
	require.Equal(t, indexpointers.ColumnTypeMinTimestamp, minTimestampCol.Type)
	require.Equal(t, "max_timestamp", maxTimestampCol.Name)
	require.Equal(t, indexpointers.ColumnTypeMaxTimestamp, maxTimestampCol.Type)

	for _, tt := range []struct {
		name     string
		columns  []*indexpointers.Column
		expected []indexpointers.IndexPointer
	}{
		{
			name:    "basic reads with selected columns",
			columns: []*indexpointers.Column{pathCol},
			expected: []indexpointers.IndexPointer{
				{Path: "path1"},
				{Path: "path2"},
				{Path: "path3"},
			},
		},
		{
			name:    "basic reads with all columns",
			columns: []*indexpointers.Column{pathCol, minTimestampCol, maxTimestampCol},
			expected: []indexpointers.IndexPointer{
				{Path: "path1", StartTs: unixTime(10).UTC(), EndTs: unixTime(20).UTC()},
				{Path: "path2", StartTs: unixTime(30).UTC(), EndTs: unixTime(40).UTC()},
				{Path: "path3", StartTs: unixTime(50).UTC(), EndTs: unixTime(60).UTC()},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := indexpointers.NewReader(indexpointers.ReaderOptions{Columns: tt.columns})
			t.Cleanup(func() { require.NoError(t, r.Close()) })

			require.Equal(t, tt.expected, readPointers(t, r))
		})
	}
}

// TestReaderWithTimestampPredicates tests reading with timestamp predicates.
func TestReaderWithTimestampPredicates(t *testing.T) {
	var (
		t10  = unixTime(10)
		t20  = unixTime(20)
		t25  = unixTime(25)
		t25s = scalar.NewTimestampScalar(arrow.Timestamp(t25.UnixNano()), &arrow.TimestampType{Unit: arrow.Nanosecond})
		t30  = unixTime(30)
		t40  = unixTime(40)
		t50  = unixTime(50)
		t55  = unixTime(55)
		t55s = scalar.NewTimestampScalar(arrow.Timestamp(t55.UnixNano()), &arrow.TimestampType{Unit: arrow.Nanosecond})
		t60  = unixTime(60)
	)
	sec := buildSection(t, []indexpointers.IndexPointer{
		{Path: "path1", StartTs: t10, EndTs: t20},
		{Path: "path2", StartTs: t30, EndTs: t40},
		{Path: "path3", StartTs: t50, EndTs: t60},
	})

	var (
		pathCol         = sec.Columns()[0]
		minTimestampCol = sec.Columns()[1]
		maxTimestampCol = sec.Columns()[2]
	)

	r := indexpointers.NewReader(indexpointers.ReaderOptions{
		Columns: []*indexpointers.Column{pathCol, minTimestampCol, maxTimestampCol},
		Predicates: []indexpointers.Predicate{
			indexpointers.WhereTimeRangeOverlapsWith(minTimestampCol, maxTimestampCol, t25s, t55s),
		},
	})
	t.Cleanup(func() { require.NoError(t, r.Close()) })

	expected := []indexpointers.IndexPointer{
		{Path: "path2", StartTs: t30.UTC(), EndTs: t40.UTC()},
		{Path: "path3", StartTs: t50.UTC(), EndTs: t60.UTC()},
	}
	require.Equal(t, expected, readPointers(t, r))
}

func TestReader_ReadRecordBatch(t *testing.T) {
	t.Run("returns an error when the reader is not open", func(t *testing.T) {
		sec := buildSection(t, []indexpointers.IndexPointer{
			{Path: "path1", StartTs: unixTime(10), EndTs: unixTime(20)},
		})
		r := indexpointers.NewReader(indexpointers.ReaderOptions{Columns: sec.Columns()})

		batch, err := r.ReadRecordBatch(t.Context(), newAllocator(t), 128)
		require.Nil(t, batch)
		require.EqualError(t, err, "reader not opened")
	})

	t.Run("returns an error when the batch size is not positive", func(t *testing.T) {
		sec := buildSection(t, []indexpointers.IndexPointer{
			{Path: "path1", StartTs: unixTime(10), EndTs: unixTime(20)},
		})
		r := indexpointers.NewReader(indexpointers.ReaderOptions{Columns: sec.Columns()})
		require.NoError(t, r.Open(t.Context()))
		t.Cleanup(func() { require.NoError(t, r.Close()) })

		alloc := newAllocator(t)
		for _, batchSize := range []int{0, -1} {
			batch, err := r.ReadRecordBatch(t.Context(), alloc, batchSize)
			require.Nil(t, batch)
			require.EqualError(t, err, fmt.Sprintf("reading record batch: batch size must be positive, got %d", batchSize))
		}
	})

	t.Run("returns an error when the allocator is nil", func(t *testing.T) {
		sec := buildSection(t, []indexpointers.IndexPointer{
			{Path: "path1", StartTs: unixTime(10), EndTs: unixTime(20)},
		})
		r := indexpointers.NewReader(indexpointers.ReaderOptions{Columns: sec.Columns()})
		require.NoError(t, r.Open(t.Context()))
		t.Cleanup(func() { require.NoError(t, r.Close()) })

		batch, err := r.ReadRecordBatch(t.Context(), nil, 128)
		require.Nil(t, batch)
		require.EqualError(t, err, "reading record batch: allocator is nil")
	})

	t.Run("returns every row in batches of one to the batch size rows until the section ends", func(t *testing.T) {
		const (
			rows      = 5
			batchSize = 2
		)
		pointers := make([]indexpointers.IndexPointer, rows)
		for i := range pointers {
			pointers[i] = indexpointers.IndexPointer{Path: fmt.Sprintf("path%d", i), StartTs: unixTime(int64(20 * i)), EndTs: unixTime(int64(20*i + 10))}
		}
		sec := buildSection(t, pointers)
		r := indexpointers.NewReader(indexpointers.ReaderOptions{Columns: sec.Columns()})
		require.NoError(t, r.Open(t.Context()))
		t.Cleanup(func() { require.NoError(t, r.Close()) })

		var total int
		alloc := newAllocator(t)
		for total < rows {
			alloc.Reclaim()
			batch, err := r.ReadRecordBatch(t.Context(), alloc, batchSize)
			require.NoError(t, err)
			require.GreaterOrEqual(t, batch.Rows, 1)
			require.LessOrEqual(t, batch.Rows, batchSize)
			total += batch.Rows
		}
		require.Equal(t, rows, total)

		batch, err := r.ReadRecordBatch(t.Context(), alloc, batchSize)
		require.Nil(t, batch)
		require.Equal(t, io.EOF, err)
	})

	t.Run("returns short batches of the matching rows and skips a batch with no match, when a function predicate reads every page", func(t *testing.T) {
		sec := buildSection(t, []indexpointers.IndexPointer{
			{Path: "path1", StartTs: unixTime(10), EndTs: unixTime(20)},
			{Path: "path2", StartTs: unixTime(30), EndTs: unixTime(40)},
			{Path: "path3", StartTs: unixTime(50), EndTs: unixTime(60)},
			{Path: "path4", StartTs: unixTime(70), EndTs: unixTime(80)},
			{Path: "path5", StartTs: unixTime(90), EndTs: unixTime(100)},
			{Path: "path6", StartTs: unixTime(110), EndTs: unixTime(120)},
		})
		pathCol := sec.Columns()[0]

		r := indexpointers.NewReader(indexpointers.ReaderOptions{
			Columns: []*indexpointers.Column{pathCol},
			Predicates: []indexpointers.Predicate{
				indexpointers.FuncPredicate{
					Column: pathCol,
					Keep: func(_ *indexpointers.Column, value scalar.Scalar) bool {
						return scalar.Equals(value, scalar.NewStringScalar("path2")) || scalar.Equals(value, scalar.NewStringScalar("path6"))
					},
				},
			},
		})
		require.NoError(t, r.Open(t.Context()))
		t.Cleanup(func() { require.NoError(t, r.Close()) })

		alloc := newAllocator(t)
		for _, want := range []string{"path2", "path6"} {
			alloc.Reclaim()
			batch, err := r.ReadRecordBatch(t.Context(), alloc, 2)
			require.NoError(t, err)
			require.Equal(t, 1, batch.Rows)
			require.Equal(t, want, string(batch.Path.Get(0)))
		}

		alloc.Reclaim()
		batch, err := r.ReadRecordBatch(t.Context(), alloc, 2)
		require.Nil(t, batch)
		require.Equal(t, io.EOF, err)
	})

	t.Run("returns nil and io.EOF at the end of the section, and on every later call", func(t *testing.T) {
		sec := buildSection(t, []indexpointers.IndexPointer{
			{Path: "path1", StartTs: unixTime(10), EndTs: unixTime(20)},
		})
		r := indexpointers.NewReader(indexpointers.ReaderOptions{Columns: sec.Columns()})
		require.NoError(t, r.Open(t.Context()))
		t.Cleanup(func() { require.NoError(t, r.Close()) })

		alloc := newAllocator(t)
		batch, err := r.ReadRecordBatch(t.Context(), alloc, 128)
		require.NoError(t, err)
		require.Equal(t, 1, batch.Rows)

		for range 3 {
			alloc.Reclaim()
			batch, err = r.ReadRecordBatch(t.Context(), alloc, 128)
			require.Nil(t, batch)
			require.Equal(t, io.EOF, err)
		}
	})
}

func TestReader_Reset(t *testing.T) {
	t.Run("reads the rows of the new section after Reset", func(t *testing.T) {
		first := buildSection(t, []indexpointers.IndexPointer{
			{Path: "first1", StartTs: unixTime(10), EndTs: unixTime(20)},
			{Path: "first2", StartTs: unixTime(30), EndTs: unixTime(40)},
		})
		second := buildSection(t, []indexpointers.IndexPointer{
			{Path: "second1", StartTs: unixTime(50), EndTs: unixTime(60)},
		})

		r := indexpointers.NewReader(indexpointers.ReaderOptions{Columns: first.Columns()[:1]})
		t.Cleanup(func() { require.NoError(t, r.Close()) })

		require.Equal(t, []indexpointers.IndexPointer{{Path: "first1"}, {Path: "first2"}}, readPointers(t, r))

		r.Reset(indexpointers.ReaderOptions{Columns: second.Columns()[:1]})
		require.Equal(t, []indexpointers.IndexPointer{{Path: "second1"}}, readPointers(t, r))
	})

	t.Run("maps the columns that Reset got when the caller changes its slice after Open", func(t *testing.T) {
		sec := buildSection(t, []indexpointers.IndexPointer{
			{Path: "path1", StartTs: unixTime(10), EndTs: unixTime(20)},
		})
		columns := []*indexpointers.Column{sec.Columns()[1], sec.Columns()[2]}

		r := indexpointers.NewReader(indexpointers.ReaderOptions{Columns: columns})
		require.NoError(t, r.Open(t.Context()))
		t.Cleanup(func() { require.NoError(t, r.Close()) })
		columns[0], columns[1] = columns[1], columns[0]

		batch, err := r.ReadRecordBatch(t.Context(), newAllocator(t), 128)
		require.NoError(t, err)
		require.Equal(t, unixTime(10).UnixNano(), batch.MinTimestamp.Get(0))
		require.Equal(t, unixTime(20).UnixNano(), batch.MaxTimestamp.Get(0))
	})
}

func buildSection(t *testing.T, ptrData []indexpointers.IndexPointer) *indexpointers.Section {
	t.Helper()

	sectionBuilder := indexpointers.NewBuilder(nil, 0, 2)

	for _, ptr := range ptrData {
		sectionBuilder.Append(ptr)
	}

	objectBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objectBuilder.Append(sectionBuilder))

	obj, closer, err := objectBuilder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })

	sec, err := indexpointers.Open(t.Context(), obj.Sections()[0])
	require.NoError(t, err)
	return sec
}

// readPointers opens r and returns the path and the time range of every row. A
// field is zero when r does not project its column.
func readPointers(t *testing.T, r *indexpointers.Reader) []indexpointers.IndexPointer {
	t.Helper()

	require.NoError(t, r.Open(t.Context()))

	const maxBatches = 100
	var (
		pointers []indexpointers.IndexPointer
		alloc    = newAllocator(t)
	)
	for range maxBatches {
		alloc.Reclaim()
		batch, err := r.ReadRecordBatch(t.Context(), alloc, 128)
		if errors.Is(err, io.EOF) {
			return pointers
		}
		require.NoError(t, err)

		for i := range batch.Rows {
			var pointer indexpointers.IndexPointer
			if batch.Path != nil {
				pointer.Path = string(batch.Path.Get(i))
			}
			if batch.MinTimestamp != nil {
				pointer.StartTs = time.Unix(0, batch.MinTimestamp.Get(i)).UTC()
			}
			if batch.MaxTimestamp != nil {
				pointer.EndTs = time.Unix(0, batch.MaxTimestamp.Get(i)).UTC()
			}
			pointers = append(pointers, pointer)
		}
	}
	require.FailNow(t, "the reader did not reach the end of the section")
	return nil
}

// newAllocator returns an allocator that the test frees at its end.
func newAllocator(t *testing.T) *memory.Allocator {
	t.Helper()

	alloc := memory.NewAllocator(nil)
	t.Cleanup(alloc.Free)
	return alloc
}
