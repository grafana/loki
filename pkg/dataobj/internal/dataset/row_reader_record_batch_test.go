package dataset

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"slices"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/memory"
)

// recordBatchTestSizes covers a record batch of one row, record batches that
// split pages, and a record batch that holds the whole dataset.
var recordBatchTestSizes = []int{1, 3, 7, 64, 10_000}

// errInjectedReadPages is the error that failingAtDataset returns.
var errInjectedReadPages = errors.New("failed to read pages")

// parityRowStats are the row statistics that Read and ReadRecordBatch must record
// alike. parityByteStats are the byte statistics. ReadRecordBatch can record fewer
// bytes than Read, never more.
var (
	parityRowStats = []string{
		dataobj.StatDatasetMaxRows.Name(),
		dataobj.StatDatasetRowsAfterPruning.Name(),
		dataobj.StatDatasetPrimaryRowsRead.Name(),
		dataobj.StatDatasetSecondaryRowsRead.Name(),
		dataobj.StatStreamRelevantRows.Name(),
	}
	parityByteStats = []string{
		dataobj.StatDatasetPrimaryRowBytes.Name(),
		dataobj.StatDatasetSecondaryRowBytes.Name(),
	}
)

// parityStats lists the row and byte statistics together. A retry after an error
// must not change them, and requireSameStats compares them for equality.
var parityStats = slices.Concat(parityRowStats, parityByteStats)

