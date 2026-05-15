package pointers_test

import (
	"bytes"
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
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

// TestReader does a basic end-to-end test over a reader with a predicate applied.
func TestReader(t *testing.T) {
	sec := buildSection(t, []pointers.SectionPointer{
		{Path: "path1", Section: 1, PointerKind: pointers.PointerKindStreamIndex, StreamID: 10, StreamIDRef: 100, StartTs: unixTime(10), EndTs: unixTime(20), LineCount: 5, UncompressedSize: 1024},
		{Path: "path2", Section: 2, PointerKind: pointers.PointerKindStreamIndex, StreamID: 20, StreamIDRef: 200, StartTs: unixTime(30), EndTs: unixTime(40), LineCount: 10, UncompressedSize: 2048},
		{Path: "path3", Section: 3, PointerKind: pointers.PointerKindStreamIndex, StreamID: 30, StreamIDRef: 300, StartTs: unixTime(50), EndTs: unixTime(60), LineCount: 15, UncompressedSize: 3072},
		{Path: "path1", Section: 1, PointerKind: pointers.PointerKindColumnIndex, ColumnName: "col1", ColumnIndex: 0, ValuesBloomFilter: []byte{1, 2, 3}},
		{Path: "path2", Section: 2, PointerKind: pointers.PointerKindColumnIndex, ColumnName: "col2", ColumnIndex: 1, ValuesBloomFilter: []byte{4, 5, 6}},
	})

	var (
		pathCol         = sec.Columns()[0]
		sectionCol      = sec.Columns()[1]
		pointerKindCol  = sec.Columns()[2]
		streamIDCol     = sec.Columns()[3]
		streamIDRefCol  = sec.Columns()[4]
		minTimestampCol = sec.Columns()[5]
		maxTimestampCol = sec.Columns()[6]
		rowCountCol     = sec.Columns()[7]
		uncompressedCol = sec.Columns()[8]
		columnNameCol   = sec.Columns()[9]
		columnIndexCol  = sec.Columns()[10]
		valuesBloomCol  = sec.Columns()[11]
	)

	require.Equal(t, "path", pathCol.Name)
	require.Equal(t, pointers.ColumnTypePath, pathCol.Type)
	require.Equal(t, "", sectionCol.Name)
	require.Equal(t, pointers.ColumnTypeSection, sectionCol.Type)
	require.Equal(t, "", pointerKindCol.Name)
	require.Equal(t, pointers.ColumnTypePointerKind, pointerKindCol.Type)
	require.Equal(t, "", streamIDCol.Name)
	require.Equal(t, pointers.ColumnTypeStreamID, streamIDCol.Type)
	require.Equal(t, "", streamIDRefCol.Name)
	require.Equal(t, pointers.ColumnTypeStreamIDRef, streamIDRefCol.Type)
	require.Equal(t, "", minTimestampCol.Name)
	require.Equal(t, pointers.ColumnTypeMinTimestamp, minTimestampCol.Type)
	require.Equal(t, "", maxTimestampCol.Name)
	require.Equal(t, pointers.ColumnTypeMaxTimestamp, maxTimestampCol.Type)
	require.Equal(t, "", rowCountCol.Name)
	require.Equal(t, pointers.ColumnTypeRowCount, rowCountCol.Type)
	require.Equal(t, "", uncompressedCol.Name)
	require.Equal(t, pointers.ColumnTypeUncompressedSize, uncompressedCol.Type)
	require.Equal(t, "column_name", columnNameCol.Name)
	require.Equal(t, pointers.ColumnTypeColumnName, columnNameCol.Type)
	require.Equal(t, "", columnIndexCol.Name)
	require.Equal(t, pointers.ColumnTypeColumnIndex, columnIndexCol.Type)
	require.Equal(t, "values_bloom_filter", valuesBloomCol.Name)
	require.Equal(t, pointers.ColumnTypeValuesBloomFilter, valuesBloomCol.Type)

	for _, tt := range []struct {
		name     string
		columns  []*pointers.Column
		expected arrowtest.Rows
	}{
		{
			name:    "basic reads with predicate",
			columns: []*pointers.Column{pathCol, sectionCol, pointerKindCol, streamIDCol, streamIDRefCol},
			expected: arrowtest.Rows{
				{"path.path.utf8": "path1", "section.int64": int64(1), "pointer_kind.int64": int64(pointers.PointerKindStreamIndex), "stream_id.int64": int64(10), "stream_id_ref.int64": int64(100), pointers.InternalLabelsFieldName: nil},
				{"path.path.utf8": "path2", "section.int64": int64(2), "pointer_kind.int64": int64(pointers.PointerKindStreamIndex), "stream_id.int64": int64(20), "stream_id_ref.int64": int64(200), pointers.InternalLabelsFieldName: nil},
			},
		},
		// tests that the reader evaluates predicates correctly even when only some columns are selected for output
		{
			name:    "reads with subset of columns",
			columns: []*pointers.Column{pathCol, sectionCol, pointerKindCol, streamIDCol},
			expected: arrowtest.Rows{
				{"path.path.utf8": "path1", "section.int64": int64(1), "pointer_kind.int64": int64(pointers.PointerKindStreamIndex), "stream_id.int64": int64(10), pointers.InternalLabelsFieldName: nil},
				{"path.path.utf8": "path2", "section.int64": int64(2), "pointer_kind.int64": int64(pointers.PointerKindStreamIndex), "stream_id.int64": int64(20), pointers.InternalLabelsFieldName: nil},
			},
		},
		// tests reading all columns
		{
			name: "read all columns for stream pointers",
			columns: []*pointers.Column{
				pathCol, sectionCol, pointerKindCol, streamIDCol, streamIDRefCol,
				minTimestampCol, maxTimestampCol, rowCountCol, uncompressedCol,
			},
			expected: arrowtest.Rows{
				{
					"path.path.utf8":                 "path1",
					"section.int64":                  int64(1),
					"pointer_kind.int64":             int64(pointers.PointerKindStreamIndex),
					"stream_id.int64":                int64(10),
					"stream_id_ref.int64":            int64(100),
					"min_timestamp.timestamp":        unixTime(10).UTC(),
					"max_timestamp.timestamp":        unixTime(20).UTC(),
					"row_count.int64":                int64(2),
					"uncompressed_size.int64":        int64(1024),
					pointers.InternalLabelsFieldName: nil,
				},
				{
					"path.path.utf8":                 "path2",
					"section.int64":                  int64(2),
					"pointer_kind.int64":             int64(pointers.PointerKindStreamIndex),
					"stream_id.int64":                int64(20),
					"stream_id_ref.int64":            int64(200),
					"min_timestamp.timestamp":        unixTime(30).UTC(),
					"max_timestamp.timestamp":        unixTime(40).UTC(),
					"row_count.int64":                int64(2),
					"uncompressed_size.int64":        int64(2048),
					pointers.InternalLabelsFieldName: nil,
				},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := pointers.NewReader(pointers.ReaderOptions{
				Columns:   tt.columns,
				Allocator: memory.DefaultAllocator,
				Predicates: []pointers.Predicate{
					pointers.InPredicate{
						Column: streamIDCol,
						Values: []scalar.Scalar{
							scalar.NewInt64Scalar(10),
							scalar.NewInt64Scalar(20),
						},
					},
					pointers.EqualPredicate{
						Column: pointerKindCol,
						Value:  scalar.NewInt64Scalar(int64(pointers.PointerKindStreamIndex)),
					},
				},
			})

			actualTable, err := readTable(context.Background(), r)
			require.NoError(t, err)

			actual, err := arrowtest.TableRows(memory.DefaultAllocator, actualTable)
			require.NoError(t, err, "failed to get rows from table")
			require.Equal(t, tt.expected, actual)
		})
	}
}

func TestReader_ReadBeforeOpen(t *testing.T) {
	sec := buildSection(t, []pointers.SectionPointer{
		{Path: "path1", Section: 1, PointerKind: pointers.PointerKindStreamIndex, StreamID: 10, StreamIDRef: 100, StartTs: unixTime(10), EndTs: unixTime(20), LineCount: 5, UncompressedSize: 1024},
	})

	r := pointers.NewReader(pointers.ReaderOptions{
		Columns:   sec.Columns(),
		Allocator: memory.DefaultAllocator,
	})

	rec, err := r.Read(context.Background(), 128)
	require.Nil(t, rec)
	require.ErrorContains(t, err, "reader not opened")
}

// TestReaderWithEqualPredicate tests reading with an EqualPredicate.
func TestReaderWithEqualPredicate(t *testing.T) {
	sec := buildSection(t, []pointers.SectionPointer{
		{Path: "path1", Section: 1, PointerKind: pointers.PointerKindStreamIndex, StreamID: 10, StreamIDRef: 100, StartTs: unixTime(10), EndTs: unixTime(20), LineCount: 5, UncompressedSize: 1024},
		{Path: "path2", Section: 2, PointerKind: pointers.PointerKindStreamIndex, StreamID: 20, StreamIDRef: 200, StartTs: unixTime(30), EndTs: unixTime(40), LineCount: 10, UncompressedSize: 2048},
		{Path: "path3", Section: 3, PointerKind: pointers.PointerKindStreamIndex, StreamID: 30, StreamIDRef: 300, StartTs: unixTime(50), EndTs: unixTime(60), LineCount: 15, UncompressedSize: 3072},
	})

	var (
		pathCol     = sec.Columns()[0]
		sectionCol  = sec.Columns()[1]
		streamIDCol = sec.Columns()[3]
	)

	r := pointers.NewReader(pointers.ReaderOptions{
		Columns:   []*pointers.Column{pathCol, sectionCol, streamIDCol},
		Allocator: memory.DefaultAllocator,
		Predicates: []pointers.Predicate{
			pointers.EqualPredicate{
				Column: streamIDCol,
				Value:  scalar.NewInt64Scalar(20),
			},
		},
	})

	actualTable, err := readTable(context.Background(), r)
	require.NoError(t, err)

	actual, err := arrowtest.TableRows(memory.DefaultAllocator, actualTable)
	require.NoError(t, err)

	expected := arrowtest.Rows{
		{"path.path.utf8": "path2", "section.int64": int64(2), "stream_id.int64": int64(20), pointers.InternalLabelsFieldName: nil},
	}
	require.Equal(t, expected, actual)
}

// TestReaderWithInPredicate tests reading with an InPredicate.
func TestReaderWithInPredicate(t *testing.T) {
	sec := buildSection(t, []pointers.SectionPointer{
		{Path: "path1", Section: 1, PointerKind: pointers.PointerKindStreamIndex, StreamID: 10, StreamIDRef: 100, StartTs: unixTime(10), EndTs: unixTime(20), LineCount: 5, UncompressedSize: 1024},
		{Path: "path2", Section: 2, PointerKind: pointers.PointerKindStreamIndex, StreamID: 20, StreamIDRef: 200, StartTs: unixTime(30), EndTs: unixTime(40), LineCount: 10, UncompressedSize: 2048},
		{Path: "path3", Section: 3, PointerKind: pointers.PointerKindStreamIndex, StreamID: 30, StreamIDRef: 300, StartTs: unixTime(50), EndTs: unixTime(60), LineCount: 15, UncompressedSize: 3072},
		{Path: "path4", Section: 4, PointerKind: pointers.PointerKindStreamIndex, StreamID: 40, StreamIDRef: 400, StartTs: unixTime(70), EndTs: unixTime(80), LineCount: 20, UncompressedSize: 4096},
	})

	var (
		pathCol     = sec.Columns()[0]
		sectionCol  = sec.Columns()[1]
		streamIDCol = sec.Columns()[3]
	)

	r := pointers.NewReader(pointers.ReaderOptions{
		Columns:   []*pointers.Column{pathCol, sectionCol, streamIDCol},
		Allocator: memory.DefaultAllocator,
		Predicates: []pointers.Predicate{
			pointers.InPredicate{
				Column: streamIDCol,
				Values: []scalar.Scalar{
					scalar.NewInt64Scalar(10),
					scalar.NewInt64Scalar(30),
				},
			},
		},
	})

	actualTable, err := readTable(context.Background(), r)
	require.NoError(t, err)

	actual, err := arrowtest.TableRows(memory.DefaultAllocator, actualTable)
	require.NoError(t, err)

	expected := arrowtest.Rows{
		{"path.path.utf8": "path1", "section.int64": int64(1), "stream_id.int64": int64(10), pointers.InternalLabelsFieldName: nil},
		{"path.path.utf8": "path3", "section.int64": int64(3), "stream_id.int64": int64(30), pointers.InternalLabelsFieldName: nil},
	}
	require.Equal(t, expected, actual)
}

// TestReaderWithGreaterThanPredicate tests reading with a GreaterThanPredicate.
func TestReaderWithGreaterThanPredicate(t *testing.T) {
	sec := buildSection(t, []pointers.SectionPointer{
		{Path: "path1", Section: 1, PointerKind: pointers.PointerKindStreamIndex, StreamID: 10, StreamIDRef: 100, StartTs: unixTime(10), EndTs: unixTime(20), LineCount: 5, UncompressedSize: 1024},
		{Path: "path2", Section: 2, PointerKind: pointers.PointerKindStreamIndex, StreamID: 20, StreamIDRef: 200, StartTs: unixTime(30), EndTs: unixTime(40), LineCount: 10, UncompressedSize: 2048},
		{Path: "path3", Section: 3, PointerKind: pointers.PointerKindStreamIndex, StreamID: 30, StreamIDRef: 300, StartTs: unixTime(50), EndTs: unixTime(60), LineCount: 15, UncompressedSize: 3072},
	})

	var (
		pathCol     = sec.Columns()[0]
		sectionCol  = sec.Columns()[1]
		streamIDCol = sec.Columns()[3]
	)

	r := pointers.NewReader(pointers.ReaderOptions{
		Columns:   []*pointers.Column{pathCol, sectionCol, streamIDCol},
		Allocator: memory.DefaultAllocator,
		Predicates: []pointers.Predicate{
			pointers.GreaterThanPredicate{
				Column: streamIDCol,
				Value:  scalar.NewInt64Scalar(15),
			},
		},
	})

	actualTable, err := readTable(context.Background(), r)
	require.NoError(t, err)

	actual, err := arrowtest.TableRows(memory.DefaultAllocator, actualTable)
	require.NoError(t, err)

	expected := arrowtest.Rows{
		{"path.path.utf8": "path2", "section.int64": int64(2), "stream_id.int64": int64(20), pointers.InternalLabelsFieldName: nil},
		{"path.path.utf8": "path3", "section.int64": int64(3), "stream_id.int64": int64(30), pointers.InternalLabelsFieldName: nil},
	}
	require.Equal(t, expected, actual)
}

// TestReaderWithLessThanPredicate tests reading with a LessThanPredicate.
func TestReaderWithLessThanPredicate(t *testing.T) {
	sec := buildSection(t, []pointers.SectionPointer{
		{Path: "path1", Section: 1, PointerKind: pointers.PointerKindStreamIndex, StreamID: 10, StreamIDRef: 100, StartTs: unixTime(10), EndTs: unixTime(20), LineCount: 5, UncompressedSize: 1024},
		{Path: "path2", Section: 2, PointerKind: pointers.PointerKindStreamIndex, StreamID: 20, StreamIDRef: 200, StartTs: unixTime(30), EndTs: unixTime(40), LineCount: 10, UncompressedSize: 2048},
		{Path: "path3", Section: 3, PointerKind: pointers.PointerKindStreamIndex, StreamID: 30, StreamIDRef: 300, StartTs: unixTime(50), EndTs: unixTime(60), LineCount: 15, UncompressedSize: 3072},
	})

	var (
		pathCol     = sec.Columns()[0]
		sectionCol  = sec.Columns()[1]
		streamIDCol = sec.Columns()[3]
	)

	r := pointers.NewReader(pointers.ReaderOptions{
		Columns:   []*pointers.Column{pathCol, sectionCol, streamIDCol},
		Allocator: memory.DefaultAllocator,
		Predicates: []pointers.Predicate{
			pointers.LessThanPredicate{
				Column: streamIDCol,
				Value:  scalar.NewInt64Scalar(25),
			},
		},
	})

	actualTable, err := readTable(context.Background(), r)
	require.NoError(t, err)

	actual, err := arrowtest.TableRows(memory.DefaultAllocator, actualTable)
	require.NoError(t, err)

	expected := arrowtest.Rows{
		{"path.path.utf8": "path1", "section.int64": int64(1), "stream_id.int64": int64(10), pointers.InternalLabelsFieldName: nil},
		{"path.path.utf8": "path2", "section.int64": int64(2), "stream_id.int64": int64(20), pointers.InternalLabelsFieldName: nil},
	}
	require.Equal(t, expected, actual)
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
	sec := buildSection(t, []pointers.SectionPointer{
		{Path: "path1", Section: 1, PointerKind: pointers.PointerKindStreamIndex, StreamID: 10, StreamIDRef: 100, StartTs: t10, EndTs: t20, LineCount: 5, UncompressedSize: 1024},
		{Path: "path2", Section: 2, PointerKind: pointers.PointerKindStreamIndex, StreamID: 20, StreamIDRef: 200, StartTs: t30, EndTs: t40, LineCount: 10, UncompressedSize: 2048},
		{Path: "path3", Section: 3, PointerKind: pointers.PointerKindStreamIndex, StreamID: 30, StreamIDRef: 300, StartTs: t50, EndTs: t60, LineCount: 15, UncompressedSize: 3072},
	})

	var (
		pathCol         = sec.Columns()[0]
		sectionCol      = sec.Columns()[1]
		minTimestampCol = sec.Columns()[5]
		maxTimestampCol = sec.Columns()[6]
	)

	r := pointers.NewReader(pointers.ReaderOptions{
		Columns:   []*pointers.Column{pathCol, sectionCol, minTimestampCol, maxTimestampCol},
		Allocator: memory.DefaultAllocator,
		Predicates: []pointers.Predicate{
			pointers.WhereTimeRangeOverlapsWith(minTimestampCol, maxTimestampCol, t25s, t55s),
		},
	})

	actualTable, err := readTable(context.Background(), r)
	require.NoError(t, err)

	actual, err := arrowtest.TableRows(memory.DefaultAllocator, actualTable)
	require.NoError(t, err)

	expected := arrowtest.Rows{
		{
			"path.path.utf8":          "path2",
			"section.int64":           int64(2),
			"min_timestamp.timestamp": t30.UTC(),
			"max_timestamp.timestamp": t40.UTC(),
		},
		{
			"path.path.utf8":          "path3",
			"section.int64":           int64(3),
			"min_timestamp.timestamp": t50.UTC(),
			"max_timestamp.timestamp": t60.UTC(),
		},
	}
	require.Equal(t, expected, actual)
}

// TestReaderWithFuncPredicate tests reading with a FuncPredicate.
func TestReaderWithFuncPredicate(t *testing.T) {
	sec := buildSection(t, []pointers.SectionPointer{
		{Path: "path1", Section: 1, PointerKind: pointers.PointerKindStreamIndex, StreamID: 10, StreamIDRef: 100, StartTs: unixTime(10), EndTs: unixTime(20), LineCount: 5, UncompressedSize: 1024},
		{Path: "path2", Section: 2, PointerKind: pointers.PointerKindStreamIndex, StreamID: 20, StreamIDRef: 200, StartTs: unixTime(30), EndTs: unixTime(40), LineCount: 10, UncompressedSize: 2048},
		{Path: "path3", Section: 3, PointerKind: pointers.PointerKindStreamIndex, StreamID: 30, StreamIDRef: 300, StartTs: unixTime(50), EndTs: unixTime(60), LineCount: 15, UncompressedSize: 3072},
	})

	var (
		pathCol    = sec.Columns()[0]
		sectionCol = sec.Columns()[1]
	)

	r := pointers.NewReader(pointers.ReaderOptions{
		Columns:   []*pointers.Column{pathCol, sectionCol},
		Allocator: memory.DefaultAllocator,
		Predicates: []pointers.Predicate{
			pointers.FuncPredicate{
				Column: pathCol,
				Keep: func(_ *pointers.Column, value scalar.Scalar) bool {
					if !value.IsValid() {
						return false
					}

					bb := value.(*scalar.String).Value.Bytes()
					return bytes.Equal(bb, []byte("path1")) || bytes.Equal(bb, []byte("path3"))
				},
			},
		},
	})

	actualTable, err := readTable(context.Background(), r)
	require.NoError(t, err)

	actual, err := arrowtest.TableRows(memory.DefaultAllocator, actualTable)
	require.NoError(t, err)

	expected := arrowtest.Rows{
		{"path.path.utf8": "path1", "section.int64": int64(1)},
		{"path.path.utf8": "path3", "section.int64": int64(3)},
	}
	require.Equal(t, expected, actual)
}

// TestReaderWithAndPredicate tests reading with an AndPredicate.
func TestReaderWithAndPredicate(t *testing.T) {
	sec := buildSection(t, []pointers.SectionPointer{
		{Path: "path1", Section: 1, PointerKind: pointers.PointerKindStreamIndex, StreamID: 10, StreamIDRef: 100, StartTs: unixTime(10), EndTs: unixTime(20), LineCount: 5, UncompressedSize: 1024},
		{Path: "path2", Section: 2, PointerKind: pointers.PointerKindStreamIndex, StreamID: 20, StreamIDRef: 200, StartTs: unixTime(30), EndTs: unixTime(40), LineCount: 10, UncompressedSize: 2048},
		{Path: "path3", Section: 3, PointerKind: pointers.PointerKindStreamIndex, StreamID: 30, StreamIDRef: 300, StartTs: unixTime(50), EndTs: unixTime(60), LineCount: 15, UncompressedSize: 3072},
	})

	var (
		pathCol     = sec.Columns()[0]
		sectionCol  = sec.Columns()[1]
		streamIDCol = sec.Columns()[3]
	)

	r := pointers.NewReader(pointers.ReaderOptions{
		Columns:   []*pointers.Column{pathCol, sectionCol, streamIDCol},
		Allocator: memory.DefaultAllocator,
		Predicates: []pointers.Predicate{
			pointers.AndPredicate{
				Left: pointers.GreaterThanPredicate{
					Column: streamIDCol,
					Value:  scalar.NewInt64Scalar(5),
				},
				Right: pointers.LessThanPredicate{
					Column: streamIDCol,
					Value:  scalar.NewInt64Scalar(25),
				},
			},
		},
	})

	actualTable, err := readTable(context.Background(), r)
	require.NoError(t, err)

	actual, err := arrowtest.TableRows(memory.DefaultAllocator, actualTable)
	require.NoError(t, err)

	expected := arrowtest.Rows{
		{"path.path.utf8": "path1", "section.int64": int64(1), "stream_id.int64": int64(10), pointers.InternalLabelsFieldName: nil},
		{"path.path.utf8": "path2", "section.int64": int64(2), "stream_id.int64": int64(20), pointers.InternalLabelsFieldName: nil},
	}
	require.Equal(t, expected, actual)
}

// TestReaderWithOrPredicate tests reading with an OrPredicate.
func TestReaderWithOrPredicate(t *testing.T) {
	sec := buildSection(t, []pointers.SectionPointer{
		{Path: "path1", Section: 1, PointerKind: pointers.PointerKindStreamIndex, StreamID: 10, StreamIDRef: 100, StartTs: unixTime(10), EndTs: unixTime(20), LineCount: 5, UncompressedSize: 1024},
		{Path: "path2", Section: 2, PointerKind: pointers.PointerKindStreamIndex, StreamID: 20, StreamIDRef: 200, StartTs: unixTime(30), EndTs: unixTime(40), LineCount: 10, UncompressedSize: 2048},
		{Path: "path3", Section: 3, PointerKind: pointers.PointerKindStreamIndex, StreamID: 30, StreamIDRef: 300, StartTs: unixTime(50), EndTs: unixTime(60), LineCount: 15, UncompressedSize: 3072},
	})

	var (
		pathCol     = sec.Columns()[0]
		sectionCol  = sec.Columns()[1]
		streamIDCol = sec.Columns()[3]
	)

	r := pointers.NewReader(pointers.ReaderOptions{
		Columns:   []*pointers.Column{pathCol, sectionCol, streamIDCol},
		Allocator: memory.DefaultAllocator,
		Predicates: []pointers.Predicate{
			pointers.OrPredicate{
				Left: pointers.EqualPredicate{
					Column: streamIDCol,
					Value:  scalar.NewInt64Scalar(10),
				},
				Right: pointers.EqualPredicate{
					Column: streamIDCol,
					Value:  scalar.NewInt64Scalar(30),
				},
			},
		},
	})

	actualTable, err := readTable(context.Background(), r)
	require.NoError(t, err)

	actual, err := arrowtest.TableRows(memory.DefaultAllocator, actualTable)
	require.NoError(t, err)

	expected := arrowtest.Rows{
		{"path.path.utf8": "path1", "section.int64": int64(1), "stream_id.int64": int64(10), pointers.InternalLabelsFieldName: nil},
		{"path.path.utf8": "path3", "section.int64": int64(3), "stream_id.int64": int64(30), pointers.InternalLabelsFieldName: nil},
	}
	require.Equal(t, expected, actual)
}

// TestReaderWithNotPredicate tests reading with a NotPredicate.
func TestReaderWithNotPredicate(t *testing.T) {
	sec := buildSection(t, []pointers.SectionPointer{
		{Path: "path1", Section: 1, PointerKind: pointers.PointerKindStreamIndex, StreamID: 10, StreamIDRef: 100, StartTs: unixTime(10), EndTs: unixTime(20), LineCount: 5, UncompressedSize: 1024},
		{Path: "path2", Section: 2, PointerKind: pointers.PointerKindStreamIndex, StreamID: 20, StreamIDRef: 200, StartTs: unixTime(30), EndTs: unixTime(40), LineCount: 10, UncompressedSize: 2048},
		{Path: "path3", Section: 3, PointerKind: pointers.PointerKindStreamIndex, StreamID: 30, StreamIDRef: 300, StartTs: unixTime(50), EndTs: unixTime(60), LineCount: 15, UncompressedSize: 3072},
	})

	var (
		pathCol     = sec.Columns()[0]
		sectionCol  = sec.Columns()[1]
		streamIDCol = sec.Columns()[3]
	)

	r := pointers.NewReader(pointers.ReaderOptions{
		Columns:   []*pointers.Column{pathCol, sectionCol, streamIDCol},
		Allocator: memory.DefaultAllocator,
		Predicates: []pointers.Predicate{
			pointers.NotPredicate{
				Inner: pointers.EqualPredicate{
					Column: streamIDCol,
					Value:  scalar.NewInt64Scalar(20),
				},
			},
		},
	})

	actualTable, err := readTable(context.Background(), r)
	require.NoError(t, err)

	actual, err := arrowtest.TableRows(memory.DefaultAllocator, actualTable)
	require.NoError(t, err)

	expected := arrowtest.Rows{
		{"path.path.utf8": "path1", "section.int64": int64(1), "stream_id.int64": int64(10), pointers.InternalLabelsFieldName: nil},
		{"path.path.utf8": "path3", "section.int64": int64(3), "stream_id.int64": int64(30), pointers.InternalLabelsFieldName: nil},
	}
	require.Equal(t, expected, actual)
}

// TestReaderWithColumnIndexPointers tests reading column index pointers.
func TestReaderWithColumnIndexPointers(t *testing.T) {
	sec := buildSection(t, []pointers.SectionPointer{
		{Path: "path1", Section: 1, PointerKind: pointers.PointerKindColumnIndex, ColumnName: "col1", ColumnIndex: 0, ValuesBloomFilter: []byte{1, 2, 3}},
		{Path: "path2", Section: 2, PointerKind: pointers.PointerKindColumnIndex, ColumnName: "col2", ColumnIndex: 1, ValuesBloomFilter: []byte{4, 5, 6}},
		{Path: "path3", Section: 3, PointerKind: pointers.PointerKindColumnIndex, ColumnName: "col3", ColumnIndex: 2, ValuesBloomFilter: []byte{7, 8, 9}},
	})

	cols := sec.Columns()
	require.Len(t, cols, 6, "expected 6 columns for column index pointers")

	var (
		pathCol        = cols[0]
		sectionCol     = cols[1]
		pointerKindCol = cols[2]
		columnNameCol  = cols[3]
		columnIndexCol = cols[4]
		valuesBloomCol = cols[5]
	)

	r := pointers.NewReader(pointers.ReaderOptions{
		Columns: []*pointers.Column{
			pathCol, sectionCol, pointerKindCol, columnNameCol, columnIndexCol, valuesBloomCol,
		},
		Allocator: memory.DefaultAllocator,
		Predicates: []pointers.Predicate{
			pointers.EqualPredicate{
				Column: pointerKindCol,
				Value:  scalar.NewInt64Scalar(int64(pointers.PointerKindColumnIndex)),
			},
		},
	})

	actualTable, err := readTable(context.Background(), r)
	require.NoError(t, err)

	actual, err := arrowtest.TableRows(memory.DefaultAllocator, actualTable)
	require.NoError(t, err)

	expected := arrowtest.Rows{
		{
			"path.path.utf8":                                 "path1",
			"section.int64":                                  int64(1),
			"pointer_kind.int64":                             int64(pointers.PointerKindColumnIndex),
			"column_name.column_name.utf8":                   "col1",
			"column_index.int64":                             int64(0),
			"values_bloom_filter.values_bloom_filter.binary": []byte{1, 2, 3},
		},
		{
			"path.path.utf8":                                 "path2",
			"section.int64":                                  int64(2),
			"pointer_kind.int64":                             int64(pointers.PointerKindColumnIndex),
			"column_name.column_name.utf8":                   "col2",
			"column_index.int64":                             int64(1),
			"values_bloom_filter.values_bloom_filter.binary": []byte{4, 5, 6},
		},
		{
			"path.path.utf8":                                 "path3",
			"section.int64":                                  int64(3),
			"pointer_kind.int64":                             int64(pointers.PointerKindColumnIndex),
			"column_name.column_name.utf8":                   "col3",
			"column_index.int64":                             int64(2),
			"values_bloom_filter.values_bloom_filter.binary": []byte{7, 8, 9},
		},
	}
	require.Equal(t, expected, actual)
}

// TestReaderWithMixedPointers tests reading both stream and column index pointers.
func TestReaderWithMixedPointers(t *testing.T) {
	sec := buildSection(t, []pointers.SectionPointer{
		{Path: "path1", Section: 1, PointerKind: pointers.PointerKindStreamIndex, StreamID: 10, StreamIDRef: 100, StartTs: unixTime(10), EndTs: unixTime(20), LineCount: 5, UncompressedSize: 1024},
		{Path: "path2", Section: 2, PointerKind: pointers.PointerKindColumnIndex, ColumnName: "col1", ColumnIndex: 0, ValuesBloomFilter: []byte{1, 2, 3}},
		{Path: "path3", Section: 3, PointerKind: pointers.PointerKindStreamIndex, StreamID: 20, StreamIDRef: 200, StartTs: unixTime(30), EndTs: unixTime(40), LineCount: 10, UncompressedSize: 2048},
	})

	var (
		pathCol        = sec.Columns()[0]
		sectionCol     = sec.Columns()[1]
		pointerKindCol = sec.Columns()[2]
	)

	// Read all pointer kinds without filtering
	r := pointers.NewReader(pointers.ReaderOptions{
		Columns:    []*pointers.Column{pathCol, sectionCol, pointerKindCol},
		Allocator:  memory.DefaultAllocator,
		Predicates: []pointers.Predicate{},
	})

	actualTable, err := readTable(context.Background(), r)
	require.NoError(t, err)

	actual, err := arrowtest.TableRows(memory.DefaultAllocator, actualTable)
	require.NoError(t, err)

	expected := arrowtest.Rows{
		{"path.path.utf8": "path1", "section.int64": int64(1), "pointer_kind.int64": int64(pointers.PointerKindStreamIndex)},
		{"path.path.utf8": "path3", "section.int64": int64(3), "pointer_kind.int64": int64(pointers.PointerKindStreamIndex)},
		{"path.path.utf8": "path2", "section.int64": int64(2), "pointer_kind.int64": int64(pointers.PointerKindColumnIndex)},
	}
	require.Equal(t, expected, actual)
}

// TestReaderPopulatesInternalLabelsFieldWhenGivenStreamMetadata tests that the reader populates the internal labels field when given stream metadata.
func TestReaderPopulatesInternalLabelsFieldWhenGivenStreamMetadata(t *testing.T) {
	sec := buildSection(t, []pointers.SectionPointer{
		{Path: "path1", Section: 1, PointerKind: pointers.PointerKindStreamIndex, StreamID: 10, StreamIDRef: 100, StartTs: unixTime(10), EndTs: unixTime(20), LineCount: 5, UncompressedSize: 1024},
		{Path: "path2", Section: 2, PointerKind: pointers.PointerKindStreamIndex, StreamID: 20, StreamIDRef: 200, StartTs: unixTime(30), EndTs: unixTime(40), LineCount: 10, UncompressedSize: 2048},
		{Path: "path3", Section: 3, PointerKind: pointers.PointerKindStreamIndex, StreamID: 30, StreamIDRef: 300, StartTs: unixTime(50), EndTs: unixTime(60), LineCount: 15, UncompressedSize: 3072},
	})

	var (
		pathCol     = sec.Columns()[0]
		sectionCol  = sec.Columns()[1]
		streamIDCol = sec.Columns()[3]
	)

	r := pointers.NewReader(pointers.ReaderOptions{
		Columns:   []*pointers.Column{pathCol, sectionCol, streamIDCol},
		Allocator: memory.DefaultAllocator,
		StreamIDToLabelNames: map[int64][]string{
			10: {"label1"},
			20: {"label2"},
			30: {"label3"},
		},
	})

	actualTable, err := readTable(context.Background(), r)
	require.NoError(t, err)

	actual, err := arrowtest.TableRows(memory.DefaultAllocator, actualTable)
	require.NoError(t, err)

	expected := arrowtest.Rows{
		{"path.path.utf8": "path1", "section.int64": int64(1), "stream_id.int64": int64(10), pointers.InternalLabelsFieldName: "label1"},
		{"path.path.utf8": "path2", "section.int64": int64(2), "stream_id.int64": int64(20), pointers.InternalLabelsFieldName: "label2"},
		{"path.path.utf8": "path3", "section.int64": int64(3), "stream_id.int64": int64(30), pointers.InternalLabelsFieldName: "label3"},
	}
	require.Equal(t, expected, actual)
}

func buildSection(t *testing.T, ptrData []pointers.SectionPointer) *pointers.Section {
	t.Helper()

	sectionBuilder := pointers.NewBuilder(nil, 0, 2)

	for _, ptr := range ptrData {
		if ptr.PointerKind == pointers.PointerKindStreamIndex {
			sectionBuilder.ObserveStream(ptr.Path, ptr.Section, ptr.StreamIDRef, ptr.StreamID, ptr.StartTs, ptr.UncompressedSize)
			sectionBuilder.ObserveStream(ptr.Path, ptr.Section, ptr.StreamIDRef, ptr.StreamID, ptr.EndTs, 0)
		} else {
			sectionBuilder.RecordColumnIndex(ptr.Path, ptr.Section, ptr.ColumnName, ptr.ColumnIndex, ptr.ValuesBloomFilter)
		}
	}

	objectBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objectBuilder.Append(sectionBuilder))

	obj, closer, err := objectBuilder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { closer.Close() })

	sec, err := pointers.Open(t.Context(), obj.Sections()[0])
	require.NoError(t, err)
	return sec
}

func readTable(ctx context.Context, r *pointers.Reader) (arrow.Table, error) {
	var recs []arrow.RecordBatch
	if err := r.Open(ctx); err != nil {
		return nil, err
	}

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
