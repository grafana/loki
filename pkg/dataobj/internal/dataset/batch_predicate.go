package dataset

import (
	"context"
	"fmt"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/compute"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/memory"
)

// vectorPredicate is a [Predicate] compiled to run over the arrays of a read
// window, one column at a time.
//
// eval receives the rows that earlier predicates selected and returns the
// subset of them that pass. It reads a column of the window only when it runs,
// so a window that earlier predicates emptied never reads the later columns.
// A predicate inside an And only sees the rows that the left side selected,
// and one inside an Or only sees the rows that the left side rejected.
type vectorPredicate interface {
	eval(ctx context.Context, w *readWindow, sel memory.Bitmap) (memory.Bitmap, error)
}

// compileVectorPredicate compiles p. The columns of p must be keys of lookup,
// which maps a column to its index in a window.
func compileVectorPredicate(p Predicate, lookup map[Column]int) (vectorPredicate, error) {
	switch p := p.(type) {
	case nil, TruePredicate:
		return vecConst(true), nil

	case FalsePredicate:
		return vecConst(false), nil

	case AndPredicate:
		left, err := compileVectorPredicate(p.Left, lookup)
		if err != nil {
			return nil, err
		}
		right, err := compileVectorPredicate(p.Right, lookup)
		if err != nil {
			return nil, err
		}
		return vecAnd{left: left, right: right}, nil

	case OrPredicate:
		if fused, ok, err := fuseOrCompare(p, lookup); err != nil {
			return nil, err
		} else if ok {
			return fused, nil
		}

		left, err := compileVectorPredicate(p.Left, lookup)
		if err != nil {
			return nil, err
		}
		right, err := compileVectorPredicate(p.Right, lookup)
		if err != nil {
			return nil, err
		}
		return vecOr{left: left, right: right}, nil

	case NotPredicate:
		inner, err := compileVectorPredicate(p.Inner, lookup)
		if err != nil {
			return nil, err
		}
		return vecNot{inner: inner}, nil

	case EqualPredicate:
		return compileCompare(p, p.Column, p.Value, compareEqual, lookup)

	case GreaterThanPredicate:
		return compileCompare(p, p.Column, p.Value, compareGreater, lookup)

	case LessThanPredicate:
		return compileCompare(p, p.Column, p.Value, compareLess, lookup)

	case InPredicate:
		idx, err := lookupColumnIndex(lookup, p.Column)
		if err != nil {
			return nil, err
		}
		return compileIn(p, idx), nil

	case FuncPredicate:
		idx, err := lookupColumnIndex(lookup, p.Column)
		if err != nil {
			return nil, err
		}
		if p.Keep == nil && p.KeepBatch == nil {
			return nil, fmt.Errorf("func predicate on column %v has neither Keep nor KeepBatch", p.Column)
		}
		return &vecFunc{column: idx, source: p.Column, keep: p.Keep, keepBatch: p.KeepBatch}, nil

	default:
		return nil, fmt.Errorf("unsupported predicate type %T", p)
	}
}

// vecConst is a predicate with a fixed result.
type vecConst bool

func (p vecConst) eval(_ context.Context, w *readWindow, sel memory.Bitmap) (memory.Bitmap, error) {
	if p {
		return sel, nil
	}
	return newSelection(w.alloc(), w.n, false), nil
}

// vecAnd passes the rows that both sides pass. The right side only sees the
// rows that the left side passed.
type vecAnd struct{ left, right vectorPredicate }

func (p vecAnd) eval(ctx context.Context, w *readWindow, sel memory.Bitmap) (memory.Bitmap, error) {
	left, err := p.left.eval(ctx, w, sel)
	if err != nil {
		return sel, err
	} else if selectionCount(left, w.n) == 0 {
		return left, nil
	}
	return p.right.eval(ctx, w, left)
}

// vecOr passes the rows that either side passes. The right side only sees the
// rows that the left side rejected.
type vecOr struct{ left, right vectorPredicate }

func (p vecOr) eval(ctx context.Context, w *readWindow, sel memory.Bitmap) (memory.Bitmap, error) {
	left, err := p.left.eval(ctx, w, sel)
	if err != nil {
		return sel, err
	}

	rest := andNotSelection(w.alloc(), w.n, sel, left)
	if selectionCount(rest, w.n) == 0 {
		return left, nil
	}

	right, err := p.right.eval(ctx, w, rest)
	if err != nil {
		return sel, err
	}
	return orSelection(w.alloc(), w.n, sel, left, right), nil
}

