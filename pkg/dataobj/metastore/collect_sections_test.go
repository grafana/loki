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

func TestCollectSections(t *testing.T) {
	t.Parallel()

	t.Run("aggregates the rows of a section across batches and stops on EOF", func(t *testing.T) {
		makeRec := func(path string, section int64, streamIDRef int64, start, end time.Time, rows, size int64) arrow.RecordBatch {
			pathB := array.NewStringBuilder(memory.DefaultAllocator)
			sectionB := array.NewInt64Builder(memory.DefaultAllocator)
			streamIDB := array.NewInt64Builder(memory.DefaultAllocator)
			streamIDRefB := array.NewInt64Builder(memory.DefaultAllocator)
			minTsB := array.NewTimestampBuilder(memory.DefaultAllocator, arrow.FixedWidthTypes.Timestamp_ns.(*arrow.TimestampType))
			maxTsB := array.NewTimestampBuilder(memory.DefaultAllocator, arrow.FixedWidthTypes.Timestamp_ns.(*arrow.TimestampType))
			rowCountB := array.NewInt64Builder(memory.DefaultAllocator)
			sizeB := array.NewInt64Builder(memory.DefaultAllocator)
			internalLabelsB := array.NewStringBuilder(memory.DefaultAllocator)

			pathB.AppendValues([]string{path}, nil)
			sectionB.AppendValues([]int64{section}, nil)
			streamIDB.AppendValues([]int64{1}, nil)
			streamIDRefB.AppendValues([]int64{streamIDRef}, nil)
			minTsB.AppendValues([]arrow.Timestamp{arrow.Timestamp(start.UnixNano())}, nil)
			maxTsB.AppendValues([]arrow.Timestamp{arrow.Timestamp(end.UnixNano())}, nil)
			rowCountB.AppendValues([]int64{rows}, nil)
			sizeB.AppendValues([]int64{size}, nil)
			internalLabelsB.AppendValues([]string{"label1,label2"}, nil)

			cols := []arrow.Array{
				pathB.NewArray(),
				sectionB.NewArray(),
				streamIDB.NewArray(),
				streamIDRefB.NewArray(),
				minTsB.NewArray(),
				maxTsB.NewArray(),
				rowCountB.NewArray(),
				sizeB.NewArray(),
				internalLabelsB.NewArray(),
			}

			rec := array.NewRecordBatch(postingsResultSchema, cols, 1)
			return rec
		}

		t0 := time.Now()
		rec1 := makeRec("obj-A", 7, 10, t0, t0.Add(time.Minute), 3, 100)
		rec2 := makeRec("obj-A", 7, 11, t0.Add(-time.Minute), t0.Add(2*time.Minute), 5, 250)
		empty := array.NewRecordBatch(postingsResultSchema, []arrow.Array{
			array.NewStringBuilder(memory.DefaultAllocator).NewArray(),
			array.NewInt64Builder(memory.DefaultAllocator).NewArray(),
			array.NewInt64Builder(memory.DefaultAllocator).NewArray(),
			array.NewInt64Builder(memory.DefaultAllocator).NewArray(),
			array.NewTimestampBuilder(memory.DefaultAllocator, arrow.FixedWidthTypes.Timestamp_ns.(*arrow.TimestampType)).NewArray(),
			array.NewTimestampBuilder(memory.DefaultAllocator, arrow.FixedWidthTypes.Timestamp_ns.(*arrow.TimestampType)).NewArray(),
			array.NewInt64Builder(memory.DefaultAllocator).NewArray(),
			array.NewInt64Builder(memory.DefaultAllocator).NewArray(),
			array.NewStringBuilder(memory.DefaultAllocator).NewArray(),
		}, 0)

		reader := &sliceRecordBatchReader{recs: []arrow.RecordBatch{empty, rec1, rec2}}
		m := newTestObjectMetastore(objstore.NewInMemBucket())

		resp, err := m.CollectSections(t.Context(), CollectSectionsRequest{Reader: reader})
		require.NoError(t, err)
		require.Len(t, resp.SectionsResponse.Sections, 1)
		desc := resp.SectionsResponse.Sections[0]
		require.Equal(t, "obj-A", desc.ObjectPath)
		require.Equal(t, int64(7), desc.SectionIdx)
		require.ElementsMatch(t, []int64{10, 11}, desc.StreamIDs)
		require.Equal(t, 8, desc.RowCount)
		require.Equal(t, int64(350), desc.Size)
		require.ElementsMatch(t, []string{"label1", "label2"}, desc.AmbiguousPredicates)
	})

	t.Run("returns the reader error", func(t *testing.T) {
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

		_, err := m.CollectSections(t.Context(), CollectSectionsRequest{Reader: reader})
		require.ErrorIs(t, err, readErr)
	})

	t.Run("returns an error when a batch has another schema", func(t *testing.T) {
		pathB := array.NewStringBuilder(memory.DefaultAllocator)
		pathB.Append("obj-A")
		rec := array.NewRecordBatch(
			arrow.NewSchema([]arrow.Field{{Name: "path.path.utf8", Type: arrow.BinaryTypes.String}}, nil),
			[]arrow.Array{pathB.NewArray()},
			1,
		)

		m := newTestObjectMetastore(objstore.NewInMemBucket())
		_, err := m.CollectSections(t.Context(), CollectSectionsRequest{Reader: &sliceRecordBatchReader{recs: []arrow.RecordBatch{rec}}})
		require.EqualError(t, err, "decode resolved rows: schema does not match postingsResultSchema")
	})
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
