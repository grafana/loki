package dataset

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math/rand"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/memory"
)

// batchTestBatchSizes covers a batch of one row, batches that split pages,
// and a batch that holds the whole dataset.
var batchTestBatchSizes = []int{1, 3, 7, 64, 10_000}

func TestBatchReader(t *testing.T) {
	t.Run("reads every row of every column when there are no predicates", func(t *testing.T) {
		dset, columns := buildTestDataset(t)
		requireSameRows(t, RowReaderOptions{Dataset: dset, ProjectedColumns: columns})
	})

	t.Run("returns the projected columns in the order of the projection", func(t *testing.T) {
		dset, columns := buildTestDataset(t)
		requireSameRows(t, RowReaderOptions{Dataset: dset, ProjectedColumns: []Column{columns[3], columns[0]}})
	})

	t.Run("filters rows with the predicates of the people test dataset", func(t *testing.T) {
		dset, columns := buildTestDataset(t)
		first, middle, last, year := columns[0], columns[1], columns[2], columns[3]

		for name, predicates := range map[string][]Predicate{
			"equal":                  {EqualPredicate{Column: first, Value: BinaryValue([]byte("Alice"))}},
			"equal empty middle":     {EqualPredicate{Column: middle, Value: BinaryValue(nil)}},
			"equal non empty middle": {EqualPredicate{Column: middle, Value: BinaryValue([]byte("Marie"))}},
			"greater than":           {GreaterThanPredicate{Column: year, Value: Int64Value(1983)}},
			"less than":              {LessThanPredicate{Column: year, Value: Int64Value(1983)}},
			"less than on binary":    {LessThanPredicate{Column: last, Value: BinaryValue([]byte("Dz"))}},
			"in":                     {InPredicate{Column: year, Values: NewInt64ValueSetOf(1980, 1990, 1)}},
			"in memoized":            {InPredicate{Column: year, Values: NewMemoizedInt64ValueSetOf(1980, 1990, 1)}},
			"in binary":              {InPredicate{Column: first, Values: NewBinaryValueSet([]Value{BinaryValue([]byte("Eve")), BinaryValue([]byte("Bob"))})}},
			"false":                  {FalsePredicate{}},
			"not":                    {NotPredicate{Inner: GreaterThanPredicate{Column: year, Value: Int64Value(1983)}}},
			"not nil":                {NotPredicate{Inner: nil}},
			"not true":               {NotPredicate{Inner: TruePredicate{}}},
			"and": {AndPredicate{
				Left:  GreaterThanPredicate{Column: year, Value: Int64Value(1979)},
				Right: LessThanPredicate{Column: year, Value: Int64Value(1988)},
			}},
			"or": {OrPredicate{
				Left:  EqualPredicate{Column: first, Value: BinaryValue([]byte("John"))},
				Right: EqualPredicate{Column: year, Value: Int64Value(1987)},
			}},
			"greater or equal": {OrPredicate{
				Left:  GreaterThanPredicate{Column: year, Value: Int64Value(1983)},
				Right: EqualPredicate{Column: year, Value: Int64Value(1983)},
			}},
			"less or equal": {OrPredicate{
				Left:  EqualPredicate{Column: year, Value: Int64Value(1983)},
				Right: LessThanPredicate{Column: year, Value: Int64Value(1983)},
			}},
			"less or equal on a nullable column": {OrPredicate{
				Left:  LessThanPredicate{Column: middle, Value: BinaryValue([]byte("M"))},
				Right: EqualPredicate{Column: middle, Value: BinaryValue([]byte("M"))},
			}},
			"not of or": {NotPredicate{Inner: OrPredicate{
				Left:  EqualPredicate{Column: first, Value: BinaryValue([]byte("John"))},
				Right: GreaterThanPredicate{Column: year, Value: Int64Value(1985)},
			}}},
			"two predicates": {
				GreaterThanPredicate{Column: year, Value: Int64Value(1979)},
				LessThanPredicate{Column: last, Value: BinaryValue([]byte("M"))},
			},
			"func": {FuncPredicate{
				Column: first,
				Keep:   func(_ Column, v Value) bool { return bytes.ContainsAny(v.Binary(), "ae") },
			}},
			"func batch": {FuncPredicate{
				Column: first,
				Keep:   func(_ Column, v Value) bool { return bytes.ContainsAny(v.Binary(), "ae") },
				KeepBatch: func(_ Column, values *columnar.UTF8, dst []int32) []int32 {
					for i := range values.Len() {
						if bytes.ContainsAny(values.Get(i), "ae") {
							dst = append(dst, int32(i))
						}
					}
					return dst
				},
			}},
			"func on nullable column": {FuncPredicate{
				Column: middle,
				Keep:   func(_ Column, v Value) bool { return v.IsNil() || len(v.Binary()) > 4 },
			}},
			"selective then func batch": {
				GreaterThanPredicate{Column: year, Value: Int64Value(1987)},
				FuncPredicate{
					Column: first,
					Keep:   func(_ Column, v Value) bool { return len(v.Binary()) > 3 },
					KeepBatch: func(_ Column, values *columnar.UTF8, dst []int32) []int32 {
						for i := range values.Len() {
							if len(values.Get(i)) > 3 {
								dst = append(dst, int32(i))
							}
						}
						return dst
					},
				},
			},
			"equal on a nil value": {EqualPredicate{Column: middle, Value: Value{}}},
		} {
			t.Run(name, func(t *testing.T) {
				requireSameRows(t, RowReaderOptions{Dataset: dset, ProjectedColumns: columns, Predicates: predicates})
				requireSameRows(t, RowReaderOptions{Dataset: dset, ProjectedColumns: []Column{columns[0]}, Predicates: predicates})
			})
		}
	})

	t.Run("keeps every row for a constant true predicate", func(t *testing.T) {
		dset, columns := buildTestDataset(t)
		want := readAllRowsAsText(t, RowReaderOptions{Dataset: dset, ProjectedColumns: columns}, 3)

		for _, p := range []Predicate{TruePredicate{}, nil, NotPredicate{Inner: FalsePredicate{}}} {
			got := readAllBatches(t.Context(), t, RowReaderOptions{Dataset: dset, ProjectedColumns: columns, Predicates: []Predicate{p}}, 3)
			require.Equal(t, want, got)
		}
	})

	t.Run("reads predicate columns that are not projected", func(t *testing.T) {
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

	t.Run("reads generated datasets with nulls and many pages", func(t *testing.T) {
		generator := DatasetGenerator{
			RowCount:     5000,
			PageSizeHint: 4096,
			Columns: []generatorColumnConfig{
				{
					Tag:               "stream",
					Type:              ColumnType{Physical: datasetmd.PHYSICAL_TYPE_INT64, Logical: "stream_id"},
					Encoding:          datasetmd.ENCODING_TYPE_DELTA,
					Compression:       datasetmd.COMPRESSION_TYPE_NONE,
					CardinalityTarget: 40,
					RunLength:         50,
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
			},
		}
		dset, columns := generator.Build(t, 1)
		stream, ts, msg, label := columns[0], columns[1], columns[2], columns[3]

		needle := []byte("7x")
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
			"stream in": {InPredicate{Column: stream, Values: NewMemoizedInt64ValueSetOf(3, 7, 11, 39)}},
			"stream in then time range": {
				InPredicate{Column: stream, Values: NewInt64ValueSetOf(3, 7, 11, 39)},
				AndPredicate{
					Left:  OrPredicate{Left: GreaterThanPredicate{Column: ts, Value: Int64Value(20_000)}, Right: EqualPredicate{Column: ts, Value: Int64Value(20_000)}},
					Right: LessThanPredicate{Column: ts, Value: Int64Value(80_000)},
				},
			},
			"line filter": {containsNeedle},
			"selective then line filter": {
				InPredicate{Column: stream, Values: NewInt64ValueSetOf(5)},
				containsNeedle,
			},
			"line filter then label": {
				containsNeedle,
				EqualPredicate{Column: label, Value: BinaryValue([]byte("not there"))},
			},
			"label empty": {EqualPredicate{Column: label, Value: BinaryValue(nil)}},
			"label less":  {LessThanPredicate{Column: label, Value: BinaryValue([]byte("5"))}},
			"not label":   {NotPredicate{Inner: EqualPredicate{Column: label, Value: BinaryValue([]byte("1"))}}},
			"or across columns": {OrPredicate{
				Left:  InPredicate{Column: stream, Values: NewInt64ValueSetOf(1)},
				Right: containsNeedle,
			}},
			"nothing passes": {EqualPredicate{Column: stream, Value: Int64Value(-5)}},
		} {
			t.Run(name, func(t *testing.T) {
				requireSameRows(t, RowReaderOptions{Dataset: dset, ProjectedColumns: columns, Predicates: predicates})
				requireSameRows(t, RowReaderOptions{Dataset: dset, ProjectedColumns: []Column{ts, msg}, Predicates: predicates})
				requireSameRows(t, RowReaderOptions{Dataset: dset, ProjectedColumns: []Column{label}, Predicates: predicates, PrefetchAllOnOpen: true})
			})
		}
	})

	t.Run("returns io.EOF and an empty batch of typed arrays at the end of the dataset", func(t *testing.T) {
		dset, columns := buildTestDataset(t)

		r := NewBatchReader(BatchReaderOptions{Dataset: dset, ProjectedColumns: columns})
		defer r.Close()
		require.NoError(t, r.Open(t.Context()))

		var alloc memory.Allocator
		for {
			_, err := r.Read(t.Context(), &alloc, 4)
			if err != nil {
				require.ErrorIs(t, err, io.EOF)
				break
			}
		}

		batch, err := r.Read(t.Context(), &alloc, 4)
		require.ErrorIs(t, err, io.EOF)
		require.Equal(t, int64(0), batch.NumRows())
		require.Equal(t, int64(len(columns)), batch.NumCols())
		require.IsType(t, &columnar.UTF8{}, batch.Column(0))
		require.IsType(t, &columnar.Number[int64]{}, batch.Column(3))
	})

	t.Run("returns an error when read before open", func(t *testing.T) {
		dset, columns := buildTestDataset(t)
		r := NewBatchReader(BatchReaderOptions{Dataset: dset, ProjectedColumns: columns})
		defer r.Close()

		var alloc memory.Allocator
		_, err := r.Read(t.Context(), &alloc, 1)
		require.ErrorIs(t, err, errRowReaderNotOpen)
	})

	t.Run("returns an error when a predicate names a column outside the dataset", func(t *testing.T) {
		dset, columns := buildTestDataset(t)
		other, _ := buildTestDataset(t)
		_ = other

		r := NewBatchReader(BatchReaderOptions{
			Dataset:          dset,
			ProjectedColumns: columns,
			Predicates:       []Predicate{EqualPredicate{Column: nil, Value: Int64Value(1)}},
		})
		defer r.Close()
		require.Error(t, r.Open(t.Context()))
	})

	t.Run("returns an error when a batch predicate keeps indexes out of order", func(t *testing.T) {
		dset, columns := buildTestDataset(t)
		r := NewBatchReader(BatchReaderOptions{
			Dataset:          dset,
			ProjectedColumns: columns,
			Predicates: []Predicate{FuncPredicate{
				Column:    columns[0],
				Keep:      func(Column, Value) bool { return true },
				KeepBatch: func(_ Column, _ *columnar.UTF8, dst []int32) []int32 { return append(dst, 1, 0) },
			}},
		})
		defer r.Close()
		require.NoError(t, r.Open(t.Context()))

		var alloc memory.Allocator
		_, err := r.Read(t.Context(), &alloc, 10)
		require.ErrorContains(t, err, "ascending")
	})

	t.Run("records the read statistics", func(t *testing.T) {
		dset, columns := buildTestDataset(t)
		opts := RowReaderOptions{
			Dataset:          dset,
			ProjectedColumns: columns,
			Predicates: []Predicate{
				GreaterThanPredicate{Column: columns[3], Value: Int64Value(1985)},
				EqualPredicate{Column: columns[0], Value: BinaryValue([]byte("Alice"))},
			},
		}

		stats := runAndCollectStats(t, func(ctx context.Context) {
			readAllBatches(ctx, t, opts, 3)
		})

		require.Equal(t, int64(len(basicReaderTestData)), stats[dataobj.StatDatasetMaxRows.Name()])
		require.Equal(t, int64(3), stats[dataobj.StatDatasetRowsAfterPruning.Name()])
		require.Equal(t, int64(3), stats[dataobj.StatDatasetPrimaryRowsRead.Name()])
		require.Equal(t, int64(1), stats[dataobj.StatDatasetSecondaryRowsRead.Name()])
	})
}

// requireSameRows reads opts with a RowReader and with a BatchReader, for every
// batch size, and requires both to return the same rows.
func requireSameRows(t *testing.T, opts RowReaderOptions) {
	t.Helper()

	for _, batchSize := range batchTestBatchSizes {
		want := readAllRowsAsText(t, opts, batchSize)
		got := readAllBatches(t.Context(), t, opts, batchSize)
		require.Equal(t, want, got, "batch size %d", batchSize)
		t.Logf("batch size %d: %d rows", batchSize, len(want))
	}
}

func readAllRowsAsText(t *testing.T, opts RowReaderOptions, batchSize int) [][]string {
	t.Helper()

	r := NewRowReader(opts)
	defer r.Close()
	require.NoError(t, r.Open(t.Context()))

	var (
		out  [][]string
		rows = make([]Row, batchSize)
	)
	for {
		n, err := r.Read(t.Context(), rows)
		for _, row := range rows[:n] {
			text := make([]string, len(row.Values))
			for i, v := range row.Values {
				text[i] = valueText(v)
			}
			out = append(out, text)
		}
		if errors.Is(err, io.EOF) {
			return out
		}
		require.NoError(t, err)
	}
}

func readAllBatches(ctx context.Context, t *testing.T, opts BatchReaderOptions, batchSize int) [][]string {
	t.Helper()

	r := NewBatchReader(opts)
	defer r.Close()
	require.NoError(t, r.Open(ctx))

	var (
		out   [][]string
		alloc memory.Allocator
	)
	for {
		alloc.Reclaim()
		batch, err := r.Read(ctx, &alloc, batchSize)
		if err != nil && !errors.Is(err, io.EOF) {
			require.NoError(t, err)
		}
		if batch != nil {
			if err == nil {
				require.Greater(t, batch.NumRows(), int64(0), "a batch must have a row")
			}
			require.LessOrEqual(t, batch.NumRows(), int64(batchSize))
			for i := range int(batch.NumRows()) {
				text := make([]string, batch.NumCols())
				for j := range text {
					text[j] = arrayText(t, batch.Column(int64(j)), i)
				}
				out = append(out, text)
			}
		}
		if errors.Is(err, io.EOF) {
			return out
		}
	}
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

	require.Equal(t, i < arr.Len(), true, "array of %d elements has no element %d", arr.Len(), i)
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

func BenchmarkBatchReader(b *testing.B) {
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

	dset, cols := generator.Build(b, rand.Int63())
	stream, ts, msg, label := cols[0], cols[1], cols[2], cols[3]

	// The needle "123x" is in about 0.1% of the lines. The needle "x" is in every line.
	filter := func(needle []byte) FuncPredicate {
		return FuncPredicate{
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
	}

	timeRange := AndPredicate{
		Left:  OrPredicate{Left: GreaterThanPredicate{Column: ts, Value: Int64Value(1000)}, Right: EqualPredicate{Column: ts, Value: Int64Value(1000)}},
		Right: LessThanPredicate{Column: ts, Value: Int64Value(99_000)},
	}
	streams := InPredicate{Column: stream, Values: NewMemoizedInt64ValueSetOf(1, 5, 9, 100, 300, 500)}

	cases := []struct {
		name       string
		predicates []Predicate
	}{
		{name: "full-scan"},
		{name: "streams", predicates: []Predicate{streams}},
		{name: "streams+time", predicates: []Predicate{streams, timeRange}},
		{name: "time-only", predicates: []Predicate{timeRange}},
		{name: "streams+time+filter-0.1-percent", predicates: []Predicate{streams, timeRange, filter([]byte("123x"))}},
		{name: "time+filter-keeps-all", predicates: []Predicate{timeRange, filter([]byte("x"))}},
		{name: "time+filter-0.1-percent", predicates: []Predicate{timeRange, filter([]byte("123x"))}},
		{name: "label-equal", predicates: []Predicate{timeRange, EqualPredicate{Column: label, Value: BinaryValue([]byte("17"))}}},
	}

	for _, tc := range cases {
		opts := RowReaderOptions{
			Dataset:           dset,
			ProjectedColumns:  cols,
			Predicates:        tc.predicates,
			PrefetchAllOnOpen: true,
		}

		b.Run(tc.name+"/row", func(b *testing.B) {
			b.ReportAllocs()
			rows := make([]Row, 1024)
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
				b.ReportMetric(float64(total), "rows/op")
			}
		})

		b.Run(tc.name+"/batch", func(b *testing.B) {
			b.ReportAllocs()
			var alloc memory.Allocator
			for b.Loop() {
				r := NewBatchReader(opts)
				require.NoError(b, r.Open(b.Context()))

				var total int64
				for {
					alloc.Reclaim()
					batch, err := r.Read(b.Context(), &alloc, 1024)
					if batch != nil {
						total += batch.NumRows()
					}
					if errors.Is(err, io.EOF) {
						break
					}
					require.NoError(b, err)
				}
				_ = r.Close()
				b.ReportMetric(float64(total), "rows/op")
			}
		})
	}
}