// vecNot passes the rows that the inner predicate rejects.
type vecNot struct{ inner vectorPredicate }

func (p vecNot) eval(ctx context.Context, w *readWindow, sel memory.Bitmap) (memory.Bitmap, error) {
	inner, err := p.inner.eval(ctx, w, sel)
	if err != nil {
		return sel, err
	}
	return andNotSelection(w.alloc(), w.n, sel, inner), nil
}

type compareOp int

const (
	compareEqual compareOp = iota
	compareGreater
	compareLess
	compareGreaterOrEqual
	compareLessOrEqual
)

// vecCompare compares a column with a constant using a compute kernel.
type vecCompare struct {
	column int
	op     compareOp
	scalar columnar.Datum

	// nullPasses is the result for a row where the column is null. It follows
	// [CompareValues], which orders a null value below every other value and
	// treats a null value and an empty binary value as equal.
	nullPasses bool
}

// compileCompare compiles a comparison leaf. A leaf that the kernels cannot
// run, for example one against a null constant, falls back to evaluating the
// row by row form of the predicate.
func compileCompare(leaf Predicate, column Column, value Value, op compareOp, lookup map[Column]int) (vectorPredicate, error) {
	idx, err := lookupColumnIndex(lookup, column)
	if err != nil {
		return nil, err
	}

	scalar := scalarOf(value)
	if scalar == nil || value.Type() != column.ColumnDesc().Type.Physical {
		return newVecRowwise(leaf, column, idx)
	}

	return &vecCompare{
		column:     idx,
		op:         op,
		scalar:     scalar,
		nullPasses: compareNullPasses(op, value),
	}, nil
}

// compareNullPasses returns the result of comparing a null value with value.
func compareNullPasses(op compareOp, value Value) bool {
	emptyBinary := value.Type() == datasetmd.PHYSICAL_TYPE_BINARY && value.IsZero()

	switch op {
	case compareEqual:
		return emptyBinary
	case compareGreater:
		return false
	case compareLess:
		return !emptyBinary
	case compareGreaterOrEqual:
		return compareNullPasses(compareGreater, value) || compareNullPasses(compareEqual, value)
	case compareLessOrEqual:
		return compareNullPasses(compareLess, value) || compareNullPasses(compareEqual, value)
	default:
		panic(fmt.Sprintf("unknown compare op %d", op))
	}
}

// scalarOf returns value as a scalar for the compute kernels, or nil if value
// is null.
func scalarOf(value Value) columnar.Datum {
	switch value.Type() {
	case datasetmd.PHYSICAL_TYPE_INT64:
		return &columnar.NumberScalar[int64]{Value: value.Int64()}
	case datasetmd.PHYSICAL_TYPE_UINT64:
		return &columnar.NumberScalar[uint64]{Value: value.Uint64()}
	case datasetmd.PHYSICAL_TYPE_BINARY:
		return &columnar.UTF8Scalar{Value: value.Binary()}
	default:
		return nil
	}
}

// fuseOrCompare turns "x > v OR x == v" into "x >= v", and the same for less
// than. A fused comparison runs one kernel instead of two and skips the Or.
func fuseOrCompare(p OrPredicate, lookup map[Column]int) (vectorPredicate, bool, error) {
	type leaf struct {
		column Column
		value  Value
		op     compareOp
	}

	asLeaf := func(p Predicate) (leaf, bool) {
		switch p := p.(type) {
		case EqualPredicate:
			return leaf{p.Column, p.Value, compareEqual}, true
		case GreaterThanPredicate:
			return leaf{p.Column, p.Value, compareGreater}, true
		case LessThanPredicate:
			return leaf{p.Column, p.Value, compareLess}, true
		}
		return leaf{}, false
	}

	left, ok := asLeaf(p.Left)
	if !ok {
		return nil, false, nil
	}
	right, ok := asLeaf(p.Right)
	if !ok {
		return nil, false, nil
	}

	if left.op == compareEqual {
		left, right = right, left
	}
	if right.op != compareEqual || left.op == compareEqual {
		return nil, false, nil
	}
	if left.column != right.column || left.value.Type() != right.value.Type() || left.value.Type() == datasetmd.PHYSICAL_TYPE_UNSPECIFIED {
		return nil, false, nil
	} else if CompareValues(&left.value, &right.value) != 0 {
		return nil, false, nil
	} else if left.value.Type() != left.column.ColumnDesc().Type.Physical {
		return nil, false, nil
	}

	idx, err := lookupColumnIndex(lookup, left.column)
	if err != nil {
		return nil, false, err
	}

	op := compareGreaterOrEqual
	if left.op == compareLess {
		op = compareLessOrEqual
	}
	return &vecCompare{
		column:     idx,
		op:         op,
		scalar:     scalarOf(left.value),
		nullPasses: compareNullPasses(op, left.value),
	}, true, nil
}