func TestRowReader_ReadRecordBatch(t *testing.T) {
	t.Run("returns the same rows as Read when there are no predicates", func(t *testing.T) {
		dset, columns := buildTestDataset(t)
		requireSameRows(t, RowReaderOptions{Dataset: dset, ProjectedColumns: columns})
	})

	t.Run("returns the projected columns in the order of the projection", func(t *testing.T) {
		dset, columns := buildTestDataset(t)
		requireSameRows(t, RowReaderOptions{Dataset: dset, ProjectedColumns: []Column{columns[3], columns[0]}})
	})

	t.Run("ends a record batch at the end of a page when a column has large pages that hold few rows", func(t *testing.T) {
		dset, columns := buildLongLineDataset(t, 4000, 1000, 512<<10)
		pageRows := pageRowCounts(t, columns[2])
		require.Greater(t, len(pageRows), 2)

		got := recordBatchSizes(t, RowReaderOptions{Dataset: dset, ProjectedColumns: columns, PrefetchAllOnOpen: true}, 1024)

		require.Equal(t, pageRows, got)
	})

	t.Run("ends a record batch at the end of a page of a column that only a predicate reads", func(t *testing.T) {
		dset, columns := buildLongLineDataset(t, 4000, 1000, 512<<10)
		keepAll := FuncPredicate{Column: columns[2], Keep: func(Column, Value) bool { return true }}

		got := recordBatchSizes(t, RowReaderOptions{Dataset: dset, ProjectedColumns: columns[:2], Predicates: []Predicate{keepAll}, PrefetchAllOnOpen: true}, 1024)

		require.Equal(t, pageRowCounts(t, columns[2]), got)
	})

	t.Run("does not end a record batch at the end of a page when the pages of the column are small", func(t *testing.T) {
		dset, columns := buildLongLineDataset(t, 4000, 1000, 16<<10)

		got := recordBatchSizes(t, RowReaderOptions{Dataset: dset, ProjectedColumns: columns, PrefetchAllOnOpen: true}, 1024)

		require.Equal(t, fullBatchSizes(4000, 1024), got)
	})

	t.Run("does not end a record batch at the end of a page when the pages hold more than two record batches of rows", func(t *testing.T) {
		dset, columns := buildLongLineDataset(t, 30000, 40, 512<<10)

		got := recordBatchSizes(t, RowReaderOptions{Dataset: dset, ProjectedColumns: columns, PrefetchAllOnOpen: true}, 1024)

		require.Equal(t, fullBatchSizes(30000, 1024), got)
	})

	t.Run("does not end a record batch at the end of a page when the pages download lazily", func(t *testing.T) {
		dset, columns := buildLongLineDataset(t, 4000, 1000, 512<<10)

		got := recordBatchSizes(t, RowReaderOptions{Dataset: dset, ProjectedColumns: columns, PrefetchAllOnOpen: false}, 1024)

		require.Equal(t, fullBatchSizes(4000, 1024), got)
	})

	t.Run("finds the columns that limit a record batch again when the batch size changes", func(t *testing.T) {
		dset, columns := buildLongLineDataset(t, 4000, 1000, 512<<10)
		pageRows := pageRowCounts(t, columns[2])
		r := NewRowReader(RowReaderOptions{Dataset: dset, ProjectedColumns: columns, PrefetchAllOnOpen: true})
		t.Cleanup(func() { require.NoError(t, r.Close()) })
		require.NoError(t, r.Open(t.Context()))

		var alloc memory.Allocator
		first, err := r.ReadRecordBatch(t.Context(), &alloc, 64)
		require.NoError(t, err)
		second, err := r.ReadRecordBatch(t.Context(), &alloc, 1024)
		require.NoError(t, err)

		require.Equal(t, int64(64), first.NumRows())
		require.Equal(t, int64(pageRows[0]-64), second.NumRows())
	})

	t.Run("returns the same rows as Read when a record batch ends at the end of a page", func(t *testing.T) {
		dset, columns := buildLongLineDataset(t, 4000, 1000, 512<<10)
		stream, ts := columns[0], columns[1]

		for name, predicates := range map[string][]Predicate{
			"without predicates":         nil,
			"with a selective predicate": {InPredicate{Column: stream, Values: NewInt64ValueSetOf(3, 77)}},
			"with an and of two comparisons": {AndPredicate{
				Left:  GreaterThanPredicate{Column: ts, Value: Int64Value(20_000)},
				Right: LessThanPredicate{Column: ts, Value: Int64Value(80_000)},
			}},
			"with a line filter": {containsNeedle(columns[2], "e")},
		} {
			t.Run(name, func(t *testing.T) {
				opts := RowReaderOptions{Dataset: dset, ProjectedColumns: columns, Predicates: predicates, PrefetchAllOnOpen: true}

				for _, batchSize := range []int{300, 1024, 10_000} {
					want := readAllRowsAsText(t, opts, batchSize)
					got := readAllRecordBatches(t.Context(), t, opts, batchSize)
					require.Equal(t, want, got, "batch size %d", batchSize)
				}
			})
		}
	})

	t.Run("returns the same rows as Read for the predicates of the people dataset", func(t *testing.T) {
		dset, columns := buildTestDataset(t)
		first, middle, last, year := columns[0], columns[1], columns[2], columns[3]

		keepLongerThan := func(n int) FuncPredicate {
			return FuncPredicate{
				Column: first,
				Keep:   func(_ Column, v Value) bool { return len(v.Binary()) > n },
				KeepBatch: func(_ Column, values *columnar.UTF8, dst []int32) []int32 {
					for i := range values.Len() {
						if len(values.Get(i)) > n {
							dst = append(dst, int32(i))
						}
					}
					return dst
				},
			}
		}

		for name, predicates := range map[string][]Predicate{
			"an equality on a string column":                     {EqualPredicate{Column: first, Value: BinaryValue([]byte("Alice"))}},
			"an equality with an empty string":                   {EqualPredicate{Column: middle, Value: BinaryValue(nil)}},
			"an equality with a string that is not empty":        {EqualPredicate{Column: middle, Value: BinaryValue([]byte("Marie"))}},
			"an equality with a nil value":                       {EqualPredicate{Column: middle, Value: Value{}}},
			"a greater than on an int64 column":                  {GreaterThanPredicate{Column: year, Value: Int64Value(1983)}},
			"a less than on an int64 column":                     {LessThanPredicate{Column: year, Value: Int64Value(1983)}},
			"a less than on a string column":                     {LessThanPredicate{Column: last, Value: BinaryValue([]byte("Dz"))}},
			"an in on an int64 column":                           {InPredicate{Column: year, Values: NewInt64ValueSetOf(1980, 1990, 1)}},
			"an in on an int64 column with a memoized set":       {InPredicate{Column: year, Values: NewMemoizedInt64ValueSetOf(1980, 1990, 1)}},
			"an in on a string column":                           {InPredicate{Column: first, Values: NewBinaryValueSet([]Value{BinaryValue([]byte("Eve")), BinaryValue([]byte("Bob"))})}},
			"a false constant":                                   {FalsePredicate{}},
			"a not on a comparison":                              {NotPredicate{Inner: GreaterThanPredicate{Column: year, Value: Int64Value(1983)}}},
			"a not on a nil predicate":                           {NotPredicate{Inner: nil}},
			"a not on a true constant":                           {NotPredicate{Inner: TruePredicate{}}},
			"an or of a false constant and a comparison":         {OrPredicate{Left: FalsePredicate{}, Right: GreaterThanPredicate{Column: year, Value: Int64Value(1983)}}},
			"a not on an or of a true constant and a comparison": {NotPredicate{Inner: OrPredicate{Left: TruePredicate{}, Right: GreaterThanPredicate{Column: year, Value: Int64Value(1983)}}}},
			"an and of two comparisons on one column": {AndPredicate{
				Left:  GreaterThanPredicate{Column: year, Value: Int64Value(1979)},
				Right: LessThanPredicate{Column: year, Value: Int64Value(1988)},
			}},
			"an or of two columns": {OrPredicate{
				Left:  EqualPredicate{Column: first, Value: BinaryValue([]byte("John"))},
				Right: EqualPredicate{Column: year, Value: Int64Value(1987)},
			}},
			"a greater than or equal made of an or": {OrPredicate{
				Left:  GreaterThanPredicate{Column: year, Value: Int64Value(1983)},
				Right: EqualPredicate{Column: year, Value: Int64Value(1983)},
			}},
			"a less than or equal made of an or": {OrPredicate{
				Left:  EqualPredicate{Column: year, Value: Int64Value(1983)},
				Right: LessThanPredicate{Column: year, Value: Int64Value(1983)},
			}},
			"a less than or equal on a string column": {OrPredicate{
				Left:  LessThanPredicate{Column: middle, Value: BinaryValue([]byte("M"))},
				Right: EqualPredicate{Column: middle, Value: BinaryValue([]byte("M"))},
			}},
			"a greater than or equal predicate on an int64 column":   {GreaterThanOrEqualPredicate{Column: year, Value: Int64Value(1983)}},
			"a less than or equal predicate on an int64 column":      {LessThanOrEqualPredicate{Column: year, Value: Int64Value(1983)}},
			"a greater than or equal predicate on a string column":   {GreaterThanOrEqualPredicate{Column: first, Value: BinaryValue([]byte("Eve"))}},
			"a less than or equal predicate on a string column":      {LessThanOrEqualPredicate{Column: first, Value: BinaryValue([]byte("Eve"))}},
			"a greater than or equal predicate with an empty string": {GreaterThanOrEqualPredicate{Column: middle, Value: BinaryValue(nil)}},
			"a less than or equal predicate with an empty string":    {LessThanOrEqualPredicate{Column: middle, Value: BinaryValue(nil)}},
			"a not on a greater than or equal predicate":             {NotPredicate{Inner: GreaterThanOrEqualPredicate{Column: year, Value: Int64Value(1983)}}},
			"a comparison and then a less than or equal predicate": {
				GreaterThanPredicate{Column: year, Value: Int64Value(1979)},
				LessThanOrEqualPredicate{Column: year, Value: Int64Value(1987)},
			},
			"a not on an or": {NotPredicate{Inner: OrPredicate{
				Left:  EqualPredicate{Column: first, Value: BinaryValue([]byte("John"))},
				Right: GreaterThanPredicate{Column: year, Value: Int64Value(1985)},
			}}},
			"two predicates on two columns": {
				GreaterThanPredicate{Column: year, Value: Int64Value(1979)},
				LessThanPredicate{Column: last, Value: BinaryValue([]byte("M"))},
			},
			"a func predicate with Keep only": {FuncPredicate{
				Column: first,
				Keep:   func(_ Column, v Value) bool { return bytes.ContainsAny(v.Binary(), "ae") },
			}},
			"a func predicate with KeepBatch": {keepLongerThan(3)},
			"a func predicate on a column of empty strings": {FuncPredicate{
				Column: middle,
				Keep:   func(_ Column, v Value) bool { return v.IsNil() || len(v.Binary()) > 4 },
			}},
			"a comparison and then a func predicate with KeepBatch": {
				GreaterThanPredicate{Column: year, Value: Int64Value(1983)},
				keepLongerThan(3),
			},
			"a comparison and then a func predicate with Keep only": {
				GreaterThanPredicate{Column: year, Value: Int64Value(1983)},
				FuncPredicate{Column: first, Keep: func(_ Column, v Value) bool { return len(v.Binary()) > 3 }},
			},
			"a comparison and then an in on an int64 column": {
				GreaterThanPredicate{Column: year, Value: Int64Value(1983)},
				InPredicate{Column: year, Values: NewInt64ValueSetOf(1980, 1985, 1990)},
			},
			"a comparison and then an in on a string column": {
				GreaterThanPredicate{Column: year, Value: Int64Value(1983)},
				InPredicate{Column: first, Values: NewBinaryValueSet([]Value{BinaryValue([]byte("Eve")), BinaryValue([]byte("Bob")), BinaryValue([]byte("David"))})},
			},
			"a comparison and then an equality with a nil value": {
				GreaterThanPredicate{Column: year, Value: Int64Value(1983)},
				EqualPredicate{Column: middle, Value: Value{}},
			},
			"a comparison and then a not on a comparison": {
				GreaterThanPredicate{Column: year, Value: Int64Value(1983)},
				NotPredicate{Inner: EqualPredicate{Column: first, Value: BinaryValue([]byte("Alice"))}},
			},
		} {
			t.Run(name, func(t *testing.T) {
				requireSameRows(t, RowReaderOptions{Dataset: dset, ProjectedColumns: columns, Predicates: predicates})
				requireSameRows(t, RowReaderOptions{Dataset: dset, ProjectedColumns: []Column{columns[0]}, Predicates: predicates})
				requireSameRows(t, RowReaderOptions{Dataset: dset, ProjectedColumns: []Column{columns[2], columns[3]}, Predicates: predicates})
			})
		}
	})

	t.Run("keeps every row for a predicate that reads no column and keeps every row", func(t *testing.T) {
		dset, columns := buildTestDataset(t)
		want := readAllRowsAsText(t, RowReaderOptions{Dataset: dset, ProjectedColumns: columns}, 3)

		for _, p := range []Predicate{TruePredicate{}, nil, NotPredicate{Inner: FalsePredicate{}}} {
			got := readAllRecordBatches(t.Context(), t, RowReaderOptions{Dataset: dset, ProjectedColumns: columns, Predicates: []Predicate{p}}, 3)
			require.Equal(t, want, got)
		}
	})

	t.Run("returns the same rows as Read when predicates read columns that are not projected", func(t *testing.T) {
		dset, columns := buildTestDataset(t)
		requireSameRows(t, RowReaderOptions{
			Dataset:          dset,
			ProjectedColumns: []Column{columns[0]},
			Predicates: []Predicate{
				GreaterThanPredicate{Column: columns[3], Value: Int64Value(1980)},
				EqualPredicate{Column: columns[1], Value: BinaryValue(nil)},
			},
		})
	})

	t.Run("returns the same rows as Read for the predicates of a generated dataset with nulls and many pages", func(t *testing.T) {
		generator := DatasetGenerator{RowCount: 5000, PageSizeHint: 4096, Columns: generatedColumns(40, 50)}
		dset, columns := generator.Build(t, 1)
		stream, ts, msg, label, count := columns[0], columns[1], columns[2], columns[3], columns[4]
		labelValue := firstBinaryValue(t, dset, label)

		for name, predicates := range map[string][]Predicate{
			"an in on the stream ID with a memoized set": {InPredicate{Column: stream, Values: NewMemoizedInt64ValueSetOf(3, 7, 11, 39)}},
			"a stream ID in and then a time range": {
				InPredicate{Column: stream, Values: NewInt64ValueSetOf(3, 7, 11, 39)},
				AndPredicate{
					Left:  OrPredicate{Left: GreaterThanPredicate{Column: ts, Value: Int64Value(20_000)}, Right: EqualPredicate{Column: ts, Value: Int64Value(20_000)}},
					Right: LessThanPredicate{Column: ts, Value: Int64Value(80_000)},
				},
			},
			"a line filter": {containsNeedle(msg, "7x")},
			"a selective predicate and a line filter":                                {InPredicate{Column: stream, Values: NewInt64ValueSetOf(5)}, containsNeedle(msg, "7x")},
			"a line filter and then a label equality":                                {containsNeedle(msg, "7x"), EqualPredicate{Column: label, Value: labelValue}},
			"an equality with an empty string on a nullable string column":           {EqualPredicate{Column: label, Value: BinaryValue(nil)}},
			"a less than on a nullable string column":                                {LessThanPredicate{Column: label, Value: BinaryValue([]byte("5"))}},
			"a less than an empty string on a nullable string column":                {LessThanPredicate{Column: label, Value: BinaryValue(nil)}},
			"a greater than on a nullable string column":                             {GreaterThanPredicate{Column: label, Value: BinaryValue([]byte("5"))}},
			"a not on an equality on a nullable string column":                       {NotPredicate{Inner: EqualPredicate{Column: label, Value: labelValue}}},
			"a greater than or equal with an empty string on a nullable column":      {OrPredicate{Left: GreaterThanPredicate{Column: label, Value: BinaryValue(nil)}, Right: EqualPredicate{Column: label, Value: BinaryValue(nil)}}},
			"a less than or equal with an empty string on a nullable column":         {OrPredicate{Left: LessThanPredicate{Column: label, Value: BinaryValue(nil)}, Right: EqualPredicate{Column: label, Value: BinaryValue(nil)}}},
			"a greater than or equal with a string on a nullable column":             {OrPredicate{Left: GreaterThanPredicate{Column: label, Value: labelValue}, Right: EqualPredicate{Column: label, Value: labelValue}}},
			"a less than or equal with a string on a nullable column":                {OrPredicate{Left: LessThanPredicate{Column: label, Value: labelValue}, Right: EqualPredicate{Column: label, Value: labelValue}}},
			"a greater than on a nullable int64 column":                              {GreaterThanPredicate{Column: count, Value: Int64Value(20)}},
			"a less than on a nullable int64 column":                                 {LessThanPredicate{Column: count, Value: Int64Value(20)}},
			"an equality on a nullable int64 column":                                 {EqualPredicate{Column: count, Value: Int64Value(0)}},
			"an equality with a nil value on a nullable int64 column":                {EqualPredicate{Column: count, Value: Value{}}},
			"a greater than or equal on a nullable int64 column":                     {OrPredicate{Left: GreaterThanPredicate{Column: count, Value: Int64Value(20)}, Right: EqualPredicate{Column: count, Value: Int64Value(20)}}},
			"a less than or equal on a nullable int64 column":                        {OrPredicate{Left: EqualPredicate{Column: count, Value: Int64Value(20)}, Right: LessThanPredicate{Column: count, Value: Int64Value(20)}}},
			"a greater than or equal predicate on a nullable int64 column":           {GreaterThanOrEqualPredicate{Column: count, Value: Int64Value(20)}},
			"a less than or equal predicate on a nullable int64 column":              {LessThanOrEqualPredicate{Column: count, Value: Int64Value(20)}},
			"a greater than or equal predicate with a nil value":                     {GreaterThanOrEqualPredicate{Column: count, Value: Value{}}},
			"a less than or equal predicate with a nil value":                        {LessThanOrEqualPredicate{Column: count, Value: Value{}}},
			"a greater than or equal predicate on a nullable string column":          {GreaterThanOrEqualPredicate{Column: label, Value: labelValue}},
			"a less than or equal predicate on a nullable string column":             {LessThanOrEqualPredicate{Column: label, Value: labelValue}},
			"a greater than or equal predicate with an empty string":                 {GreaterThanOrEqualPredicate{Column: label, Value: BinaryValue(nil)}},
			"a less than or equal predicate with an empty string":                    {LessThanOrEqualPredicate{Column: label, Value: BinaryValue(nil)}},
			"a selective predicate and then a less than or equal predicate":          {InPredicate{Column: stream, Values: NewInt64ValueSetOf(5)}, LessThanOrEqualPredicate{Column: count, Value: Int64Value(25)}},
			"an in that holds zero on a nullable int64 column":                       {InPredicate{Column: count, Values: NewInt64ValueSetOf(0, 5)}},
			"a not on an in that holds zero on a nullable int64 column":              {NotPredicate{Inner: InPredicate{Column: count, Values: NewInt64ValueSetOf(0, 5)}}},
			"an or of a greater than and an equality on two columns":                 {OrPredicate{Left: GreaterThanPredicate{Column: stream, Value: Int64Value(3)}, Right: EqualPredicate{Column: count, Value: Int64Value(3)}}},
			"an or of a greater than and an equality with another value":             {OrPredicate{Left: GreaterThanPredicate{Column: stream, Value: Int64Value(3)}, Right: EqualPredicate{Column: stream, Value: Int64Value(5)}}},
			"a selective predicate and then an in on a nullable int64 column":        {InPredicate{Column: stream, Values: NewInt64ValueSetOf(5)}, InPredicate{Column: count, Values: NewInt64ValueSetOf(0, 5, 10, 15, 20, 25)}},
			"a selective predicate and then an equality with a nil value":            {InPredicate{Column: stream, Values: NewInt64ValueSetOf(5)}, EqualPredicate{Column: label, Value: Value{}}},
			"a selective predicate and then a func predicate with Keep only":         {InPredicate{Column: stream, Values: NewInt64ValueSetOf(5)}, FuncPredicate{Column: label, Keep: func(_ Column, v Value) bool { return v.IsNil() || len(v.Binary()) > 4 }}},
			"a selective predicate and then an in on a nullable string column":       {InPredicate{Column: stream, Values: NewInt64ValueSetOf(5)}, InPredicate{Column: label, Values: NewBinaryValueSet([]Value{labelValue})}},
			"an or across columns":                                                   {OrPredicate{Left: InPredicate{Column: stream, Values: NewInt64ValueSetOf(1)}, Right: containsNeedle(msg, "7x")}},
			"a predicate that no row passes":                                         {EqualPredicate{Column: stream, Value: Int64Value(-5)}},
			"a line filter and then a column that no row of the record batch passes": {containsNeedle(msg, "no such line"), EqualPredicate{Column: label, Value: labelValue}},
		} {
			t.Run(name, func(t *testing.T) {
				requireSameRows(t, RowReaderOptions{Dataset: dset, ProjectedColumns: columns, Predicates: predicates})
				requireSameRows(t, RowReaderOptions{Dataset: dset, ProjectedColumns: []Column{ts, msg}, Predicates: predicates})
				requireSameRows(t, RowReaderOptions{Dataset: dset, ProjectedColumns: []Column{label, count}, Predicates: predicates, PrefetchAllOnOpen: true})
			})
		}
	})

	t.Run("returns the same rows as Read when the selected rows are scattered over the pages", func(t *testing.T) {
		const streamCardinality, streamRunLength = 400, 1
		generator := DatasetGenerator{RowCount: 8000, PageSizeHint: 2048, Columns: generatedColumns(streamCardinality, streamRunLength)}
		dset, columns := generator.Build(t, 3)
		stream, ts, msg, label := columns[0], columns[1], columns[2], columns[3]
		labelValue := firstBinaryValue(t, dset, label)

		for name, predicates := range map[string][]Predicate{
			"a rare stream and then a line filter": {
				InPredicate{Column: stream, Values: NewInt64ValueSetOf(3, 77)},
				containsNeedle(msg, "1x"),
			},
			"a few streams, a label and then a line filter": {
				InPredicate{Column: stream, Values: NewInt64ValueSetOf(streamRange(5, 25)...)},
				EqualPredicate{Column: label, Value: BinaryValue(nil)},
				containsNeedle(msg, "1x"),
			},
			"a rare stream and then two conditions on the same column": {
				InPredicate{Column: stream, Values: NewInt64ValueSetOf(9, 10)},
				AndPredicate{
					Left:  containsNeedle(msg, "1x"),
					Right: NotPredicate{Inner: EqualPredicate{Column: label, Value: labelValue}},
				},
			},
			"an or that reads a column for few rows and then a predicate that needs it for many": {
				OrPredicate{
					Left:  NotPredicate{Inner: EqualPredicate{Column: stream, Value: Int64Value(7)}},
					Right: EqualPredicate{Column: msg, Value: BinaryValue([]byte("no such line"))},
				},
				containsNeedle(msg, "1x"),
			},
			"an or that reads a column for few rows and then the projection of the column": {
				OrPredicate{
					Left:  NotPredicate{Inner: EqualPredicate{Column: stream, Value: Int64Value(7)}},
					Right: EqualPredicate{Column: msg, Value: BinaryValue([]byte("no such line"))},
				},
			},
			"a not on an and that reads a column for few rows and then a predicate that needs it for many": {
				NotPredicate{Inner: AndPredicate{
					Left:  EqualPredicate{Column: stream, Value: Int64Value(7)},
					Right: EqualPredicate{Column: msg, Value: BinaryValue([]byte("no such line"))},
				}},
				containsNeedle(msg, "1x"),
			},
			"a common set of streams and then a line filter": {
				InPredicate{Column: stream, Values: NewInt64ValueSetOf(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20)},
				containsNeedle(msg, "1x"),
			},
		} {
			t.Run(name, func(t *testing.T) {
				for _, prefetch := range []bool{false, true} {
					requireSameRows(t, RowReaderOptions{Dataset: dset, ProjectedColumns: columns, Predicates: predicates, PrefetchAllOnOpen: prefetch})
					requireSameRows(t, RowReaderOptions{Dataset: dset, ProjectedColumns: []Column{ts, label}, Predicates: predicates, PrefetchAllOnOpen: prefetch})
				}
			})
		}
	})

	t.Run("keeps the record batches valid until the caller reclaims the allocator", func(t *testing.T) {
		dset, columns := buildTestDataset(t)
		first, year := columns[0], columns[3]

		for name, predicates := range map[string][]Predicate{
			"when there are no predicates":                 nil,
			"when a predicate keeps some rows":             {GreaterThanPredicate{Column: year, Value: Int64Value(1983)}},
			"when a predicate keeps every row":             {GreaterThanPredicate{Column: year, Value: Int64Value(0)}},
			"when an or reads a column for few rows":       {OrPredicate{Left: EqualPredicate{Column: first, Value: BinaryValue([]byte("John"))}, Right: EqualPredicate{Column: year, Value: Int64Value(1987)}}},
			"when a func predicate keeps some binary rows": {FuncPredicate{Column: first, Keep: func(_ Column, v Value) bool { return len(v.Binary()) > 3 }}},
		} {
			t.Run(name, func(t *testing.T) {
				opts := RowReaderOptions{Dataset: dset, ProjectedColumns: columns, Predicates: predicates}
				want := readAllRowsAsText(t, opts, 3)

				r := NewRowReader(opts)
				t.Cleanup(func() { require.NoError(t, r.Close()) })
				require.NoError(t, r.Open(t.Context()))

				var (
					alloc   memory.Allocator
					batches []*columnar.RecordBatch
				)
				for {
					batch, err := r.ReadRecordBatch(t.Context(), &alloc, 3)
					if errors.Is(err, io.EOF) {
						break
					}
					require.NoError(t, err)
					batches = append(batches, batch)
				}

				var got [][]string
				for _, batch := range batches {
					got = appendRecordBatchText(t, got, batch)
				}
				require.Equal(t, want, got)
			})
		}
	})

	t.Run("downloads no more pages and makes no more download calls than Read when few rows pass and pages download lazily", func(t *testing.T) {
		generator := DatasetGenerator{RowCount: 8000, PageSizeHint: 2048, Columns: generatedColumns(400, 1)}
		dset, columns := generator.Build(t, 3)
		predicates := []Predicate{InPredicate{Column: columns[0], Values: NewInt64ValueSetOf(3, 77)}}

		var totalPages int
		for _, column := range columns {
			pages, err := result.Collect(column.ListPages(t.Context()))
			require.NoError(t, err)
			totalPages += len(pages)
		}

		opts := RowReaderOptions{Dataset: dset, ProjectedColumns: columns, Predicates: predicates}
		rowDownloads := countDownloads(t, opts, 1, 1024, false)
		recordBatchDownloads := countDownloads(t, opts, 1, 1024, true)

		require.Less(t, recordBatchDownloads.pages, totalPages)
		require.LessOrEqual(t, recordBatchDownloads.pages, rowDownloads.pages)
		require.LessOrEqual(t, recordBatchDownloads.calls, rowDownloads.calls)
	})

	t.Run("downloads no more calls, pages and bytes than Read for each predicate shape and download target", func(t *testing.T) {
		generator := DatasetGenerator{RowCount: 8000, PageSizeHint: 2048, Columns: generatedColumns(400, 1)}
		dset, columns := generator.Build(t, 3)
		stream, ts, msg := columns[0], columns[1], columns[2]

		var (
			fewStreams  = InPredicate{Column: stream, Values: NewInt64ValueSetOf(3, 77)}
			manyStreams = InPredicate{Column: stream, Values: NewInt64ValueSetOf(streamRange(0, 100)...)}
			timeRange   = AndPredicate{
				Left:  GreaterThanOrEqualPredicate{Column: ts, Value: Int64Value(40_000)},
				Right: LessThanPredicate{Column: ts, Value: Int64Value(50_000)},
			}
		)
		shapes := []struct {
			name       string
			projected  []Column
			predicates []Predicate
		}{
			{name: "no predicate"},
			{name: "a stream filter that keeps few rows", predicates: []Predicate{fewStreams}},
			{name: "a stream filter that keeps many rows", predicates: []Predicate{manyStreams}},
			{name: "a time range", predicates: []Predicate{timeRange}},
			{name: "a stream filter and then a time range", predicates: []Predicate{fewStreams, timeRange}},
			{name: "a stream filter and then a line filter", predicates: []Predicate{manyStreams, containsNeedle(msg, "1")}},
			{name: "a time range and then a line filter", predicates: []Predicate{timeRange, containsNeedle(msg, "1")}},
			{name: "a stream filter and a projection of one column", projected: []Column{msg}, predicates: []Predicate{fewStreams}},
		}

		for _, target := range []int{1, 32 << 10} {
			for _, shape := range shapes {
				t.Run(fmt.Sprintf("for %s with a download target of %d bytes", shape.name, target), func(t *testing.T) {
					projected := shape.projected
					if projected == nil {
						projected = columns
					}
					opts := RowReaderOptions{Dataset: dset, ProjectedColumns: projected, Predicates: shape.predicates}

					rowDownloads := countDownloads(t, opts, target, 512, false)
					recordBatchDownloads := countDownloads(t, opts, target, 512, true)

					require.LessOrEqual(t, recordBatchDownloads.calls, rowDownloads.calls)
					require.LessOrEqual(t, recordBatchDownloads.pages, rowDownloads.pages)
					require.LessOrEqual(t, recordBatchDownloads.bytes, rowDownloads.bytes)
				})
			}
		}
	})

	t.Run("returns io.EOF and an empty record batch of typed arrays at the end of the dataset, on every call", func(t *testing.T) {
		dset, columns := buildTestDataset(t)

		r := NewRowReader(RowReaderOptions{Dataset: dset, ProjectedColumns: columns})
		t.Cleanup(func() { require.NoError(t, r.Close()) })
		require.NoError(t, r.Open(t.Context()))

		var alloc memory.Allocator
		for {
			_, err := r.ReadRecordBatch(t.Context(), &alloc, 4)
			if err != nil {
				require.ErrorIs(t, err, io.EOF)
				break
			}
		}

		for range 2 {
			batch, err := r.ReadRecordBatch(t.Context(), &alloc, 4)
			require.ErrorIs(t, err, io.EOF)
			require.Equal(t, int64(0), batch.NumRows())
			require.Equal(t, int64(len(columns)), batch.NumCols())
			require.IsType(t, &columnar.UTF8{}, batch.Column(0))
			require.IsType(t, &columnar.Number[int64]{}, batch.Column(3))
		}
	})

	t.Run("returns an error when it is read before it is opened", func(t *testing.T) {
		dset, columns := buildTestDataset(t)
		r := NewRowReader(RowReaderOptions{Dataset: dset, ProjectedColumns: columns})
		t.Cleanup(func() { require.NoError(t, r.Close()) })

		var alloc memory.Allocator
		batch, err := r.ReadRecordBatch(t.Context(), &alloc, 1)
		require.EqualError(t, err, "row reader not opened")
		require.ErrorIs(t, err, errRowReaderNotOpen)
		require.Equal(t, int64(0), batch.NumRows())
		require.Equal(t, int64(len(columns)), batch.NumCols())
	})

	t.Run("returns an error when the batch size is not positive", func(t *testing.T) {
		dset, columns := buildTestDataset(t)
		r := NewRowReader(RowReaderOptions{Dataset: dset, ProjectedColumns: columns})
		t.Cleanup(func() { require.NoError(t, r.Close()) })
		require.NoError(t, r.Open(t.Context()))

		var alloc memory.Allocator
		for _, batchSize := range []int{0, -1} {
			batch, err := r.ReadRecordBatch(t.Context(), &alloc, batchSize)
			require.EqualError(t, err, fmt.Sprintf("batch size must be positive, got %d", batchSize))
			require.Equal(t, int64(0), batch.NumRows())
		}
	})

	t.Run("returns an error when the allocator is nil", func(t *testing.T) {
		dset, columns := buildTestDataset(t)
		r := NewRowReader(RowReaderOptions{Dataset: dset, ProjectedColumns: columns})
		t.Cleanup(func() { require.NoError(t, r.Close()) })
		require.NoError(t, r.Open(t.Context()))

		batch, err := r.ReadRecordBatch(t.Context(), nil, 1)
		require.EqualError(t, err, "allocator is nil")
		require.Equal(t, int64(0), batch.NumRows())
	})

	t.Run("returns the error of the context when the context is canceled", func(t *testing.T) {
		dset, columns := buildTestDataset(t)
		r := NewRowReader(RowReaderOptions{Dataset: dset, ProjectedColumns: columns})
		t.Cleanup(func() { require.NoError(t, r.Close()) })
		require.NoError(t, r.Open(t.Context()))

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		var alloc memory.Allocator
		_, err := r.ReadRecordBatch(ctx, &alloc, 1)
		require.EqualError(t, err, "context canceled")
	})

	t.Run("returns an error when it opens with a predicate that reads a nil column", func(t *testing.T) {
		dset, columns := buildTestDataset(t)
		r := NewRowReader(RowReaderOptions{
			Dataset:          dset,
			ProjectedColumns: columns,
			Predicates:       []Predicate{EqualPredicate{Column: nil, Value: Int64Value(1)}},
		})
		t.Cleanup(func() { require.NoError(t, r.Close()) })

		require.EqualError(t, r.Open(t.Context()), "initializing reader: predicate reads a nil column")
	})

	t.Run("returns an error, on every attempt, when it opens with a func predicate that has no Keep function", func(t *testing.T) {
		dset, columns := buildTestDataset(t)
		r := NewRowReader(RowReaderOptions{
			Dataset:          dset,
			ProjectedColumns: columns,
			Predicates: []Predicate{FuncPredicate{
				Column:    columns[0],
				KeepBatch: func(_ Column, _ *columnar.UTF8, dst []int32) []int32 { return dst },
			}},
		})
		t.Cleanup(func() { require.NoError(t, r.Close()) })

		const wantErr = `initializing reader: func predicate on column "first_name" has no Keep function`
		require.EqualError(t, r.Open(t.Context()), wantErr)
		require.EqualError(t, r.Open(t.Context()), wantErr)
	})

	t.Run("returns an error when a KeepBatch function returns indexes that are not ascending and in range", func(t *testing.T) {
		dset, columns := buildTestDataset(t)
		rows := int32(len(basicReaderTestData))

		for name, tc := range map[string]struct {
			indexes func(dst []int32) []int32
			wantErr string
		}{
			"in descending order": {
				indexes: func(dst []int32) []int32 { return append(dst, 1, 0) },
				wantErr: "kept index 0 of 10 values after index 1",
			},
			"repeated": {
				indexes: func(dst []int32) []int32 { return append(dst, 1, 1) },
				wantErr: "kept index 1 of 10 values after index 1",
			},
			"equal to the number of values": {
				indexes: func(dst []int32) []int32 { return append(dst, rows) },
				wantErr: "kept index 10 of 10 values after index -1",
			},
			"negative": {
				indexes: func(dst []int32) []int32 { return append(dst, -1) },
				wantErr: "kept index -1 of 10 values after index -1",
			},
		} {
			t.Run("when they are "+name, func(t *testing.T) {
				r := NewRowReader(RowReaderOptions{
					Dataset:          dset,
					ProjectedColumns: columns,
					Predicates: []Predicate{FuncPredicate{
						Column:    columns[0],
						Keep:      func(Column, Value) bool { return true },
						KeepBatch: func(_ Column, _ *columnar.UTF8, dst []int32) []int32 { return tc.indexes(dst) },
					}},
				})
				t.Cleanup(func() { require.NoError(t, r.Close()) })
				require.NoError(t, r.Open(t.Context()))

				var alloc memory.Allocator
				_, err := r.ReadRecordBatch(t.Context(), &alloc, len(basicReaderTestData))
				require.EqualError(t, err, "evaluating predicate 0: batch predicate "+tc.wantErr+", want ascending indexes in range")
			})
		}
	})

	t.Run("returns an error when a KeepBatch function returns an index out of range for a compacted array", func(t *testing.T) {
		dset, columns := buildTestDataset(t)
		r := NewRowReader(RowReaderOptions{
			Dataset:          dset,
			ProjectedColumns: columns,
			Predicates: []Predicate{
				GreaterThanPredicate{Column: columns[3], Value: Int64Value(1985)},
				FuncPredicate{
					Column: columns[0],
					Keep:   func(Column, Value) bool { return true },
					KeepBatch: func(_ Column, values *columnar.UTF8, dst []int32) []int32 {
						return append(dst, int32(values.Len()))
					},
				},
			},
		})
		t.Cleanup(func() { require.NoError(t, r.Close()) })
		require.NoError(t, r.Open(t.Context()))

		var alloc memory.Allocator
		_, err := r.ReadRecordBatch(t.Context(), &alloc, len(basicReaderTestData))
		require.EqualError(t, err, "evaluating predicate 1: batch predicate kept index 3 of 3 values after index -1, want ascending indexes in range")
	})

	t.Run("returns the same rows and records the same statistics as a run without failures when a read fails at any page download and the caller retries", func(t *testing.T) {
		generator := DatasetGenerator{RowCount: 3000, PageSizeHint: 2048, Columns: generatedColumns(100, 1)}
		dset, columns := generator.Build(t, 5)
		stream, ts, msg := columns[0], columns[1], columns[2]

		for name, predicates := range map[string][]Predicate{
			"without predicates":         nil,
			"with a selective predicate": {InPredicate{Column: stream, Values: NewInt64ValueSetOf(3, 77)}},
			"with an and of two comparisons": {AndPredicate{
				Left:  GreaterThanPredicate{Column: ts, Value: Int64Value(20_000)},
				Right: LessThanPredicate{Column: ts, Value: Int64Value(80_000)},
			}},
			"with an or of two columns": {OrPredicate{
				Left:  InPredicate{Column: stream, Values: NewInt64ValueSetOf(1)},
				Right: EqualPredicate{Column: msg, Value: BinaryValue([]byte("no such line"))},
			}},
			"with a not": {NotPredicate{Inner: GreaterThanPredicate{Column: ts, Value: Int64Value(50_000)}}},
			"with a selective predicate and a line filter": {
				InPredicate{Column: stream, Values: NewInt64ValueSetOf(3, 5, 6, 7, 77)},
				containsNeedle(msg, "1x"),
			},
		} {
			t.Run(name, func(t *testing.T) {
				opts := RowReaderOptions{Dataset: dset, ProjectedColumns: columns, Predicates: predicates}
				want := readAllRowsAsText(t, opts, 64)

				wantStats := runAndCollectStats(t, func(ctx context.Context) {
					readAllRecordBatches(ctx, t, opts, 64)
				})

				for failAt := 1; ; failAt++ {
					failing := &failingAtDataset{Dataset: dset, failAt: failAt}
					opts := opts
					opts.Dataset = failing

					var (
						got      [][]string
						failures int
					)
					gotStats := runAndCollectStats(t, func(ctx context.Context) {
						r := NewRowReader(opts)
						require.NoError(t, r.Open(ctx))
						r.dl.targetCompressedBytes = 1

						var alloc memory.Allocator
						for {
							alloc.Reclaim()
							batch, err := r.ReadRecordBatch(ctx, &alloc, 64)
							if err != nil && !errors.Is(err, io.EOF) {
								failures++
								require.ErrorIs(t, err, errInjectedReadPages, "fail at %d", failAt)
								require.Equal(t, int64(0), batch.NumRows())
								continue
							}
							got = appendRecordBatchText(t, got, batch)
							if errors.Is(err, io.EOF) {
								break
							}
						}
						require.NoError(t, r.Close())
					})
					require.Equal(t, want, got, "fail at %d", failAt)
					for _, stat := range parityStats {
						require.Equal(t, wantStats[stat], gotStats[stat], "%s, fail at %d", stat, failAt)
					}

					if failing.calls < failAt {
						return
					}
					require.Equal(t, 1, failures, "fail at %d", failAt)
				}
			})
		}
	})

	t.Run("returns an error that does not match io.EOF when listing the pages fails with io.EOF or an error that wraps it", func(t *testing.T) {
		dset, columns := buildTestDataset(t)
		keepAll := FuncPredicate{Column: columns[0], Keep: func(Column, Value) bool { return true }}

		for errName, listErr := range map[string]error{
			"a bare io.EOF":              io.EOF,
			"an error that wraps io.EOF": fmt.Errorf("listing pages: %w", io.EOF),
		} {
			for name, predicates := range map[string][]Predicate{
				"without predicates":                                {},
				"with a func predicate":                             {keepAll},
				"with a func predicate on the right side of an or":  {OrPredicate{Left: FalsePredicate{}, Right: keepAll}},
				"with a func predicate on the right side of an and": {AndPredicate{Left: NotPredicate{Inner: FalsePredicate{}}, Right: keepAll}},
			} {
				t.Run(name+", when the listing fails with "+errName, func(t *testing.T) {
					r := NewRowReader(RowReaderOptions{Dataset: &failingListDataset{Dataset: dset, err: listErr}, ProjectedColumns: columns, Predicates: predicates})
					t.Cleanup(func() { require.NoError(t, r.Close()) })
					require.NoError(t, r.Open(t.Context()))

					var alloc memory.Allocator
					batch, err := r.ReadRecordBatch(t.Context(), &alloc, 4)
					require.ErrorContains(t, err, "listing pages of column 0")
					require.NotErrorIs(t, err, io.EOF)
					require.ErrorIs(t, err, io.ErrUnexpectedEOF)
					require.Equal(t, int64(0), batch.NumRows())
				})
			}
		}
	})

	t.Run("returns an error when the listed pages end before the last row of a column", func(t *testing.T) {
		dset, columns := buildTestDataset(t)
		r := NewRowReader(RowReaderOptions{Dataset: &truncatedListDataset{Dataset: dset}, ProjectedColumns: columns})
		t.Cleanup(func() { require.NoError(t, r.Close()) })
		require.NoError(t, r.Open(t.Context()))

		var alloc memory.Allocator
		batch, err := r.ReadRecordBatch(t.Context(), &alloc, len(basicReaderTestData))
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
		require.EqualError(t, err, "projecting column 0: pages of column 0 end at row 9, but the column holds 10 rows: unexpected EOF")
		require.Equal(t, int64(0), batch.NumRows())
	})

	t.Run("returns the same rows as Read when it runs again after Reset with other options", func(t *testing.T) {
		dset, columns := buildTestDataset(t)
		year := columns[3]

		optionsList := []RowReaderOptions{
			{Dataset: dset, ProjectedColumns: columns, Predicates: []Predicate{GreaterThanPredicate{Column: year, Value: Int64Value(1983)}}},
			{Dataset: dset, ProjectedColumns: []Column{columns[0]}},
			{Dataset: dset, ProjectedColumns: []Column{columns[2], columns[3]}, Predicates: []Predicate{LessThanPredicate{Column: year, Value: Int64Value(1983)}}},
			{Dataset: dset, ProjectedColumns: columns, PrefetchAllOnOpen: true, Predicates: []Predicate{GreaterThanPredicate{Column: year, Value: Int64Value(1983)}}},
			{Dataset: dset, ProjectedColumns: columns, Predicates: []Predicate{GreaterThanPredicate{Column: year, Value: Int64Value(1983)}}},
		}

		r := NewRowReader(optionsList[0])
		t.Cleanup(func() { require.NoError(t, r.Close()) })
		for i, opts := range optionsList {
			r.Reset(opts)
			require.NoError(t, r.Open(t.Context()))
			require.Equal(t, readAllRowsAsText(t, opts, 3), readRemainingRecordBatches(t.Context(), t, r, 3), "options %d", i)
		}
	})

	t.Run("releases the scratch memory when it closes, and returns the rows again from the first row after Reset", func(t *testing.T) {
		dset, columns := buildTestDataset(t)
		opts := RowReaderOptions{Dataset: dset, ProjectedColumns: columns, Predicates: []Predicate{GreaterThanPredicate{Column: columns[3], Value: Int64Value(1983)}}}
		want := readAllRowsAsText(t, opts, 3)

		r := NewRowReader(opts)
		require.NoError(t, r.Open(t.Context()))
		require.Equal(t, want, readRemainingRecordBatches(t.Context(), t, r, 3))
		require.NotZero(t, r.recordBatchScratch.AllocatedBytes())
		require.NoError(t, r.Close())
		require.Zero(t, r.recordBatchScratch.AllocatedBytes())

		r.Reset(opts)
		require.NoError(t, r.Open(t.Context()))
		require.Equal(t, want, readRemainingRecordBatches(t.Context(), t, r, 3))
		require.NoError(t, r.Close())
	})

	t.Run("returns the same rows as Read when calls of Read and ReadRecordBatch alternate", func(t *testing.T) {
		dset, columns := buildTestDataset(t)

		for name, predicates := range map[string][]Predicate{
			"without predicates":  nil,
			"with a predicate":    {GreaterThanPredicate{Column: columns[3], Value: Int64Value(1983)}},
			"with two predicates": {GreaterThanPredicate{Column: columns[3], Value: Int64Value(1979)}, LessThanPredicate{Column: columns[2], Value: BinaryValue([]byte("M"))}},
		} {
			t.Run(name, func(t *testing.T) {
				opts := RowReaderOptions{Dataset: dset, ProjectedColumns: columns, Predicates: predicates}

				r := NewRowReader(opts)
				t.Cleanup(func() { require.NoError(t, r.Close()) })
				require.NoError(t, r.Open(t.Context()))

				require.Equal(t, readAllRowsAsText(t, opts, 3), readAlternating(t, r, 3, 3))
			})
		}
	})

	t.Run("returns the same rows as Read when calls of Read and ReadRecordBatch of different sizes alternate on a dataset with many pages", func(t *testing.T) {
		generator := DatasetGenerator{RowCount: 8000, PageSizeHint: 2048, Columns: generatedColumns(400, 1)}
		dset, columns := generator.Build(t, 3)
		predicates := []Predicate{
			InPredicate{Column: columns[0], Values: NewInt64ValueSetOf(streamRange(3, 13)...)},
			containsNeedle(columns[2], "1x"),
		}

		for _, prefetch := range []bool{false, true} {
			opts := RowReaderOptions{Dataset: dset, ProjectedColumns: columns, Predicates: predicates, PrefetchAllOnOpen: prefetch}

			r := NewRowReader(opts)
			t.Cleanup(func() { require.NoError(t, r.Close()) })
			require.NoError(t, r.Open(t.Context()))

			require.Equal(t, readAllRowsAsText(t, opts, 64), readAlternating(t, r, 7, 100), "prefetch %v", prefetch)
		}
	})

	t.Run("returns each row once when Open is called twice", func(t *testing.T) {
		dset, columns := buildTestDataset(t)
		opts := RowReaderOptions{Dataset: dset, ProjectedColumns: columns}

		r := NewRowReader(opts)
		t.Cleanup(func() { require.NoError(t, r.Close()) })
		require.NoError(t, r.Open(t.Context()))
		require.NoError(t, r.Open(t.Context()))
		require.Equal(t, readAllRowsAsText(t, opts, 3), readRemainingRecordBatches(t.Context(), t, r, 3))
	})

	t.Run("returns the same rows as Read from a dataset with no rows", func(t *testing.T) {
		dset, columns := memDatasetOf(t, buildInt64Column(t, "a"), buildStringColumn(t, "b"))
		requireSameRows(t, RowReaderOptions{Dataset: dset, ProjectedColumns: columns})
	})

	t.Run("reads the rows past the end of a column as null", func(t *testing.T) {
		dset, columns := buildShortColumnDataset(t)
		wantRows := func(keep func(i int) bool) [][]string {
			var rows [][]string
			for i := range 10 {
				if keep(i) {
					second := nilValueText
					if i < 7 {
						second = strconv.Itoa(i)
					}
					rows = append(rows, []string{fmt.Sprintf("a%d", i), second})
				}
			}
			return rows
		}

		t.Run("returns the same rows as Read when there are no predicates", func(t *testing.T) {
			requireSameRows(t, RowReaderOptions{Dataset: dset, ProjectedColumns: columns})
		})

		t.Run("returns the same rows as Read when another column holds the rows that the column misses", func(t *testing.T) {
			a, b, c := buildStringColumn(t, "a"), buildInt64Column(t, "b"), buildStringColumn(t, "c")
			for i := range 10 {
				require.NoError(t, a.Append(i, BinaryValue(fmt.Appendf(nil, "a%d", i))))
				require.NoError(t, c.Append(i, BinaryValue(fmt.Appendf(nil, "c%d", i))))
			}
			for i := range 7 {
				require.NoError(t, b.Append(i, Int64Value(int64(i))))
			}
			threeDataset, threeColumns := memDatasetOf(t, a, b, c)

			requireSameRows(t, RowReaderOptions{
				Dataset:          threeDataset,
				ProjectedColumns: threeColumns,
				Predicates:       []Predicate{FuncPredicate{Column: threeColumns[0], Keep: func(Column, Value) bool { return true }}},
			})
		})

		t.Run("returns null for the missing rows when a predicate keeps the rows of another column", func(t *testing.T) {
			opts := RowReaderOptions{
				Dataset:          dset,
				ProjectedColumns: columns,
				Predicates:       []Predicate{FuncPredicate{Column: columns[0], Keep: func(Column, Value) bool { return true }}},
			}
			for _, batchSize := range []int{3, 10} {
				require.Equal(t, wantRows(func(int) bool { return true }), readAllRecordBatches(t.Context(), t, opts, batchSize), "batch size %d", batchSize)
			}
		})

		t.Run("returns null for the missing rows when a later predicate reads the column", func(t *testing.T) {
			opts := RowReaderOptions{
				Dataset:          dset,
				ProjectedColumns: columns,
				Predicates: []Predicate{
					FuncPredicate{Column: columns[0], Keep: func(Column, Value) bool { return true }},
					FuncPredicate{Column: columns[1], Keep: func(_ Column, v Value) bool { return v.IsNil() || v.Int64() != 3 }},
				},
			}
			for _, batchSize := range []int{3, 10} {
				require.Equal(t, wantRows(func(i int) bool { return i != 3 }), readAllRecordBatches(t.Context(), t, opts, batchSize), "batch size %d", batchSize)
			}
		})

		t.Run("returns the kept rows, with null for the missing values, when a predicate keeps only some rows", func(t *testing.T) {
			keepRows := func(rows ...int) Predicate {
				keep := make(map[string]bool, len(rows))
				for _, i := range rows {
					keep[fmt.Sprintf("a%d", i)] = true
				}
				return FuncPredicate{Column: columns[0], Keep: func(_ Column, v Value) bool { return keep[string(v.Binary())] }}
			}
			shortValue := func(i int) string {
				if i < 7 {
					return strconv.Itoa(i)
				}
				return nilValueText
			}
			keepAllShort := FuncPredicate{Column: columns[1], Keep: func(Column, Value) bool { return true }}

			for _, rows := range [][]int{{8}, {1, 8}, {0, 6, 7, 9}, {9}} {
				var both, onlyShort [][]string
				for _, i := range rows {
					both = append(both, []string{fmt.Sprintf("a%d", i), shortValue(i)})
					onlyShort = append(onlyShort, []string{shortValue(i)})
				}

				for _, batchSize := range []int{1, 3, 10} {
					msg := fmt.Sprintf("rows %v, batch size %d", rows, batchSize)

					projectBoth := RowReaderOptions{Dataset: dset, ProjectedColumns: columns, Predicates: []Predicate{keepRows(rows...)}}
					require.Equal(t, both, readAllRecordBatches(t.Context(), t, projectBoth, batchSize), msg)

					projectShort := RowReaderOptions{Dataset: dset, ProjectedColumns: []Column{columns[1]}, Predicates: []Predicate{keepRows(rows...)}}
					require.Equal(t, onlyShort, readAllRecordBatches(t.Context(), t, projectShort, batchSize), msg)

					thenShort := RowReaderOptions{Dataset: dset, ProjectedColumns: columns, Predicates: []Predicate{keepRows(rows...), keepAllShort}}
					require.Equal(t, both, readAllRecordBatches(t.Context(), t, thenShort, batchSize), msg)
				}
			}
		})

		t.Run("returns the rows that the first predicate keeps when it reads the column and keeps null", func(t *testing.T) {
			opts := RowReaderOptions{
				Dataset:          dset,
				ProjectedColumns: columns,
				Predicates:       []Predicate{FuncPredicate{Column: columns[1], Keep: func(_ Column, v Value) bool { return v.IsNil() || v.Int64()%2 == 0 }}},
			}
			for _, batchSize := range []int{3, 10} {
				require.Equal(t, wantRows(func(i int) bool { return i >= 7 || i%2 == 0 }), readAllRecordBatches(t.Context(), t, opts, batchSize), "batch size %d", batchSize)
			}
		})

		t.Run("returns the same rows as Read when a short binary column has no predicate", func(t *testing.T) {
			a, b := buildInt64Column(t, "a"), buildStringColumn(t, "b")
			for i := range 10 {
				require.NoError(t, a.Append(i, Int64Value(int64(i))))
			}
			for i := range 7 {
				require.NoError(t, b.Append(i, BinaryValue(fmt.Appendf(nil, "b%d", i))))
			}
			binaryDataset, binaryColumns := memDatasetOf(t, a, b)

			requireSameRows(t, RowReaderOptions{Dataset: binaryDataset, ProjectedColumns: binaryColumns})
			requireSameRows(t, RowReaderOptions{Dataset: binaryDataset, ProjectedColumns: []Column{binaryColumns[1]}})
		})
	})

	t.Run("returns the same rows as Read for a dataset sorted by stream, which prunes pages into several ranges", func(t *testing.T) {
		dset, columns := buildSortedDataset(t, 3000)
		stream, ts, msg, label, count := columns[0], columns[1], columns[2], columns[3], columns[4]
		streams := InPredicate{Column: stream, Values: NewInt64ValueSetOf(2, 11, 25)}

		for name, predicates := range map[string][]Predicate{
			"an in on the stream":                        {streams},
			"an in on the stream and then a line filter": {streams, containsNeedle(msg, "x3")},
			"an equality on the stream":                  {EqualPredicate{Column: stream, Value: Int64Value(7)}},
			"a greater than on the stream":               {GreaterThanPredicate{Column: stream, Value: Int64Value(26)}},
			"an or of two streams": {OrPredicate{
				Left:  EqualPredicate{Column: stream, Value: Int64Value(3)},
				Right: EqualPredicate{Column: stream, Value: Int64Value(20)},
			}},
			"streams and a time range": {
				InPredicate{Column: stream, Values: NewInt64ValueSetOf(4, 5, 6)},
				AndPredicate{Left: GreaterThanPredicate{Column: ts, Value: Int64Value(450)}, Right: LessThanPredicate{Column: ts, Value: Int64Value(620)}},
			},
			"streams and then a nullable string": {InPredicate{Column: stream, Values: NewInt64ValueSetOf(8, 9)}, EqualPredicate{Column: label, Value: BinaryValue(nil)}},
			"streams and then a nullable int64":  {InPredicate{Column: stream, Values: NewInt64ValueSetOf(8, 9)}, GreaterThanPredicate{Column: count, Value: Int64Value(20)}},
			"a stream that no page holds":        {EqualPredicate{Column: stream, Value: Int64Value(500)}},
		} {
			t.Run(name, func(t *testing.T) {
				for _, prefetch := range []bool{false, true} {
					opts := RowReaderOptions{Dataset: dset, ProjectedColumns: columns, Predicates: predicates, PrefetchAllOnOpen: prefetch}
					requireSameRows(t, opts)

					opts.ProjectedColumns = []Column{msg, label}
					requireSameRows(t, opts)
				}
			})
		}

		t.Run("prunes the rows into several ranges and records the same statistics as Read", func(t *testing.T) {
			opts := RowReaderOptions{Dataset: dset, ProjectedColumns: columns, Predicates: []Predicate{streams, containsNeedle(msg, "x3")}}

			r := NewRowReader(opts)
			require.NoError(t, r.Open(t.Context()))
			var ranges int
			for range r.ranges.Iter() {
				ranges++
			}
			require.NoError(t, r.Close())
			require.Greater(t, ranges, 1)

			requireSameStats(t, opts, 100)
		})
	})

	t.Run("returns the same rows as Read for the nullable columns of a dataset sorted by stream", func(t *testing.T) {
		dset, columns := buildSortedDataset(t, 3000)
		stream, msg, label := columns[0], columns[2], columns[3]
		emptyLabel := FuncPredicate{
			Column: label,
			Keep:   func(_ Column, v Value) bool { return v.IsNil() || len(v.Binary()) == 0 },
			KeepBatch: func(_ Column, values *columnar.UTF8, dst []int32) []int32 {
				for i := range values.Len() {
					if len(values.Get(i)) == 0 {
						dst = append(dst, int32(i))
					}
				}
				return dst
			},
		}

		for name, predicates := range map[string][]Predicate{
			"a func predicate with KeepBatch on a nullable string column": {emptyLabel},
			"streams and then a func predicate with KeepBatch":            {InPredicate{Column: stream, Values: NewInt64ValueSetOf(4, 8)}, emptyLabel},
			"a line filter and then a func predicate with KeepBatch":      {containsNeedle(msg, "x3"), emptyLabel},
		} {
			t.Run(name, func(t *testing.T) {
				requireSameRows(t, RowReaderOptions{Dataset: dset, ProjectedColumns: columns, Predicates: predicates})
			})
		}
	})

	t.Run("returns the same rows as Read when a projected column is repeated", func(t *testing.T) {
		dset, columns := buildTestDataset(t)
		year := columns[3]
		requireSameRows(t, RowReaderOptions{
			Dataset:          dset,
			ProjectedColumns: []Column{year, columns[0], year},
			Predicates:       []Predicate{GreaterThanPredicate{Column: year, Value: Int64Value(1983)}},
		})
	})

	t.Run("returns record batches with no column when no column is projected", func(t *testing.T) {
		dset, columns := buildTestDataset(t)
		opts := RowReaderOptions{Dataset: dset, Predicates: []Predicate{GreaterThanPredicate{Column: columns[3], Value: Int64Value(1983)}}}
		want := len(readAllRowsAsText(t, RowReaderOptions{Dataset: dset, ProjectedColumns: columns, Predicates: opts.Predicates}, 3))

		r := NewRowReader(opts)
		t.Cleanup(func() { require.NoError(t, r.Close()) })
		require.NoError(t, r.Open(t.Context()))

		var (
			alloc memory.Allocator
			rows  int64
		)
		for {
			batch, err := r.ReadRecordBatch(t.Context(), &alloc, 3)
			rows += batch.NumRows()
			if errors.Is(err, io.EOF) {
				break
			}
			require.NoError(t, err)
			require.Equal(t, int64(0), batch.NumCols())
		}
		require.Equal(t, int64(want), rows)
	})

	t.Run("keeps the scratch memory bounded while it reads", func(t *testing.T) {
		const maxScratchGrowthFactor = 8

		generator := DatasetGenerator{RowCount: 8000, PageSizeHint: 2048, Columns: generatedColumns(400, 1)}
		dset, columns := generator.Build(t, 3)
		stream, msg := columns[0], columns[2]

		for name, predicates := range map[string][]Predicate{
			"with a selective predicate": {InPredicate{Column: stream, Values: NewInt64ValueSetOf(3, 77)}},
			"with a selective predicate and a line filter": {
				InPredicate{Column: stream, Values: NewInt64ValueSetOf(streamRange(3, 13)...)},
				containsNeedle(msg, "1x"),
			},
		} {
			t.Run(name, func(t *testing.T) {
				r := NewRowReader(RowReaderOptions{Dataset: dset, ProjectedColumns: columns, Predicates: predicates})
				t.Cleanup(func() { require.NoError(t, r.Close()) })
				require.NoError(t, r.Open(t.Context()))

				var (
					alloc            memory.Allocator
					afterFiveReads   int
					afterTheLastRead int
				)
				for reads := 1; ; reads++ {
					alloc.Reclaim()
					_, err := r.ReadRecordBatch(t.Context(), &alloc, 16)
					if reads == 5 {
						afterFiveReads = r.recordBatchScratch.AllocatedBytes()
					}
					afterTheLastRead = r.recordBatchScratch.AllocatedBytes()
					if errors.Is(err, io.EOF) {
						break
					}
					require.NoError(t, err)
				}
				require.Less(t, afterTheLastRead, maxScratchGrowthFactor*afterFiveReads)
			})
		}
	})

	t.Run("records one read call for each call of ReadRecordBatch", func(t *testing.T) {
		dset, columns := buildTestDataset(t)
		stats := runAndCollectStats(t, func(ctx context.Context) {
			r := NewRowReader(RowReaderOptions{Dataset: dset, ProjectedColumns: columns})
			t.Cleanup(func() { require.NoError(t, r.Close()) })
			require.NoError(t, r.Open(ctx))

			var alloc memory.Allocator
			for range 3 {
				_, err := r.ReadRecordBatch(ctx, &alloc, 4)
				require.NoError(t, err)
			}
		})
		require.Equal(t, int64(3), stats[dataobj.StatDatasetReadCalls.Name()])
	})

	t.Run("passes a compacted array to KeepBatch when fewer than half of the rows of a record batch reach it", func(t *testing.T) {
		dset, columns := buildSortedDataset(t, 400)
		ts, msg := columns[1], columns[2]

		var lengths []int
		recordLengths := FuncPredicate{
			Column: msg,
			Keep:   func(Column, Value) bool { return true },
			KeepBatch: func(_ Column, values *columnar.UTF8, dst []int32) []int32 {
				lengths = append(lengths, values.Len())
				for i := range values.Len() {
					dst = append(dst, int32(i))
				}
				return dst
			},
		}

		for _, tc := range []struct {
			name        string
			upperBound  int64
			wantLengths []int
		}{
			{name: "passes 49 values when 49 of 100 rows reach it", upperBound: 49, wantLengths: []int{49}},
			{name: "passes 100 values when 50 of 100 rows reach it", upperBound: 50, wantLengths: []int{100}},
			{name: "passes 100 values when 51 of 100 rows reach it", upperBound: 51, wantLengths: []int{100}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				opts := RowReaderOptions{
					Dataset:          dset,
					ProjectedColumns: []Column{msg},
					Predicates:       []Predicate{LessThanPredicate{Column: ts, Value: Int64Value(tc.upperBound)}, recordLengths},
				}

				lengths = nil
				readAllRecordBatches(t.Context(), t, opts, 100)
				require.Equal(t, tc.wantLengths, lengths)

				requireSameRows(t, opts)
			})
		}
	})

	t.Run("does not call KeepBatch on the right side of an and or an or when the left side leaves no row to test", func(t *testing.T) {
		dset, columns := buildSortedDataset(t, 1000)
		stream, msg := columns[0], columns[2]

		var calls int
		counting := FuncPredicate{
			Column: msg,
			Keep:   func(Column, Value) bool { return true },
			KeepBatch: func(_ Column, _ *columnar.UTF8, dst []int32) []int32 {
				calls++
				return dst
			},
		}
		keepsNoRow := FuncPredicate{Column: stream, Keep: func(Column, Value) bool { return false }}
		keepsEveryRow := GreaterThanPredicate{Column: stream, Value: Int64Value(-5)}

		for name, predicate := range map[string]Predicate{
			"an and whose left side keeps no row":   AndPredicate{Left: keepsNoRow, Right: counting},
			"an or whose left side keeps every row": OrPredicate{Left: keepsEveryRow, Right: counting},
		} {
			t.Run(name, func(t *testing.T) {
				calls = 0
				readAllRecordBatches(t.Context(), t, RowReaderOptions{Dataset: dset, ProjectedColumns: columns, Predicates: []Predicate{predicate}}, 100)
				require.Zero(t, calls)
			})
		}
	})

	t.Run("tests only the rows that reach the right side of an or and of an and", func(t *testing.T) {
		dset, columns := buildSortedDataset(t, 1000)
		stream, msg := columns[0], columns[2]

		var calls int
		counting := FuncPredicate{Column: msg, Keep: func(Column, Value) bool { calls++; return true }}
		firstStreams := LessThanPredicate{Column: stream, Value: Int64Value(5)}

		for name, predicate := range map[string]Predicate{
			"calls Keep for the 500 rows that the left side rejects when the predicate is on the right side of an or": OrPredicate{Left: firstStreams, Right: counting},
			"calls Keep for the 500 rows that the left side keeps when the predicate is on the right side of an and":  AndPredicate{Left: NotPredicate{Inner: firstStreams}, Right: counting},
			"calls Keep for the 500 rows that the left side of an and keeps when a not wraps the and":                 NotPredicate{Inner: AndPredicate{Left: firstStreams, Right: NotPredicate{Inner: counting}}},
		} {
			t.Run(name, func(t *testing.T) {
				calls = 0
				readAllRecordBatches(t.Context(), t, RowReaderOptions{Dataset: dset, ProjectedColumns: []Column{msg}, Predicates: []Predicate{predicate}}, 64)
				require.Equal(t, 500, calls)
			})
		}
	})

	t.Run("returns the error of the context when the context is canceled during a scan where no row passes", func(t *testing.T) {
		dset, columns := buildSortedDataset(t, 1000)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		var calls int
		r := NewRowReader(RowReaderOptions{
			Dataset:          dset,
			ProjectedColumns: columns,
			Predicates: []Predicate{FuncPredicate{
				Column: columns[2],
				Keep: func(Column, Value) bool {
					calls++
					cancel()
					return false
				},
			}},
		})
		t.Cleanup(func() { require.NoError(t, r.Close()) })
		require.NoError(t, r.Open(ctx))

		var alloc memory.Allocator
		_, err := r.ReadRecordBatch(ctx, &alloc, 10)
		require.EqualError(t, err, "context canceled")
		require.Equal(t, 10, calls)
	})

	t.Run("records the same row statistics as Read, and no more bytes, for random predicate trees", func(t *testing.T) {
		const treeCount = 120

		generator := DatasetGenerator{RowCount: 8000, PageSizeHint: 2048, Columns: generatedColumns(400, 1)}
		dset, columns := generator.Build(t, 3)
		stream, ts, msg, label, count := columns[0], columns[1], columns[2], columns[3], columns[4]

		leaves := []Predicate{
			InPredicate{Column: stream, Values: NewInt64ValueSetOf(streamRange(0, 60)...)},
			GreaterThanPredicate{Column: ts, Value: Int64Value(50_000)},
			LessThanPredicate{Column: ts, Value: Int64Value(80_000)},
			GreaterThanPredicate{Column: count, Value: Int64Value(10)},
			EqualPredicate{Column: label, Value: BinaryValue(nil)},
			EqualPredicate{Column: msg, Value: BinaryValue([]byte("no such line"))},
			containsNeedle(msg, "1x"),
		}
		projections := [][]Column{columns, {msg, label}, {ts}}

		rng := rand.New(rand.NewSource(7))
		for i := range treeCount {
			predicates := []Predicate{randomPredicateTree(rng, leaves, 3)}
			if i%2 == 1 {
				predicates = append(predicates, randomPredicateTree(rng, leaves, 2))
			}
			opts := RowReaderOptions{Dataset: dset, ProjectedColumns: projections[i%len(projections)], Predicates: predicates}

			want := readStats(t, opts, 64, false)
			got := readStats(t, opts, 64, true)
			for _, stat := range parityRowStats {
				require.Equal(t, want[stat], got[stat], "%s, tree %d: %v", stat, i, predicates)
			}
			for _, stat := range parityByteStats {
				require.LessOrEqual(t, got[stat], want[stat], "%s, tree %d: %v", stat, i, predicates)
			}
		}
	})

	t.Run("records the same read statistics as Read for the predicate shapes that read the same rows", func(t *testing.T) {
		generator := DatasetGenerator{RowCount: 8000, PageSizeHint: 2048, Columns: generatedColumns(400, 1)}
		dset, columns := generator.Build(t, 3)
		stream, ts, msg, label := columns[0], columns[1], columns[2], columns[3]

		for name, predicates := range map[string][]Predicate{
			"when there are no predicates":             nil,
			"when a stream ID predicate comes first":   {InPredicate{Column: stream, Values: NewInt64ValueSetOf(3, 7)}, containsNeedle(msg, "7x")},
			"when a stream ID predicate comes second":  {GreaterThanPredicate{Column: ts, Value: Int64Value(50_000)}, InPredicate{Column: stream, Values: NewInt64ValueSetOf(3, 7)}},
			"when a predicate keeps no row":            {EqualPredicate{Column: stream, Value: Int64Value(-5)}},
			"when predicates read a nullable column":   {GreaterThanPredicate{Column: ts, Value: Int64Value(50_000)}, EqualPredicate{Column: label, Value: BinaryValue(nil)}},
			"when two predicates read the same column": {GreaterThanPredicate{Column: ts, Value: Int64Value(20_000)}, LessThanPredicate{Column: ts, Value: Int64Value(80_000)}},
			"when the right side of an or never runs for a projected column": {
				OrPredicate{
					Left:  GreaterThanPredicate{Column: ts, Value: Int64Value(-1)},
					Right: EqualPredicate{Column: msg, Value: BinaryValue([]byte("no such line"))},
				},
			},
			"when a later predicate reads a column again for more rows": {
				OrPredicate{
					Left:  NotPredicate{Inner: EqualPredicate{Column: stream, Value: Int64Value(7)}},
					Right: EqualPredicate{Column: msg, Value: BinaryValue([]byte("no such line"))},
				},
				containsNeedle(msg, "7x"),
			},
			"when only the projection reads a column again for more rows": {
				OrPredicate{
					Left:  NotPredicate{Inner: EqualPredicate{Column: stream, Value: Int64Value(7)}},
					Right: EqualPredicate{Column: msg, Value: BinaryValue([]byte("no such line"))},
				},
			},
			"when an or reads a projected column for part of the rows that an earlier predicate kept": {
				InPredicate{Column: stream, Values: NewInt64ValueSetOf(streamRange(0, 40)...)},
				OrPredicate{
					Left:  GreaterThanPredicate{Column: ts, Value: Int64Value(50_000)},
					Right: EqualPredicate{Column: msg, Value: BinaryValue([]byte("no such line"))},
				},
			},
		} {
			t.Run(name, func(t *testing.T) {
				requireSameStats(t, RowReaderOptions{Dataset: dset, ProjectedColumns: columns, Predicates: predicates}, 64)
			})
		}

		t.Run("when a predicate reads a column that is not projected", func(t *testing.T) {
			requireSameStats(t, RowReaderOptions{
				Dataset:          dset,
				ProjectedColumns: []Column{msg, label},
				Predicates:       []Predicate{InPredicate{Column: stream, Values: NewInt64ValueSetOf(streamRange(0, 40)...)}},
			}, 64)
		})

		t.Run("when predicates read every projected column", func(t *testing.T) {
			requireSameStats(t, RowReaderOptions{
				Dataset:          dset,
				ProjectedColumns: []Column{ts, msg},
				Predicates:       []Predicate{GreaterThanPredicate{Column: ts, Value: Int64Value(50_000)}, containsNeedle(msg, "7x")},
			}, 64)
		})
	})

	t.Run("uses memory in proportion to the record batch when a page holds many more rows", func(t *testing.T) {
		const (
			batchSize    = 32
			pageSizeHint = 256 * 1024
		)

		generator := DatasetGenerator{
			RowCount:     20_000,
			PageSizeHint: pageSizeHint,
			Columns: []generatorColumnConfig{
				{
					Tag:               "ts",
					Type:              ColumnType{Physical: datasetmd.PHYSICAL_TYPE_INT64, Logical: "number"},
					Encoding:          datasetmd.ENCODING_TYPE_DELTA,
					Compression:       datasetmd.COMPRESSION_TYPE_NONE,
					CardinalityTarget: 100_000,
				},
				{
					Tag:               "msg",
					Type:              ColumnType{Physical: datasetmd.PHYSICAL_TYPE_BINARY, Logical: "string"},
					Encoding:          datasetmd.ENCODING_TYPE_PLAIN,
					Compression:       datasetmd.COMPRESSION_TYPE_ZSTD,
					AvgSize:           200,
					CardinalityTarget: 1000,
				},
			},
		}
		dset, columns := generator.Build(t, 1)
		ts, msg := columns[0], columns[1]

		pages, err := result.Collect(msg.ListPages(t.Context()))
		require.NoError(t, err)
		require.Greater(t, pages[0].PageDesc().RowCount, 10*batchSize)

		for name, predicates := range map[string][]Predicate{
			"without predicates":               nil,
			"with a predicate on the messages": {containsNeedle(msg, "1x")},
		} {
			t.Run(name, func(t *testing.T) {
				r := NewRowReader(RowReaderOptions{Dataset: dset, ProjectedColumns: []Column{ts, msg}, Predicates: predicates})
				t.Cleanup(func() { require.NoError(t, r.Close()) })
				require.NoError(t, r.Open(t.Context()))

				var alloc memory.Allocator
				for range 3 {
					alloc.Reclaim()
					_, err := r.ReadRecordBatch(t.Context(), &alloc, batchSize)
					require.NoError(t, err)
				}

				// Memory sized from the page would be far above the limit.
				const limit = pageSizeHint / 4
				require.Less(t, alloc.AllocatedBytes(), limit, "memory of the record batches")
				require.Less(t, r.recordBatchScratch.AllocatedBytes(), limit, "scratch memory")
			})
		}
	})
}

