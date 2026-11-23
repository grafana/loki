package dataset

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/rangeset"
	"github.com/grafana/loki/v3/pkg/memory"
)

// evalTestRows is the number of rows of the dataset of the evalRecordBatch tests.
const evalTestRows = 37

// evalTestRecordBatches are the record batches that the evalRecordBatch tests read: the whole
// dataset, a record batch in the middle, one at the end and one of one row.
var evalTestRecordBatches = []struct {
	row int64
	n   int
}{{0, evalTestRows}, {11, 17}, {30, 7}, {0, 1}}

func TestCompiledPredicate_evalRecordBatch(t *testing.T) {
	t.Run("passes exactly the selected rows that evalRow passes for every predicate that reads at most one column", func(t *testing.T) {
		data := buildEvalTestData(t)
		requireEvalRecordBatchMatchesEvalRow(t, data, evalTestPredicates(data), rand.New(rand.NewSource(1)))
	})

	t.Run("passes exactly the selected rows that evalRow passes for random trees of and, or and not", func(t *testing.T) {
		data := buildEvalTestData(t)
		predicates := evalTestPredicates(data)

		rng := rand.New(rand.NewSource(42))
		trees := make([]Predicate, 0, 800)
		for range cap(trees) {
			trees = append(trees, randomPredicateTree(rng, predicates, 3))
		}
		requireEvalRecordBatchMatchesEvalRow(t, data, trees, rng)
	})

	t.Run("panics like evalRow when the constant has the type of another column", func(t *testing.T) {
		data := buildEvalTestData(t)
		r := openEvalTestRecordBatch(t, data, false, evalTestRecordBatches[0].row, evalTestRecordBatches[0].n)

		for name, p := range map[string]Predicate{
			"an int64 column compared with a binary value": EqualPredicate{Column: data.columns[0], Value: BinaryValue([]byte("x"))},
			"a binary column compared with an int64 value": GreaterThanPredicate{Column: data.columns[1], Value: Int64Value(1)},
		} {
			t.Run(name, func(t *testing.T) {
				compiled, err := compilePredicate(p, r.columnsLookup)
				require.NoError(t, err)

				want := recoverPanic(func() {
					for i := range evalTestRows {
						compiled.evalRow(evalTestRow(data, i))
					}
				})
				got := recoverPanic(func() {
					_, _ = compiled.evalRecordBatch(t.Context(), &r.currRecordBatch, newSelectionAllRows(evalTestRecordBatches[0].n))
				})
				require.NotNil(t, want)
				require.Equal(t, want, got)
			})
		}
	})

	t.Run("does not modify the selection it receives", func(t *testing.T) {
		data := buildEvalTestData(t)
		r := openEvalTestRecordBatch(t, data, false, evalTestRecordBatches[0].row, evalTestRecordBatches[0].n)

		var alloc memory.Allocator
		for _, p := range evalTestPredicates(data) {
			compiled, err := compilePredicate(p, r.columnsLookup)
			require.NoError(t, err)

			for _, tc := range evalTestSelections(&alloc, evalTestRows, rand.New(rand.NewSource(3))) {
				before := selectionBits(tc.sel, evalTestRows)
				resetEvalTestRecordBatch(r, evalTestRecordBatches[0].row, evalTestRecordBatches[0].n)

				_, err := compiled.evalRecordBatch(t.Context(), &r.currRecordBatch, tc.sel)
				require.NoError(t, err)
				require.Equal(t, before, selectionBits(tc.sel, evalTestRows), "selection %s", tc.name)
			}
		}
	})
}

// requireEvalRecordBatchMatchesEvalRow requires that evalRecordBatch passes, for every
// record batch, prefetch mode and selection, exactly the selected rows that
// evalRow passes.
func requireEvalRecordBatchMatchesEvalRow(t *testing.T, data evalTestData, predicates []Predicate, rng *rand.Rand) {
	t.Helper()

	for _, batch := range evalTestRecordBatches {
		for _, prefetch := range []bool{false, true} {
			r := openEvalTestRecordBatch(t, data, prefetch, batch.row, batch.n)

			for i, p := range predicates {
				compiled, err := compilePredicate(p, r.columnsLookup)
				require.NoError(t, err)

				var alloc memory.Allocator
				for _, tc := range evalTestSelections(&alloc, batch.n, rng) {
					resetEvalTestRecordBatch(r, batch.row, batch.n)

					got, err := compiled.evalRecordBatch(t.Context(), &r.currRecordBatch, tc.sel)
					require.NoError(t, err)
					require.Equal(t, batch.n, got.n, "predicate %d selection %s", i, tc.name)
					if !got.isAllRows() {
						require.Equal(t, batch.n, got.bits.Len(), "predicate %d selection %s", i, tc.name)
					}

					var (
						selected = selectionBits(tc.sel, batch.n)
						passed   = selectionBits(got, batch.n)
					)
					for j := range batch.n {
						want := selected[j] && compiled.evalRow(evalTestRow(data, int(batch.row)+j))
						require.Equal(t, want, passed[j], "predicate %d (%#v), record batch %v, prefetch %v, selection %s, row %d: evalRow vs evalRecordBatch",
							i, p, batch, prefetch, tc.name, j)
					}
				}
			}
			require.NoError(t, r.Close())
		}
	}
}

