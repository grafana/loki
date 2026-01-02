package indexpointers_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
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
		expected arrowtest.Rows
	}{
		{
			name:    "basic reads with selected columns",
			columns: []*indexpointers.Column{pathCol},
			expected: arrowtest.Rows{
				{"path.path.utf8": "path1"},
				{"path.path.utf8": "path2"},
				{"path.path.utf8": "path3"},
			},
		},
		{
			name:    "basic reads with all columns",
			columns: []*indexpointers.Column{pathCol, minTimestampCol, maxTimestampCol},
			expected: arrowtest.Rows{
				{"path.path.utf8": "path1", "min_timestamp.min_timestamp.timestamp": unixTime(10).UTC(), "max_timestamp.max_timestamp.timestamp": unixTime(20).UTC()},
				{"path.path.utf8": "path2", "min_timestamp.min_timestamp.timestamp": unixTime(30).UTC(), "max_timestamp.max_timestamp.timestamp": unixTime(40).UTC()},
				{"path.path.utf8": "path3", "min_timestamp.min_timestamp.timestamp": unixTime(50).UTC(), "max_timestamp.max_timestamp.timestamp": unixTime(60).UTC()},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := indexpointers.NewReader(indexpointers.ReaderOptions{
				Columns:    tt.columns,
				Allocator:  memory.DefaultAllocator,
				Predicates: nil,
			})

			actualTable, err := readTable(context.Background(), r)
			require.NoError(t, err)

			actual, err := arrowtest.TableRows(memory.DefaultAllocator, actualTable)
			require.NoError(t, err, "failed to get rows from table")
			require.Equal(t, tt.expected, actual)
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
		Columns:   []*indexpointers.Column{pathCol, minTimestampCol, maxTimestampCol},
		Allocator: memory.DefaultAllocator,
		Predicates: []indexpointers.Predicate{
			indexpointers.WhereTimeRangeOverlapsWith(minTimestampCol, maxTimestampCol, t25s, t55s),
		},
	})

	actualTable, err := readTable(context.Background(), r)
	require.NoError(t, err)

	actual, err := arrowtest.TableRows(memory.DefaultAllocator, actualTable)
	require.NoError(t, err)

	expected := arrowtest.Rows{
		{
			"path.path.utf8":                        "path2",
			"min_timestamp.min_timestamp.timestamp": t30.UTC(),
			"max_timestamp.max_timestamp.timestamp": t40.UTC(),
		},
		{
			"path.path.utf8":                        "path3",
			"min_timestamp.min_timestamp.timestamp": t50.UTC(),
			"max_timestamp.max_timestamp.timestamp": t60.UTC(),
		},
	}
	require.Equal(t, expected, actual)
}

func buildSection(t *testing.T, ptrData []indexpointers.IndexPointer) *indexpointers.Section {
	t.Helper()

	sectionBuilder := indexpointers.NewBuilder(nil, 0, 2)

	for _, ptr := range ptrData {
		sectionBuilder.Append(ptr.Path, ptr.StartTs, ptr.EndTs)
	}

	objectBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objectBuilder.Append(sectionBuilder))

	obj, closer, err := objectBuilder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { closer.Close() })

	sec, err := indexpointers.Open(t.Context(), obj.Sections()[0])
	require.NoError(t, err)
	return sec
}

func readTable(ctx context.Context, r *indexpointers.Reader) (arrow.Table, error) {
	var recs []arrow.RecordBatch

	for {
		rec, err := r.Read(ctx, 128)
		if rec != nil {
			if rec.NumRows() > 0 {
				recs = append(recs, rec)
			}
		}

		if err != nil && errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, err
		}
	}

	if len(recs) == 0 {
		return nil, io.EOF
	}

	return array.NewTableFromRecords(recs[0].Schema(), recs), nil
}