func TestRecordBatch_columnArray(t *testing.T) {
	const n = 4

	var alloc memory.Allocator

	newBatch := func(reader recordBatchColumnReader) *recordBatch {
		var rb recordBatch
		rb.reset(reader, &alloc, 1, 0, n)
		return &rb
	}

	t.Run("reads the union of the rows of earlier reads and of the new read, and reads nothing when the earlier reads filled them", func(t *testing.T) {
		reader := &fakeColumnReader{}
		rb := newBatch(reader)

		for _, rows := range [][]int{{1, 2}, {2, 3}, {1, 3}, {2}} {
			_, err := rb.columnArray(t.Context(), 0, selectionOf(&alloc, n, rows...))
			require.NoError(t, err)
		}
		require.Equal(t, [][]int{{1, 2}, {1, 2, 3}}, reader.reads)
		require.Equal(t, int64(3*8), rb.filledBytes())
	})

	t.Run("reads every row when the new read selects every row, and then reads nothing", func(t *testing.T) {
		reader := &fakeColumnReader{}
		rb := newBatch(reader)

		for _, sel := range []selection{selectionOf(&alloc, n, 1), newSelectionAllRows(n), selectionOf(&alloc, n, 0, 3), newSelectionAllRows(n)} {
			_, err := rb.columnArray(t.Context(), 0, sel)
			require.NoError(t, err)
		}
		require.Equal(t, [][]int{{1}, nil}, reader.reads)
		require.Equal(t, int64(n*8), rb.filledBytes())
	})

	t.Run("keeps the earlier read when a new read fails", func(t *testing.T) {
		reader := &fakeColumnReader{}
		rb := newBatch(reader)

		_, err := rb.columnArray(t.Context(), 0, selectionOf(&alloc, n, 1, 2))
		require.NoError(t, err)

		reader.err = errInjectedReadPages
		_, err = rb.columnArray(t.Context(), 0, selectionOf(&alloc, n, 2, 3))
		require.ErrorIs(t, err, errInjectedReadPages)

		reader.err = nil
		_, err = rb.columnArray(t.Context(), 0, selectionOf(&alloc, n, 1))
		require.NoError(t, err)
		require.Equal(t, [][]int{{1, 2}, {1, 2, 3}}, reader.reads)
		require.Equal(t, int64(2*8), rb.filledBytes())
	})

	t.Run("returns an error when the reader returns an array of another length", func(t *testing.T) {
		rb := newBatch(&fakeColumnReader{length: n - 1})

		_, err := rb.columnArray(t.Context(), 0, selectionOf(&alloc, n, 1))
		require.EqualError(t, err, "column 0 has 3 rows, want 4")
	})
}

