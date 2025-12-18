package metastore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
)

func TestCollectSections_StopsOnEOFAndAggregates(t *testing.T) {
	t.Parallel()

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "path.path.utf8", Type: arrow.BinaryTypes.String},
		{Name: "section.int64", Type: arrow.PrimitiveTypes.Int64},
		{Name: "stream_id.int64", Type: arrow.PrimitiveTypes.Int64},
		{Name: "stream_id_ref.int64", Type: arrow.PrimitiveTypes.Int64},
		{Name: "min_timestamp.timestamp", Type: arrow.FixedWidthTypes.Timestamp_ns},
		{Name: "max_timestamp.timestamp", Type: arrow.FixedWidthTypes.Timestamp_ns},
		{Name: "row_count.int64", Type: arrow.PrimitiveTypes.Int64},
		{Name: "uncompressed_size.int64", Type: arrow.PrimitiveTypes.Int64},
	}, nil)

	makeRec := func(path string, section int64, streamIDRef int64, start, end time.Time, rows, size int64) arrow.RecordBatch {
		pathB := array.NewStringBuilder(memory.DefaultAllocator)
		sectionB := array.NewInt64Builder(memory.DefaultAllocator)
		streamIDB := array.NewInt64Builder(memory.DefaultAllocator)
		streamIDRefB := array.NewInt64Builder(memory.DefaultAllocator)
		minTsB := array.NewTimestampBuilder(memory.DefaultAllocator, arrow.FixedWidthTypes.Timestamp_ns.(*arrow.TimestampType))
		maxTsB := array.NewTimestampBuilder(memory.DefaultAllocator, arrow.FixedWidthTypes.Timestamp_ns.(*arrow.TimestampType))
		rowCountB := array.NewInt64Builder(memory.DefaultAllocator)
		sizeB := array.NewInt64Builder(memory.DefaultAllocator)

		pathB.AppendValues([]string{path}, nil)
		sectionB.AppendValues([]int64{section}, nil)
		streamIDB.AppendValues([]int64{1}, nil)
		streamIDRefB.AppendValues([]int64{streamIDRef}, nil)
		minTsB.AppendValues([]arrow.Timestamp{arrow.Timestamp(start.UnixNano())}, nil)
		maxTsB.AppendValues([]arrow.Timestamp{arrow.Timestamp(end.UnixNano())}, nil)
		rowCountB.AppendValues([]int64{rows}, nil)
		sizeB.AppendValues([]int64{size}, nil)

		cols := []arrow.Array{
			pathB.NewArray(),
			sectionB.NewArray(),
			streamIDB.NewArray(),
			streamIDRefB.NewArray(),
			minTsB.NewArray(),
			maxTsB.NewArray(),
			rowCountB.NewArray(),
			sizeB.NewArray(),
		}

		rec := array.NewRecordBatch(schema, cols, 1)
		return rec
	}

	t0 := time.Now()
	rec1 := makeRec("obj-A", 7, 10, t0, t0.Add(time.Minute), 3, 100)
	rec2 := makeRec("obj-A", 7, 11, t0.Add(-time.Minute), t0.Add(2*time.Minute), 5, 250)
	empty := array.NewRecordBatch(schema, []arrow.Array{
		array.NewStringBuilder(memory.DefaultAllocator).NewArray(),
		array.NewInt64Builder(memory.DefaultAllocator).NewArray(),
		array.NewInt64Builder(memory.DefaultAllocator).NewArray(),
		array.NewInt64Builder(memory.DefaultAllocator).NewArray(),
		array.NewTimestampBuilder(memory.DefaultAllocator, arrow.FixedWidthTypes.Timestamp_ns.(*arrow.TimestampType)).NewArray(),
		array.NewTimestampBuilder(memory.DefaultAllocator, arrow.FixedWidthTypes.Timestamp_ns.(*arrow.TimestampType)).NewArray(),
		array.NewInt64Builder(memory.DefaultAllocator).NewArray(),
		array.NewInt64Builder(memory.DefaultAllocator).NewArray(),
	}, 0)

	reader := &sliceRecordBatchReader{recs: []arrow.RecordBatch{empty, rec1, rec2}}
	m := newTestObjectMetastore(objstore.NewInMemBucket())

	resp, err := m.CollectSections(context.Background(), CollectSectionsRequest{Reader: reader})
	require.NoError(t, err)
	require.Len(t, resp.SectionsResponse.Sections, 1)
	desc := resp.SectionsResponse.Sections[0]
	require.Equal(t, "obj-A", desc.ObjectPath)
	require.Equal(t, int64(7), desc.SectionIdx)
	require.ElementsMatch(t, []int64{10, 11}, desc.StreamIDs)
	require.Equal(t, 8, desc.RowCount)
	require.Equal(t, int64(350), desc.Size)
}

func TestCollectSections_PropagatesReaderError(t *testing.T) {
	t.Parallel()

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "path.path.utf8", Type: arrow.BinaryTypes.String},
		{Name: "section.int64", Type: arrow.PrimitiveTypes.Int64},
	}, nil)

	pathB := array.NewStringBuilder(memory.DefaultAllocator)
	secB := array.NewInt64Builder(memory.DefaultAllocator)
	pathB.AppendValues([]string{"a"}, nil)
	secB.AppendValues([]int64{1}, nil)
	cols := []arrow.Array{pathB.NewArray(), secB.NewArray()}
	rec := array.NewRecordBatch(schema, cols, 1)

	readErr := errors.New("boom")
	reader := &sliceRecordBatchReader{recs: []arrow.RecordBatch{rec}, errs: []error{readErr}}
	m := newTestObjectMetastore(objstore.NewInMemBucket())

	_, err := m.CollectSections(context.Background(), CollectSectionsRequest{Reader: reader})
	require.ErrorIs(t, err, readErr)
}

func TestIndexSectionsReader_RequiresSelector(t *testing.T) {
	t.Parallel()

	m := newTestObjectMetastore(objstore.NewInMemBucket())
	_, err := m.IndexSectionsReader(context.Background(), IndexSectionsReaderRequest{
		IndexPath: "does-not-matter",
		SectionsRequest: SectionsRequest{
			Start:    time.Now().Add(-time.Hour),
			End:      time.Now(),
			Matchers: nil,
		},
	})
	require.Error(t, err)
}