// evalTestData is a dataset of evalTestRows rows with four columns, and the value of
// each cell.
type evalTestData struct {
	columns []Column  // An int64 column with nulls, a binary column with nulls, an int64 column and a binary column.
	values  [][]Value // values[column][row].
	dataset Dataset
}

func buildEvalTestData(t *testing.T) evalTestData {
	t.Helper()

	newBuilder := func(name string, physical datasetmd.PhysicalType, encoding datasetmd.EncodingType) *ColumnBuilder {
		builder, err := NewColumnBuilder(name, BuilderOptions{
			PageSizeHint: 24,
			Type:         ColumnType{Physical: physical, Logical: "test"},
			Compression:  datasetmd.COMPRESSION_TYPE_NONE,
			Encoding:     encoding,
			Statistics:   StatisticsOptions{StoreRangeStats: true},
		})
		require.NoError(t, err)
		return builder
	}

	var (
		sparseInts   = newBuilder("sparse_ints", datasetmd.PHYSICAL_TYPE_INT64, datasetmd.ENCODING_TYPE_DELTA)
		sparseBytes  = newBuilder("sparse_bytes", datasetmd.PHYSICAL_TYPE_BINARY, datasetmd.ENCODING_TYPE_PLAIN)
		denseInts    = newBuilder("dense_ints", datasetmd.PHYSICAL_TYPE_INT64, datasetmd.ENCODING_TYPE_DELTA)
		denseBytes   = newBuilder("dense_bytes", datasetmd.PHYSICAL_TYPE_BINARY, datasetmd.ENCODING_TYPE_PLAIN)
		binaryValues = [][]byte{[]byte("a"), []byte("ab"), []byte("b"), {0xff}, {0x80, 0x01}, {}}
		values       = make([][]Value, 4)
	)
	for i := range values {
		values[i] = make([]Value, evalTestRows)
	}

	for i := range evalTestRows {
		if i%5 != 0 && i < evalTestRows-4 {
			values[0][i] = Int64Value(int64(i%7) - 2)
			require.NoError(t, sparseInts.Append(i, values[0][i]))
		}
		if i%4 != 0 && i < evalTestRows-5 {
			values[1][i] = BinaryValue(binaryValues[(i*7)%len(binaryValues)])
			require.NoError(t, sparseBytes.Append(i, values[1][i]))
		}
		values[2][i] = Int64Value(int64(i / 5))
		require.NoError(t, denseInts.Append(i, values[2][i]))
		values[3][i] = BinaryValue(fmt.Appendf(nil, "line %d x%d", i, i%3))
		require.NoError(t, denseBytes.Append(i, values[3][i]))
	}
	sparseInts.Backfill(evalTestRows)
	sparseBytes.Backfill(evalTestRows)

	dataset, columns := memDatasetOf(t, sparseInts, sparseBytes, denseInts, denseBytes)
	return evalTestData{columns: columns, values: values, dataset: dataset}
}

// evalTestPredicates returns the predicates that read one column of data, for
// each column, and the constant predicates. Some of them combine comparisons
// with Or.
func evalTestPredicates(data evalTestData) []Predicate {
	var predicates []Predicate
	predicates = append(predicates, evalTestIntPredicates(data.columns[0])...)
	predicates = append(predicates, evalTestIntPredicates(data.columns[2])...)
	predicates = append(predicates, evalTestBinaryPredicates(data.columns[1])...)
	predicates = append(predicates, evalTestBinaryPredicates(data.columns[3])...)
	return append(predicates, TruePredicate{}, FalsePredicate{}, nil)
}