func BenchmarkRowReader_ReadRecordBatch(b *testing.B) {
	generator := DatasetGenerator{
		RowCount:     1_000_000,
		PageSizeHint: 2 * 1024 * 1024,
		Columns: []generatorColumnConfig{
			{
				Tag:               "stream",
				Type:              ColumnType{Physical: datasetmd.PHYSICAL_TYPE_INT64, Logical: "stream_id"},
				Encoding:          datasetmd.ENCODING_TYPE_DELTA,
				Compression:       datasetmd.COMPRESSION_TYPE_NONE,
				CardinalityTarget: 1000,
				RunLength:         1000,
			},
			{
				Tag:               "timestamp",
				Type:              ColumnType{Physical: datasetmd.PHYSICAL_TYPE_INT64, Logical: "number"},
				Encoding:          datasetmd.ENCODING_TYPE_DELTA,
				Compression:       datasetmd.COMPRESSION_TYPE_NONE,
				CardinalityTarget: 100_000,
			},
			{
				Tag:               "log",
				Type:              ColumnType{Physical: datasetmd.PHYSICAL_TYPE_BINARY, Logical: "string"},
				Encoding:          datasetmd.ENCODING_TYPE_PLAIN,
				Compression:       datasetmd.COMPRESSION_TYPE_ZSTD,
				AvgSize:           256,
				CardinalityTarget: 100_000,
			},
			{
				Tag:               "label",
				Type:              ColumnType{Physical: datasetmd.PHYSICAL_TYPE_BINARY, Logical: "label"},
				Encoding:          datasetmd.ENCODING_TYPE_PLAIN,
				Compression:       datasetmd.COMPRESSION_TYPE_ZSTD,
				AvgSize:           16,
				CardinalityTarget: 20,
				SparsityRate:      0.3,
			},
		},
	}

	dset, cols := generator.Build(b, 1)
	stream, ts, msg, label := cols[0], cols[1], cols[2], cols[3]

	timeRange := func(from, to int64) Predicate {
		return AndPredicate{
			Left:  OrPredicate{Left: GreaterThanPredicate{Column: ts, Value: Int64Value(from)}, Right: EqualPredicate{Column: ts, Value: Int64Value(from)}},
			Right: LessThanPredicate{Column: ts, Value: Int64Value(to)},
		}
	}

	// The timestamps are uniform in [0, 100000). The needle "123x" is in about 0.1% of the lines. The needle "x" is in every line.
	var (
		wideTime   = timeRange(1000, 99_000)
		narrowTime = timeRange(49_000, 51_000)
		streams    = InPredicate{Column: stream, Values: NewMemoizedInt64ValueSetOf(1, 5, 9, 100, 300, 500)}
		manyStream = InPredicate{Column: stream, Values: NewMemoizedInt64ValueSetOf(streamRange(0, 50)...)}
		rare       = containsNeedle(msg, "123x")
		everything = containsNeedle(msg, "x")
		labelEqual = EqualPredicate{Column: label, Value: firstBinaryValue(b, dset, label)}
	)

	cases := []struct {
		name       string
		projected  []Column
		predicates []Predicate
		lazy       bool
	}{
		{name: "full-scan"},
		{name: "streams", predicates: []Predicate{streams}},
		{name: "streams-5-percent", predicates: []Predicate{manyStream}},
		{name: "streams+time", predicates: []Predicate{streams, wideTime}},
		{name: "time-keeps-98-percent", predicates: []Predicate{wideTime}},
		{name: "time-keeps-2-percent", predicates: []Predicate{narrowTime}},
		{name: "streams+time+filter-rare", predicates: []Predicate{streams, wideTime, rare}},
		{name: "time+filter-keeps-all", predicates: []Predicate{wideTime, everything}},
		{name: "time+filter-0.1-percent", predicates: []Predicate{wideTime, rare}},
		{name: "time+label-equal", predicates: []Predicate{wideTime, labelEqual}},
		{name: "streams-5-percent+narrow-projection", projected: []Column{msg}, predicates: []Predicate{manyStream}},
		{name: "time-keeps-2-percent+narrow-projection", projected: []Column{msg, label}, predicates: []Predicate{narrowTime}},
		{name: "streams-5-percent+lazy-download", predicates: []Predicate{manyStream}, lazy: true},
		{name: "time-keeps-2-percent+lazy-download", predicates: []Predicate{narrowTime}, lazy: true},
	}

	for _, tc := range cases {
		projected := tc.projected
		if projected == nil {
			projected = cols
		}
		opts := RowReaderOptions{
			Dataset:           dset,
			ProjectedColumns:  projected,
			Predicates:        tc.predicates,
			PrefetchAllOnOpen: !tc.lazy,
		}

		// Both readers must return the same number of rows, or the comparison of
		// their times means nothing. Count them once, and only for a case that runs.
		wantRows := sync.OnceValue(func() int { return countRows(b, opts) })

		b.Run(tc.name+"/row", func(b *testing.B) {
			b.ReportAllocs()
			want := wantRows()
			rows := make([]Row, 1024)
			b.ResetTimer()
			for b.Loop() {
				r := NewRowReader(opts)
				require.NoError(b, r.Open(b.Context()))

				var total int
				for {
					n, err := r.Read(b.Context(), rows)
					total += n
					if errors.Is(err, io.EOF) {
						break
					}
					require.NoError(b, err)
				}
				_ = r.Close()
				require.Equal(b, want, total)
				b.ReportMetric(float64(total), "rows/op")
			}
		})

		b.Run(tc.name+"/record-batch", func(b *testing.B) {
			b.ReportAllocs()
			want := wantRows()
			var alloc memory.Allocator
			b.ResetTimer()
			for b.Loop() {
				r := NewRowReader(opts)
				require.NoError(b, r.Open(b.Context()))

				var total int64
				for {
					alloc.Reclaim()
					batch, err := r.ReadRecordBatch(b.Context(), &alloc, 1024)
					total += batch.NumRows()
					if errors.Is(err, io.EOF) {
						break
					}
					require.NoError(b, err)
				}
				_ = r.Close()
				require.Equal(b, want, int(total))
				b.ReportMetric(float64(total), "rows/op")
			}
		})
	}
}

