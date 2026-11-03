package dataset

import (
	"bytes"
	"testing"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
)

func TestBatchReader_ScatteredSelection(t *testing.T) {
	// The stream ID changes on every row, so a predicate on it keeps rows scattered over every
	// window and every page, and some pages of the other columns hold no kept row.
	generator := DatasetGenerator{
		RowCount:     8000,
		PageSizeHint: 2048,
		Columns: []generatorColumnConfig{
			{
				Tag:               "stream",
				Type:              ColumnType{Physical: datasetmd.PHYSICAL_TYPE_INT64, Logical: "stream_id"},
				Encoding:          datasetmd.ENCODING_TYPE_DELTA,
				Compression:       datasetmd.COMPRESSION_TYPE_NONE,
				CardinalityTarget: 400,
				RunLength:         1,
			},
			{
				Tag:               "msg",
				Type:              ColumnType{Physical: datasetmd.PHYSICAL_TYPE_BINARY, Logical: "string"},
				Encoding:          datasetmd.ENCODING_TYPE_PLAIN,
				Compression:       datasetmd.COMPRESSION_TYPE_SNAPPY,
				AvgSize:           30,
				CardinalityTarget: 500,
			},
			{
				Tag:               "label",
				Type:              ColumnType{Physical: datasetmd.PHYSICAL_TYPE_BINARY, Logical: "label"},
				Encoding:          datasetmd.ENCODING_TYPE_PLAIN,
				Compression:       datasetmd.COMPRESSION_TYPE_NONE,
				AvgSize:           12,
				CardinalityTarget: 30,
				SparsityRate:      0.3,
			},
			{
				Tag:               "ts",
				Type:              ColumnType{Physical: datasetmd.PHYSICAL_TYPE_INT64, Logical: "number"},
				Encoding:          datasetmd.ENCODING_TYPE_DELTA,
				Compression:       datasetmd.COMPRESSION_TYPE_NONE,
				CardinalityTarget: 1_000_000,
			},
		},
	}
	dset, columns := generator.Build(t, 3)
	stream, msg, label, ts := columns[0], columns[1], columns[2], columns[3]

	needle := []byte("1x")
	containsNeedle := FuncPredicate{
		Column: msg,
		Keep:   func(_ Column, v Value) bool { return bytes.Contains(v.Binary(), needle) },
		KeepBatch: func(_ Column, values *columnar.UTF8, dst []int32) []int32 {
			for i := range values.Len() {
				if bytes.Contains(values.Get(i), needle) {
					dst = append(dst, int32(i))
				}
			}
			return dst
		},
	}

	for name, predicates := range map[string][]Predicate{
		"rare stream then line filter": {
			InPredicate{Column: stream, Values: NewInt64ValueSetOf(3, 77)},
			containsNeedle,
		},
		"rare stream then label then line filter": {
			InPredicate{Column: stream, Values: NewInt64ValueSetOf(5, 6, 7)},
			EqualPredicate{Column: label, Value: BinaryValue(nil)},
			containsNeedle,
		},
		"rare stream and a second condition on the same column": {
			InPredicate{Column: stream, Values: NewInt64ValueSetOf(9, 10)},
			AndPredicate{
				Left:  containsNeedle,
				Right: NotPredicate{Inner: EqualPredicate{Column: label, Value: BinaryValue([]byte("1"))}},
			},
		},
		"or reads a column for few rows, then a later predicate needs it for many": {
			OrPredicate{
				Left:  NotPredicate{Inner: EqualPredicate{Column: stream, Value: Int64Value(7)}},
				Right: EqualPredicate{Column: msg, Value: BinaryValue([]byte("no such line"))},
			},
			containsNeedle,
		},
		"or reads a column for few rows, then the column is projected": {
			OrPredicate{
				Left:  NotPredicate{Inner: EqualPredicate{Column: stream, Value: Int64Value(7)}},
				Right: EqualPredicate{Column: msg, Value: BinaryValue([]byte("no such line"))},
			},
		},
		"not of an and reads a column for few rows, then a later predicate needs it for many": {
			NotPredicate{Inner: AndPredicate{
				Left:  EqualPredicate{Column: stream, Value: Int64Value(7)},
				Right: EqualPredicate{Column: msg, Value: BinaryValue([]byte("no such line"))},
			}},
			containsNeedle,
		},
		"common stream set": {
			InPredicate{Column: stream, Values: NewInt64ValueSetOf(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20)},
			containsNeedle,
		},
	} {
		t.Run(name, func(t *testing.T) {
			for _, prefetch := range []bool{false, true} {
				requireSameRows(t, RowReaderOptions{Dataset: dset, ProjectedColumns: columns, Predicates: predicates, PrefetchAllOnOpen: prefetch})
				requireSameRows(t, RowReaderOptions{Dataset: dset, ProjectedColumns: []Column{ts, label}, Predicates: predicates, PrefetchAllOnOpen: prefetch})
			}
		})
	}
}