func (p *vecCompare) eval(ctx context.Context, w *readWindow, sel memory.Bitmap) (memory.Bitmap, error) {
	arr, err := w.column(ctx, p.column, sel)
	if err != nil {
		return sel, err
	}

	var kernel func(*memory.Allocator, columnar.Datum, columnar.Datum, memory.Bitmap) (columnar.Datum, error)
	switch p.op {
	case compareEqual:
		kernel = compute.Equals
	case compareGreater:
		kernel = compute.GreaterThan
	case compareLess:
		kernel = compute.LessThan
	case compareGreaterOrEqual:
		kernel = compute.GreaterOrEqual
	case compareLessOrEqual:
		kernel = compute.LessOrEqual
	}

	res, err := kernel(w.alloc(), arr, p.scalar, sel)
	if err != nil {
		return sel, fmt.Errorf("comparing column %d: %w", p.column, err)
	}
	return leafSelection(w.alloc(), w.n, res, p.nullPasses, sel), nil
}

// vecRowwise evaluates a leaf predicate on each selected row with the same
// code as the row reader. It handles the leaves that have no kernel.
type vecRowwise struct {
	column   int
	compiled compiledPredicate
}

func newVecRowwise(leaf Predicate, column Column, idx int) (*vecRowwise, error) {
	compiled, err := compilePredicate(leaf, map[Column]int{column: 0})
	if err != nil {
		return nil, err
	}
	return &vecRowwise{column: idx, compiled: compiled}, nil
}

func (p *vecRowwise) eval(ctx context.Context, w *readWindow, sel memory.Bitmap) (memory.Bitmap, error) {
	arr, err := w.column(ctx, p.column, sel)
	if err != nil {
		return sel, err
	}

	var (
		out = newSelection(w.alloc(), w.n, false)
		row = Row{Values: make([]Value, 1)}
	)
	forEachSelected(sel, w.n, func(i int) {
		row.Values[0] = valueAt(arr, i)
		if p.compiled.eval(row) {
			out.Set(i, true)
		}
	})
	return out, nil
}

// valueAt returns the value at index i of arr. The returned value shares memory
// with arr.
func valueAt(arr columnar.Array, i int) Value {
	switch arr := arr.(type) {
	case *columnar.Number[int64]:
		if arr.IsNull(i) {
			return Value{}
		}
		return Int64Value(arr.Get(i))
	case *columnar.Number[uint64]:
		if arr.IsNull(i) {
			return Value{}
		}
		return Uint64Value(arr.Get(i))
	case *columnar.UTF8:
		if arr.IsNull(i) {
			return Value{}
		}
		return BinaryValue(arr.Get(i))
	default:
		return Value{}
	}
}

// compileIn compiles an InPredicate. A set of int64 members, the stream ID
// set of a logs section, runs on a typed fast path.
func compileIn(p InPredicate, idx int) vectorPredicate {
	var members map[int64]Value
	switch set := p.Values.(type) {
	case Int64Set:
		members = set.values
	case *MemoizedInt64Set:
		members = set.set.values
	}

	if members != nil && p.Column.ColumnDesc().Type.Physical == datasetmd.PHYSICAL_TYPE_INT64 {
		return &vecInInt64{column: idx, members: members}
	}
	return &vecIn{column: idx, physical: p.Column.ColumnDesc().Type.Physical, values: p.Values}
}

// vecInInt64 passes the rows whose int64 value is a member of a set.
type vecInInt64 struct {
	column  int
	members map[int64]Value
}