// requireSameRows reads opts with Read and with ReadRecordBatch, for every batch size,
// and requires both to return the same rows.
func requireSameRows(t *testing.T, opts RowReaderOptions) {
	t.Helper()

	for _, batchSize := range recordBatchTestSizes {
		want := readAllRowsAsText(t, opts, batchSize)
		got := readAllRecordBatches(t.Context(), t, opts, batchSize)
		require.Equal(t, want, got, "batch size %d", batchSize)
	}
}

func readAllRowsAsText(t testing.TB, opts RowReaderOptions, batchSize int) [][]string {
	t.Helper()

	r := NewRowReader(opts)
	t.Cleanup(func() { require.NoError(t, r.Close()) })
	require.NoError(t, r.Open(t.Context()))

	var (
		out  [][]string
		rows = make([]Row, batchSize)
	)
	for {
		n, err := r.Read(t.Context(), rows)
		for _, row := range rows[:n] {
			out = append(out, rowText(row))
		}
		if errors.Is(err, io.EOF) {
			return out
		}
		require.NoError(t, err)
	}
}

func rowText(row Row) []string {
	text := make([]string, len(row.Values))
	for i, v := range row.Values {
		text[i] = valueText(v)
	}
	return text
}

// readAllRecordBatches opens a RowReader and reads it to the end with ReadRecordBatch, as
// text.
func readAllRecordBatches(ctx context.Context, t *testing.T, opts RowReaderOptions, batchSize int) [][]string {
	t.Helper()

	r := NewRowReader(opts)
	t.Cleanup(func() { require.NoError(t, r.Close()) })
	require.NoError(t, r.Open(ctx))
	return readRemainingRecordBatches(ctx, t, r, batchSize)
}