func evalTestIntPredicates(col Column) []Predicate {
	i := Int64Value
	return []Predicate{
		EqualPredicate{Column: col, Value: i(0)},
		EqualPredicate{Column: col, Value: i(3)},
		EqualPredicate{Column: col, Value: Value{}},
		GreaterThanPredicate{Column: col, Value: i(0)},
		GreaterThanPredicate{Column: col, Value: i(-5)},
		GreaterThanPredicate{Column: col, Value: Value{}},
		LessThanPredicate{Column: col, Value: i(1)},
		LessThanPredicate{Column: col, Value: i(-5)},
		LessThanPredicate{Column: col, Value: Value{}},
		OrPredicate{Left: GreaterThanPredicate{Column: col, Value: i(1)}, Right: EqualPredicate{Column: col, Value: i(1)}},
		OrPredicate{Left: EqualPredicate{Column: col, Value: i(1)}, Right: GreaterThanPredicate{Column: col, Value: i(1)}},
		OrPredicate{Left: LessThanPredicate{Column: col, Value: i(1)}, Right: EqualPredicate{Column: col, Value: i(1)}},
		OrPredicate{Left: EqualPredicate{Column: col, Value: i(1)}, Right: LessThanPredicate{Column: col, Value: i(1)}},
		OrPredicate{Left: GreaterThanPredicate{Column: col, Value: i(1)}, Right: EqualPredicate{Column: col, Value: i(2)}},
		OrPredicate{Left: GreaterThanPredicate{Column: col, Value: i(1)}, Right: LessThanPredicate{Column: col, Value: i(1)}},
		GreaterThanOrEqualPredicate{Column: col, Value: i(1)},
		GreaterThanOrEqualPredicate{Column: col, Value: i(-5)},
		GreaterThanOrEqualPredicate{Column: col, Value: Value{}},
		LessThanOrEqualPredicate{Column: col, Value: i(1)},
		LessThanOrEqualPredicate{Column: col, Value: i(-5)},
		LessThanOrEqualPredicate{Column: col, Value: Value{}},
		InPredicate{Column: col, Values: NewInt64ValueSetOf(0, 3)},
		InPredicate{Column: col, Values: NewInt64ValueSetOf(7)},
		InPredicate{Column: col, Values: NewMemoizedInt64ValueSetOf(0, 3)},
		InPredicate{Column: col, Values: NewInt64ValueSetOf()},
		FuncPredicate{Column: col, Keep: func(_ Column, v Value) bool { return v.IsNil() || v.Int64()%2 == 0 }},
		FuncPredicate{Column: col, Keep: func(_ Column, v Value) bool { return !v.IsNil() && v.Int64() > 0 }},
	}
}

func evalTestBinaryPredicates(col Column) []Predicate {
	bv := func(s string) Value { return BinaryValue([]byte(s)) }
	keepWhere := func(keep func(value []byte) bool) func(Column, *columnar.UTF8, []int32) []int32 {
		return func(_ Column, values *columnar.UTF8, dst []int32) []int32 {
			for i := range values.Len() {
				if keep(values.Get(i)) {
					dst = append(dst, int32(i))
				}
			}
			return dst
		}
	}
	isEmpty := func(v []byte) bool { return len(v) == 0 }
	isOneByte := func(v []byte) bool { return len(v) == 1 }

	return []Predicate{
		EqualPredicate{Column: col, Value: bv("a")},
		EqualPredicate{Column: col, Value: bv("")},
		EqualPredicate{Column: col, Value: Value{}},
		EqualPredicate{Column: col, Value: BinaryValue([]byte{0xff})},
		GreaterThanPredicate{Column: col, Value: bv("a")},
		GreaterThanPredicate{Column: col, Value: bv("")},
		GreaterThanPredicate{Column: col, Value: BinaryValue([]byte{0x80})},
		GreaterThanPredicate{Column: col, Value: Value{}},
		LessThanPredicate{Column: col, Value: bv("b")},
		LessThanPredicate{Column: col, Value: bv("")},
		LessThanPredicate{Column: col, Value: BinaryValue([]byte{0xfe})},
		LessThanPredicate{Column: col, Value: Value{}},
		OrPredicate{Left: GreaterThanPredicate{Column: col, Value: bv("a")}, Right: EqualPredicate{Column: col, Value: bv("a")}},
		OrPredicate{Left: LessThanPredicate{Column: col, Value: bv("a")}, Right: EqualPredicate{Column: col, Value: bv("a")}},
		OrPredicate{Left: EqualPredicate{Column: col, Value: bv("")}, Right: GreaterThanPredicate{Column: col, Value: bv("")}},
		OrPredicate{Left: EqualPredicate{Column: col, Value: bv("")}, Right: LessThanPredicate{Column: col, Value: bv("")}},
		GreaterThanOrEqualPredicate{Column: col, Value: bv("a")},
		GreaterThanOrEqualPredicate{Column: col, Value: bv("")},
		GreaterThanOrEqualPredicate{Column: col, Value: Value{}},
		GreaterThanOrEqualPredicate{Column: col, Value: BinaryValue([]byte{0xff})},
		LessThanOrEqualPredicate{Column: col, Value: bv("b")},
		LessThanOrEqualPredicate{Column: col, Value: bv("")},
		LessThanOrEqualPredicate{Column: col, Value: Value{}},
		LessThanOrEqualPredicate{Column: col, Value: BinaryValue([]byte{0x80})},
		InPredicate{Column: col, Values: NewBinaryValueSet([]Value{bv("a"), bv("b")})},
		InPredicate{Column: col, Values: NewBinaryValueSet([]Value{bv("")})},
		FuncPredicate{Column: col, Keep: func(_ Column, v Value) bool { return v.IsNil() || len(v.Binary()) == 0 }},
		FuncPredicate{Column: col, Keep: func(_ Column, v Value) bool { return v.IsNil() || len(v.Binary()) == 0 }, KeepBatch: keepWhere(isEmpty)},
		FuncPredicate{Column: col, Keep: func(_ Column, v Value) bool { return !v.IsNil() && len(v.Binary()) == 1 }, KeepBatch: keepWhere(isOneByte)},
	}
}