func (p *vecInInt64) eval(ctx context.Context, w *readWindow, sel memory.Bitmap) (memory.Bitmap, error) {
	arr, err := w.column(ctx, p.column, sel)
	if err != nil {
		return sel, err
	}
	numbers, ok := arr.(*columnar.Number[int64])
	if !ok {
		return sel, fmt.Errorf("column %d returned %T, want int64 numbers", p.column, arr)
	}

	var (
		values = numbers.Values()
		out    = newSelection(w.alloc(), w.n, false)

		// A logs section keeps the rows of one stream together, so consecutive
		// values repeat. Remembering the last lookup skips most map accesses.
		lastKey    int64
		lastResult bool
		haveLast   bool
	)
	forEachSelected(sel, w.n, func(i int) {
		if numbers.IsNull(i) {
			return
		}
		key := values[i]
		if !haveLast || key != lastKey {
			_, lastResult = p.members[key]
			lastKey, haveLast = key, true
		}
		if lastResult {
			out.Set(i, true)
		}
	})
	return out, nil
}

// vecIn passes the rows whose value is a member of a [ValueSet].
type vecIn struct {
	column   int
	physical datasetmd.PhysicalType
	values   ValueSet
}

func (p *vecIn) eval(ctx context.Context, w *readWindow, sel memory.Bitmap) (memory.Bitmap, error) {
	arr, err := w.column(ctx, p.column, sel)
	if err != nil {
		return sel, err
	}

	out := newSelection(w.alloc(), w.n, false)
	forEachSelected(sel, w.n, func(i int) {
		value := valueAt(arr, i)
		if value.IsNil() || value.Type() != p.physical {
			return
		}
		if p.values.Contains(value) {
			out.Set(i, true)
		}
	})
	return out, nil
}

// vecFunc runs the Keep function of a [FuncPredicate].
type vecFunc struct {
	column    int
	source    Column
	keep      func(column Column, value Value) bool
	keepBatch func(column Column, values *columnar.UTF8, dstIndexes []int32) []int32
}

func (p *vecFunc) eval(ctx context.Context, w *readWindow, sel memory.Bitmap) (memory.Bitmap, error) {
	arr, err := w.column(ctx, p.column, sel)
	if err != nil {
		return sel, err
	}

	if utf8Arr, ok := arr.(*columnar.UTF8); ok && p.keepBatch != nil {
		return p.evalBatch(w, utf8Arr, sel)
	}
	if p.keep == nil {
		return sel, fmt.Errorf("func predicate on column %d has no Keep function for a %T array", p.column, arr)
	}

	out := newSelection(w.alloc(), w.n, false)
	forEachSelected(sel, w.n, func(i int) {
		if p.keep(p.source, valueAt(arr, i)) {
			out.Set(i, true)
		}
	})
	return out, nil
}

func (p *vecFunc) evalBatch(w *readWindow, arr *columnar.UTF8, sel memory.Bitmap) (memory.Bitmap, error) {
	var (
		src       = arr
		positions []int32 // Maps an index of src to a row of the window, when src is dense.
	)

	// Copying the selected lines into a dense array costs less than testing
	// the lines that earlier predicates rejected, once fewer than half remain.
	if selected := selectionCount(sel, w.n); sel.Len() > 0 && selected*2 < w.n {
		dense, err := compute.Filter(w.alloc(), arr, sel)
		if err != nil {
			return sel, fmt.Errorf("compacting column %d: %w", p.column, err)
		}
		src = dense.(*columnar.UTF8)

		positions = w.r.positions[:0]
		for i := range sel.IterValues(true) {
			positions = append(positions, int32(i))
		}
		w.r.positions = positions
	}

	keep := p.keepBatch(p.source, src, w.r.keepScratch[:0])
	w.r.keepScratch = keep[:0]

	out := newSelection(w.alloc(), w.n, false)
	prev := int32(-1)
	for _, k := range keep {
		if k <= prev || int(k) >= src.Len() {
			return sel, fmt.Errorf("batch predicate kept index %d of %d values after index %d, want ascending indexes in range", k, src.Len(), prev)
		}
		prev = k

		row := int(k)
		if positions != nil {
			row = int(positions[k])
		}
		out.Set(row, true)
	}

	if positions == nil {
		// KeepBatch tested every row of the window, including the rejected ones.
		return andSelection(w.alloc(), w.n, out, sel), nil
	}
	return out, nil
}