// readAlternating reads an open RowReader to the end, as text. It alternates
// calls of Read of readSize rows and calls of ReadRecordBatch of batchSize rows.
func readAlternating(t *testing.T, r *RowReader, readSize, batchSize int) [][]string {
	t.Helper()

	var (
		got   [][]string
		alloc memory.Allocator
		rows  = make([]Row, readSize)
	)
	for useReadRecordBatch := false; ; useReadRecordBatch = !useReadRecordBatch {
		var err error
		if useReadRecordBatch {
			alloc.Reclaim()

			var batch *columnar.RecordBatch
			batch, err = r.ReadRecordBatch(t.Context(), &alloc, batchSize)
			got = appendRecordBatchText(t, got, batch)
		} else {
			var n int
			n, err = r.Read(t.Context(), rows)
			for _, row := range rows[:n] {
				got = append(got, rowText(row))
			}
		}

		if errors.Is(err, io.EOF) {
			return got
		}
		require.NoError(t, err)
	}
}

// readRemainingRecordBatches reads an open RowReader to the end with ReadRecordBatch, as
// text. It checks that every record batch is not nil, has one to batchSize rows
// when the error is nil, and has no more than batchSize rows. It reclaims the
// allocator before each read, so a record batch that used memory of an earlier
// record batch would read corrupted data.
func readRemainingRecordBatches(ctx context.Context, t *testing.T, r *RowReader, batchSize int) [][]string {
	t.Helper()

	var (
		out   [][]string
		alloc memory.Allocator
	)
	for {
		alloc.Reclaim()
		batch, err := r.ReadRecordBatch(ctx, &alloc, batchSize)
		require.NotNil(t, batch)
		if err == nil {
			require.Greater(t, batch.NumRows(), int64(0), "a batch must have a row")
		} else if !errors.Is(err, io.EOF) {
			require.NoError(t, err)
		}
		require.LessOrEqual(t, batch.NumRows(), int64(batchSize))

		out = appendRecordBatchText(t, out, batch)
		if errors.Is(err, io.EOF) {
			return out
		}
	}
}