// randomPredicateTree returns a random tree of And, Or and Not predicates, up to
// the given depth, with the given leaves.
func randomPredicateTree(rng *rand.Rand, leaves []Predicate, depth int) Predicate {
	if depth == 0 || rng.Intn(3) == 0 {
		return leaves[rng.Intn(len(leaves))]
	}

	switch rng.Intn(3) {
	case 0:
		return AndPredicate{Left: randomPredicateTree(rng, leaves, depth-1), Right: randomPredicateTree(rng, leaves, depth-1)}
	case 1:
		return OrPredicate{Left: randomPredicateTree(rng, leaves, depth-1), Right: randomPredicateTree(rng, leaves, depth-1)}
	default:
		return NotPredicate{Inner: randomPredicateTree(rng, leaves, depth-1)}
	}
}

type evalTestSelection struct {
	name string
	sel  selection
}

// evalTestSelections returns selections of n rows: every row as a selection with
// no bitmap, and bitmaps that select few, many and scattered rows.
func evalTestSelections(alloc *memory.Allocator, n int, rng *rand.Rand) []evalTestSelection {
	bitmap := func(selected func(i int) bool) selection {
		sel := newSelection(alloc, n, false)
		for i := range n {
			if selected(i) {
				sel.set(i)
			}
		}
		return sel
	}

	return []evalTestSelection{
		{"every row", newSelectionAllRows(n)},
		{"alternate rows", bitmap(func(i int) bool { return i%2 == 0 })},
		{"a third of the rows", bitmap(func(i int) bool { return i%3 == 1 })},
		{"the first three rows", bitmap(func(i int) bool { return i < 3 })},
		{"the last row", bitmap(func(i int) bool { return i == n-1 })},
		{"the middle row", bitmap(func(i int) bool { return i == n/2 })},
		{"every row as a bitmap", bitmap(func(int) bool { return true })},
		{"random rows", bitmap(func(int) bool { return rng.Intn(4) == 0 })},
		{"most rows", bitmap(func(int) bool { return rng.Intn(4) != 0 })},
		{"the last six rows", bitmap(func(i int) bool { return i >= n-6 })},
	}
}

// openEvalTestRecordBatch opens a RowReader on data and starts a record batch of n rows
// from the given row.
func openEvalTestRecordBatch(t *testing.T, data evalTestData, prefetch bool, row int64, n int) *RowReader {
	t.Helper()

	r := NewRowReader(RowReaderOptions{Dataset: data.dataset, ProjectedColumns: data.columns, PrefetchAllOnOpen: prefetch})
	require.NoError(t, r.Open(t.Context()))
	resetEvalTestRecordBatch(r, row, n)
	return r
}

// resetEvalTestRecordBatch starts a new record batch of n rows from the given row, as
// ReadRecordBatch does.
func resetEvalTestRecordBatch(r *RowReader, row int64, n int) {
	r.dl.SetReadRange(rangeset.Range{Start: uint64(row), End: uint64(row) + uint64(n)})
	r.recordBatchScratch.Reclaim()
	r.currRecordBatch.reset(r, &r.recordBatchScratch, len(r.columns), row, n)
}

func evalTestRow(data evalTestData, row int) Row {
	values := make([]Value, len(data.columns))
	for c := range values {
		values[c] = data.values[c][row]
	}
	return Row{Values: values}
}

// recoverPanic runs f and returns the value that it panics with, or nil if f does
// not panic.
func recoverPanic(f func()) (recovered any) {
	defer func() { recovered = recover() }()
	f()
	return nil
}

// selectionBits returns one bool for each of the n rows of the record batch.
func selectionBits(sel selection, n int) []bool {
	bits := make([]bool, n)
	sel.forEach(func(i int) { bits[i] = true })
	return bits
}