// appendRecordBatchText appends the rows of the record batch to out, as text.
func appendRecordBatchText(t *testing.T, out [][]string, batch *columnar.RecordBatch) [][]string {
	t.Helper()

	for i := range int(batch.NumRows()) {
		text := make([]string, batch.NumCols())
		for j := range text {
			text[j] = arrayText(t, batch.Column(int64(j)), i)
		}
		out = append(out, text)
	}
	return out
}

func valueText(v Value) string {
	switch v.Type() {
	case datasetmd.PHYSICAL_TYPE_INT64:
		return strconv.FormatInt(v.Int64(), 10)
	case datasetmd.PHYSICAL_TYPE_UINT64:
		return strconv.FormatUint(v.Uint64(), 10)
	case datasetmd.PHYSICAL_TYPE_BINARY:
		return string(v.Binary())
	default:
		return nilValueText
	}
}

func arrayText(t *testing.T, arr columnar.Array, i int) string {
	t.Helper()

	require.Less(t, i, arr.Len(), "array of %d elements has no element %d", arr.Len(), i)
	if arr.IsNull(i) {
		return nilValueText
	}
	switch arr := arr.(type) {
	case *columnar.Number[int64]:
		return strconv.FormatInt(arr.Get(i), 10)
	case *columnar.Number[uint64]:
		return strconv.FormatUint(arr.Get(i), 10)
	case *columnar.UTF8:
		return string(arr.Get(i))
	default:
		t.Fatalf("unexpected array type %T", arr)
		return ""
	}
}

// generatedColumns returns the column configs of a generated dataset: a stream ID,
// a timestamp, a message, a sparse string label and a sparse int64 count.
func generatedColumns(streamCardinality, streamRunLength int64) []generatorColumnConfig {
	return []generatorColumnConfig{
		{
			Tag:               "stream",
			Type:              ColumnType{Physical: datasetmd.PHYSICAL_TYPE_INT64, Logical: "stream_id"},
			Encoding:          datasetmd.ENCODING_TYPE_DELTA,
			Compression:       datasetmd.COMPRESSION_TYPE_NONE,
			CardinalityTarget: streamCardinality,
			RunLength:         streamRunLength,
		},
		{
			Tag:               "ts",
			Type:              ColumnType{Physical: datasetmd.PHYSICAL_TYPE_INT64, Logical: "number"},
			Encoding:          datasetmd.ENCODING_TYPE_DELTA,
			Compression:       datasetmd.COMPRESSION_TYPE_ZSTD,
			CardinalityTarget: 100_000,
		},
		{
			Tag:               "msg",
			Type:              ColumnType{Physical: datasetmd.PHYSICAL_TYPE_BINARY, Logical: "string"},
			Encoding:          datasetmd.ENCODING_TYPE_PLAIN,
			Compression:       datasetmd.COMPRESSION_TYPE_ZSTD,
			AvgSize:           40,
			CardinalityTarget: 1000,
		},
		{
			Tag:               "label",
			Type:              ColumnType{Physical: datasetmd.PHYSICAL_TYPE_BINARY, Logical: "label"},
			Encoding:          datasetmd.ENCODING_TYPE_PLAIN,
			Compression:       datasetmd.COMPRESSION_TYPE_SNAPPY,
			AvgSize:           8,
			CardinalityTarget: 10,
			SparsityRate:      0.4,
		},
		{
			Tag:               "count",
			Type:              ColumnType{Physical: datasetmd.PHYSICAL_TYPE_INT64, Logical: "number"},
			Encoding:          datasetmd.ENCODING_TYPE_DELTA,
			Compression:       datasetmd.COMPRESSION_TYPE_NONE,
			CardinalityTarget: 50,
			SparsityRate:      0.5,
		},
	}
}

// containsNeedle returns a func predicate that keeps the values of column that
// contain needle. It sets KeepBatch.
func containsNeedle(column Column, needle string) FuncPredicate {
	return FuncPredicate{
		Column: column,
		Keep:   func(_ Column, v Value) bool { return bytes.Contains(v.Binary(), []byte(needle)) },
		KeepBatch: func(_ Column, values *columnar.UTF8, dst []int32) []int32 {
			for i := range values.Len() {
				if bytes.Contains(values.Get(i), []byte(needle)) {
					dst = append(dst, int32(i))
				}
			}
			return dst
		},
	}
}

// countRows returns the number of rows that a RowReader reads for opts.
func countRows(t testing.TB, opts RowReaderOptions) int {
	t.Helper()

	r := NewRowReader(opts)
	t.Cleanup(func() { require.NoError(t, r.Close()) })
	require.NoError(t, r.Open(t.Context()))

	var (
		total int
		rows  = make([]Row, 1024)
	)
	for {
		n, err := r.Read(t.Context(), rows)
		total += n
		if errors.Is(err, io.EOF) {
			return total
		}
		require.NoError(t, err)
	}
}

// countDownloads reads opts to the end in steps of size rows, with Read or, if
// asRecordBatches is true, with ReadRecordBatch. It returns the dataset that served the page
// downloads. The reader batches its downloads up to target bytes of compressed
// pages, so a small target makes it download pages lazily.
func countDownloads(t *testing.T, opts RowReaderOptions, target, size int, asRecordBatches bool) *countingDataset {
	t.Helper()

	counting := &countingDataset{Dataset: opts.Dataset}
	opts.Dataset = counting

	r := NewRowReader(opts)
	t.Cleanup(func() { require.NoError(t, r.Close()) })
	require.NoError(t, r.Open(t.Context()))
	r.dl.targetCompressedBytes = target

	var (
		rows  = make([]Row, size)
		alloc memory.Allocator
	)
	for {
		var err error
		if asRecordBatches {
			alloc.Reclaim()
			_, err = r.ReadRecordBatch(t.Context(), &alloc, size)
		} else {
			_, err = r.Read(t.Context(), rows)
		}
		if errors.Is(err, io.EOF) {
			return counting
		}
		require.NoError(t, err)
	}
}

// firstBinaryValue returns the first value of a binary column that is neither nil nor empty.
func firstBinaryValue(t testing.TB, dset Dataset, column Column) Value {
	t.Helper()

	r := NewRowReader(RowReaderOptions{Dataset: dset, ProjectedColumns: []Column{column}})
	t.Cleanup(func() { require.NoError(t, r.Close()) })
	require.NoError(t, r.Open(t.Context()))

	rows := make([]Row, 64)
	for {
		n, err := r.Read(t.Context(), rows)
		for _, row := range rows[:n] {
			if !row.Values[0].IsNil() && len(row.Values[0].Binary()) > 0 {
				return BinaryValue(bytes.Clone(row.Values[0].Binary()))
			}
		}
		require.NoError(t, err)
	}
}

// streamRange returns the stream IDs from the start up to, and not including,
// the end.
func streamRange(start, end int64) []int64 {
	ids := make([]int64, 0, end-start)
	for id := start; id < end; id++ {
		ids = append(ids, id)
	}
	return ids
}

// requireSameStats reads opts with Read and with ReadRecordBatch, with the same batch
// size, and requires both to record the same read statistics.
func requireSameStats(t *testing.T, opts RowReaderOptions, batchSize int) {
	t.Helper()

	want := readStats(t, opts, batchSize, false)
	got := readStats(t, opts, batchSize, true)
	for _, stat := range parityStats {
		require.Equal(t, want[stat], got[stat], stat)
	}
}

// readStats reads opts to the end in steps of batchSize rows, with Read or, if
// recordBatches is true, with ReadRecordBatch. It returns the statistics that the
// reader recorded.
func readStats(t *testing.T, opts RowReaderOptions, batchSize int, recordBatches bool) map[string]int64 {
	t.Helper()

	return runAndCollectStats(t, func(ctx context.Context) {
		if recordBatches {
			readAllRecordBatches(ctx, t, opts, batchSize)
			return
		}

		r := NewRowReader(opts)
		t.Cleanup(func() { require.NoError(t, r.Close()) })
		require.NoError(t, r.Open(ctx))

		rows := make([]Row, batchSize)
		for {
			_, err := r.Read(ctx, rows)
			if errors.Is(err, io.EOF) {
				return
			}
			require.NoError(t, err)
		}
	})
}

// buildSortedDataset returns a dataset of the given number of rows, with these
// columns in this order: stream, ts, msg, label and count. The stream ID grows
// by one every 100 rows, so the page statistics of the stream column prune
// pages. The label and count columns are sparse, and the builders backfill their
// trailing nulls, as the logs section builder does.
func buildSortedDataset(t *testing.T, rows int) (Dataset, []Column) {
	t.Helper()

	newBuilder := func(name string, physical datasetmd.PhysicalType, logical string, encoding datasetmd.EncodingType) *ColumnBuilder {
		builder, err := NewColumnBuilder(name, BuilderOptions{
			PageSizeHint: 256,
			Type:         ColumnType{Physical: physical, Logical: logical},
			Compression:  datasetmd.COMPRESSION_TYPE_NONE,
			Encoding:     encoding,
			Statistics:   StatisticsOptions{StoreRangeStats: true},
		})
		require.NoError(t, err)
		return builder
	}

	var (
		stream = newBuilder("stream", datasetmd.PHYSICAL_TYPE_INT64, "stream_id", datasetmd.ENCODING_TYPE_DELTA)
		ts     = newBuilder("ts", datasetmd.PHYSICAL_TYPE_INT64, "number", datasetmd.ENCODING_TYPE_DELTA)
		msg    = newBuilder("msg", datasetmd.PHYSICAL_TYPE_BINARY, "string", datasetmd.ENCODING_TYPE_PLAIN)
		label  = newBuilder("label", datasetmd.PHYSICAL_TYPE_BINARY, "label", datasetmd.ENCODING_TYPE_PLAIN)
		count  = newBuilder("count", datasetmd.PHYSICAL_TYPE_INT64, "number", datasetmd.ENCODING_TYPE_DELTA)
	)
	for i := range rows {
		require.NoError(t, stream.Append(i, Int64Value(int64(i/100))))
		require.NoError(t, ts.Append(i, Int64Value(int64(i))))
		require.NoError(t, msg.Append(i, BinaryValue(fmt.Appendf(nil, "line %d x%d", i, i%13))))
		if i%3 != 0 {
			require.NoError(t, label.Append(i, BinaryValue(fmt.Appendf(nil, "l%d", i%5))))
		}
		if i%4 != 0 {
			require.NoError(t, count.Append(i, Int64Value(int64(i%50))))
		}
	}
	label.Backfill(rows)
	count.Backfill(rows)

	return memDatasetOf(t, stream, ts, msg, label, count)
}

// countingDataset is a Dataset that counts the page downloads that it serves.
type countingDataset struct {
	Dataset
	calls int // Number of ReadPages calls.
	pages int // Number of pages that the calls asked for.
	bytes int // Compressed size of the pages that the calls asked for.
}

func (d *countingDataset) ReadPages(ctx context.Context, pages []Page) result.Seq[PageData] {
	d.calls++
	d.pages += len(pages)
	for _, page := range pages {
		d.bytes += page.PageDesc().CompressedSize
	}
	return d.Dataset.ReadPages(ctx, pages)
}

// fakeColumnReader is a recordBatchColumnReader that returns the row numbers as
// an int64 column, and records the rows that each
// readUncompactedColumnBySelection call selected.
type fakeColumnReader struct {
	reads  [][]int // The selected rows of each call. A nil entry means every row.
	err    error   // The error that a call returns, if set.
	length int     // The length of the array that a call returns, if set.
}

func (s *fakeColumnReader) readUncompactedColumnBySelection(_ context.Context, _ *memory.Allocator, _ int, row int64, n int, sel selection) (columnar.Array, error) {
	if sel.isAllRows() {
		s.reads = append(s.reads, nil)
	} else {
		s.reads = append(s.reads, selectedRows(sel))
	}
	if s.err != nil {
		return nil, s.err
	}

	if s.length > 0 {
		n = s.length
	}
	values := make([]int64, n)
	for i := range values {
		values[i] = row + int64(i)
	}
	return columnar.NewNumber(values, memory.Bitmap{}), nil
}

// failingAtDataset is a Dataset whose ReadPages call number failAt fails with
// errInjectedReadPages.
type failingAtDataset struct {
	Dataset
	failAt int // Number of the ReadPages call that fails.
	calls  int // Number of ReadPages calls so far.
}

func (d *failingAtDataset) ReadPages(ctx context.Context, pages []Page) result.Seq[PageData] {
	d.calls++
	if d.calls == d.failAt {
		return result.Iter(func(func(PageData) bool) error { return errInjectedReadPages })
	}
	return d.Dataset.ReadPages(ctx, pages)
}

// failingListDataset is a Dataset whose ListPages call fails with err.
type failingListDataset struct {
	Dataset
	err error
}

func (d *failingListDataset) ListPages(context.Context, []Column) result.Seq[Pages] {
	return result.Iter(func(func(Pages) bool) error { return d.err })
}

// truncatedListDataset is a Dataset that lists one page less than each column
// that has more than one page.
type truncatedListDataset struct {
	Dataset
}

func (d *truncatedListDataset) ListPages(ctx context.Context, columns []Column) result.Seq[Pages] {
	return func(yield func(result.Result[Pages]) bool) {
		for res := range d.Dataset.ListPages(ctx, columns) {
			if pages, err := res.Value(); err == nil && len(pages) > 1 {
				res = result.Value(pages[:len(pages)-1])
			}
			if !yield(res) {
				return
			}
		}
	}
}

// buildLongLineDataset returns a dataset with the given number of rows and three columns, in this
// order: a stream ID, a timestamp and a message of about lineSize bytes. A page holds at most about
// pageSizeHint bytes.
func buildLongLineDataset(t *testing.T, rows int, lineSize int64, pageSizeHint int) (Dataset, []Column) {
	t.Helper()

	columns := generatedColumns(100, 1)[:3]
	columns[2].AvgSize = lineSize
	for i := range columns {
		columns[i].Compression = datasetmd.COMPRESSION_TYPE_NONE
	}

	generator := DatasetGenerator{RowCount: rows, PageSizeHint: pageSizeHint, Columns: columns}
	return generator.Build(t, 7)
}

// pageRowCounts returns the number of rows of each page of column, in order.
func pageRowCounts(t *testing.T, column Column) []int {
	t.Helper()

	var counts []int
	for result := range column.ListPages(t.Context()) {
		page, err := result.Value()
		require.NoError(t, err)
		counts = append(counts, page.PageDesc().RowCount)
	}
	return counts
}

// recordBatchSizes returns the number of rows of each record batch that ReadRecordBatch returns,
// in order, when it reads the dataset of opts with batches of at most batchSize rows.
func recordBatchSizes(t *testing.T, opts RowReaderOptions, batchSize int) []int {
	t.Helper()

	r := NewRowReader(opts)
	t.Cleanup(func() { require.NoError(t, r.Close()) })
	require.NoError(t, r.Open(t.Context()))

	var (
		sizes []int
		alloc memory.Allocator
	)
	for {
		alloc.Reclaim()
		batch, err := r.ReadRecordBatch(t.Context(), &alloc, batchSize)
		if errors.Is(err, io.EOF) {
			return sizes
		}
		require.NoError(t, err)
		sizes = append(sizes, int(batch.NumRows()))
	}
}

// fullBatchSizes returns the sizes of the record batches of a dataset with the given number of
// rows, when no page end limits them: batches of batchSize rows, and a last batch with the rows that
// remain.
func fullBatchSizes(rows, batchSize int) []int {
	var sizes []int
	for ; rows > 0; rows -= batchSize {
		sizes = append(sizes, min(rows, batchSize))
	}
	return sizes
}
