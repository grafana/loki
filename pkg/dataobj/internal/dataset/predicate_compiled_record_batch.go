package dataset

import (
	"context"
	"fmt"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/compute"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/memory"
)

func (p compiledConstPredicate) evalRecordBatch(_ context.Context, rb *recordBatch, sel selection) (selection, error) {
	if p {
		return sel, nil
	}
	return newSelection(rb.scratch, rb.n, false), nil
}

// evalRecordBatch passes the rows that both sides pass. The right side only sees the
// rows that the left side passed.
func (p *compiledAndPredicate) evalRecordBatch(ctx context.Context, rb *recordBatch, sel selection) (selection, error) {
	left, err := p.left.evalRecordBatch(ctx, rb, sel)
	if err != nil {
		return sel, err
	} else if left.empty() {
		return left, nil
	}
	return p.right.evalRecordBatch(ctx, rb, left)
}

// evalRecordBatch passes the rows that either side passes. The right side only sees
// the rows that the left side rejected.
func (p *compiledOrPredicate) evalRecordBatch(ctx context.Context, rb *recordBatch, sel selection) (selection, error) {
	left, err := p.left.evalRecordBatch(ctx, rb, sel)
	if err != nil {
		return sel, err
	}

	rest := sel.subtract(rb.scratch, left)
	if rest.empty() {
		return left, nil
	}

	right, err := p.right.evalRecordBatch(ctx, rb, rest)
	if err != nil {
		return sel, err
	}
	return left.or(rb.scratch, right), nil
}

// evalRecordBatch passes the rows that the inner predicate rejects.
func (p *compiledNotPredicate) evalRecordBatch(ctx context.Context, rb *recordBatch, sel selection) (selection, error) {
	inner, err := p.inner.evalRecordBatch(ctx, rb, sel)
	if err != nil {
		return sel, err
	}
	return sel.subtract(rb.scratch, inner), nil
}

func (p *compiledEqualPredicate) evalRecordBatch(ctx context.Context, rb *recordBatch, sel selection) (selection, error) {
	return evalCompareRecordBatch(ctx, rb, p.columnIndex, p.kernel, p, sel)
}

func (p *compiledGreaterThanPredicate) evalRecordBatch(ctx context.Context, rb *recordBatch, sel selection) (selection, error) {
	return evalCompareRecordBatch(ctx, rb, p.columnIndex, p.kernel, p, sel)
}

func (p *compiledLessThanPredicate) evalRecordBatch(ctx context.Context, rb *recordBatch, sel selection) (selection, error) {
	return evalCompareRecordBatch(ctx, rb, p.columnIndex, p.kernel, p, sel)
}

func (p *compiledGreaterThanOrEqualPredicate) evalRecordBatch(ctx context.Context, rb *recordBatch, sel selection) (selection, error) {
	return evalCompareRecordBatch(ctx, rb, p.columnIndex, p.kernel, p, sel)
}

func (p *compiledLessThanOrEqualPredicate) evalRecordBatch(ctx context.Context, rb *recordBatch, sel selection) (selection, error) {
	return evalCompareRecordBatch(ctx, rb, p.columnIndex, p.kernel, p, sel)
}

func (p *compiledInPredicate) evalRecordBatch(ctx context.Context, rb *recordBatch, sel selection) (selection, error) {
	arr, err := rb.columnArrayOrView(ctx, p.columnIndex, sel)
	if err != nil {
		return sel, err
	}
	if p.int64Values == nil {
		return evalRowwiseRecordBatch(rb, p.columnIndex, arr, p, sel), nil
	}

	numbers, ok := arr.(*columnar.Number[int64])
	if !ok {
		return sel, fmt.Errorf("column %d returned %T, want int64 numbers", p.columnIndex, arr)
	}

	var (
		values = numbers.Values()
		out    = newSelection(rb.scratch, rb.n, false)
	)
	sel.forEach(func(i int) {
		if !numbers.IsNull(i) && p.int64Values.ContainsInt64(values[i]) {
			out.set(i)
		}
	})
	return out, nil
}

// evalRecordBatch calls KeepBatch when the predicate sets it and the column is
// binary, wherever the predicate sits in the predicate tree. Otherwise it calls
// Keep on each selected row.
//
// The array of the column can be a view over several pages. Then evalRecordBatch
// calls KeepBatch once for each chunk that holds a selected row.
//
// If fewer than half of the rows are selected, a call gets only the selected
// values of its chunk. Otherwise it gets the values of every row of its chunk.
// In that case evalRecordBatch drops the indexes of the unselected rows. The
// values of those rows are unspecified.
//
// evalRecordBatch returns an error if an index is out of range, repeats, or does
// not ascend.
func (p *compiledFuncPredicate) evalRecordBatch(ctx context.Context, rb *recordBatch, sel selection) (selection, error) {
	arr, err := rb.columnArrayOrView(ctx, p.columnIndex, sel)
	if err != nil {
		return sel, err
	}

	if values, ok := arr.(columnar.UTF8Reader); ok && p.keepBatch != nil {
		return p.evalKeepBatch(rb, values, sel)
	}
	return evalRowwiseRecordBatch(rb, p.columnIndex, arr, p, sel), nil
}

// evalKeepBatch calls keepBatchChunk for each chunk of the column. KeepBatch runs
// only for a chunk that holds a selected row. A [columnar.UTF8] is one chunk.
// evalKeepBatch returns an error if values does not hold exactly the rows of the
// record batch.
func (p *compiledFuncPredicate) evalKeepBatch(rb *recordBatch, values columnar.UTF8Reader, sel selection) (selection, error) {
	// Compacting copies the selected values into a dense array, so KeepBatch skips
	// the rows that earlier predicates dropped. The copy grows with the number of
	// selected rows. The KeepBatch work that it saves shrinks.
	//
	// The threshold of one half assumes that KeepBatch costs more per value than
	// the copy. Below half, compacting saves more than it costs. At half or more,
	// the function passes each whole chunk instead, and KeepBatch tests at most
	// twice the values it needs. The threshold is a heuristic.
	compact := !sel.isAllRows() && sel.count()*2 < rb.n

	if values.Len() != rb.n {
		return sel, fmt.Errorf("column %d has %d rows, want %d", p.columnIndex, values.Len(), rb.n)
	}

	var (
		out   = newSelection(rb.scratch, rb.n, false)
		start int
	)
	keepChunk := func(chunk *columnar.UTF8) error {
		end := start + chunk.Len()
		err := p.keepBatchChunk(rb, chunk, start, end, compact, sel, &out)
		start = end
		return err
	}

	switch values := values.(type) {
	case *columnar.UTF8:
		if err := keepChunk(values); err != nil {
			return sel, err
		}
	case *columnar.ChunkedUTF8:
		for k := range values.NumChunks() {
			if err := keepChunk(values.Chunk(k)); err != nil {
				return sel, err
			}
		}
	default:
		return sel, fmt.Errorf("column %d returned %T, want a binary array", p.columnIndex, values)
	}

	if !compact {
		// KeepBatch saw each whole chunk that holds a selected row. Drop the rows
		// that did not reach this predicate.
		return out.and(rb.scratch, sel), nil
	}
	return out, nil
}

// keepBatchChunk calls KeepBatch on the chunk that holds the rows [start, end)
// of the record batch, and sets in out the rows that it keeps. It skips a chunk
// that holds no selected row. In compact mode, it passes only the selected
// values of a chunk that also holds unselected rows.
func (p *compiledFuncPredicate) keepBatchChunk(rb *recordBatch, chunk *columnar.UTF8, start, end int, compact bool, sel selection, out *selection) error {
	var (
		src       = chunk
		compacted bool
		positions []int32 // Maps an index of src to a row of the record batch, when src is compacted.
	)
	if !sel.isAllRows() {
		chunkSel := sel.slice(start, end)
		if chunkSel.empty() {
			return nil
		}

		if compact && chunkSel.count() < chunk.Len() {
			dense, err := chunkSel.filterArray(rb.scratch, chunk)
			if err != nil {
				return fmt.Errorf("compacting column %d: %w", p.columnIndex, err)
			}
			var ok bool
			if src, ok = dense.(*columnar.UTF8); !ok {
				return fmt.Errorf("compacting column %d returned %T, want a binary array", p.columnIndex, dense)
			}

			compacted = true
			positions = rb.keepBatchPositions[:0]
			chunkSel.forEach(func(i int) {
				positions = append(positions, int32(start+i))
			})
			rb.keepBatchPositions = positions
		}
	}

	// dstIndexes is a scratch slice that later calls reuse, so KeepBatch must not
	// retain it.
	keep := p.keepBatch(p.column, src, rb.keepBatchScratch[:0])
	rb.keepBatchScratch = keep[:0]

	prev := int32(-1)
	for _, k := range keep {
		if k <= prev || int(k) >= src.Len() {
			return fmt.Errorf("batch predicate on column %d kept index %d of %d values for rows [%d, %d) after index %d, want ascending indexes in range", p.columnIndex, k, src.Len(), start, end, prev)
		}
		prev = k

		row := start + int(k)
		if compacted {
			row = int(positions[k])
		}
		out.set(row)
	}
	return nil
}

type compareOp int

const (
	compareEqual compareOp = iota
	compareGreater
	compareLess
	compareGreaterOrEqual
	compareLessOrEqual
)

// kernelComparison holds what a comparison leaf needs to run a compute kernel
// on the array of a column.
type kernelComparison struct {
	op compareOp

	// scalar is the constant of the comparison. It is nil if the kernels cannot
	// run the comparison.
	scalar columnar.Datum

	// nullPasses is the result for a row where the column is null. It follows
	// [CompareValues]. That function orders a null value below every other value,
	// except an empty binary value, which it treats as equal to null.
	nullPasses bool
}

// newKernelComparison returns the kernel comparison of the column with value.
func newKernelComparison(op compareOp, column Column, value Value) *kernelComparison {
	// The kernels cannot compare a column with a null constant or with a
	// constant of another type. The leaf runs row by row instead.
	scalar := scalarOf(value)
	if scalar == nil || value.Type() != column.ColumnDesc().Type.Physical {
		return &kernelComparison{op: op}
	}
	return &kernelComparison{op: op, scalar: scalar, nullPasses: compareNullPasses(op, value)}
}

// evalCompareRecordBatch evaluates the comparison leaf on the column at columnIndex.
// It runs a kernel if k allows it, and leaf.evalRow on each selected row
// otherwise.
func evalCompareRecordBatch(ctx context.Context, rb *recordBatch, columnIndex int, k *kernelComparison, leaf compiledPredicate, sel selection) (selection, error) {
	if k.scalar == nil {
		arr, err := rb.columnArrayOrView(ctx, columnIndex, sel)
		if err != nil {
			return sel, err
		}
		return evalRowwiseRecordBatch(rb, columnIndex, arr, leaf, sel), nil
	}

	// The kernels read a contiguous array only.
	arr, err := rb.columnArray(ctx, columnIndex, sel)
	if err != nil {
		return sel, err
	}

	var kernel func(*memory.Allocator, columnar.Datum, columnar.Datum, memory.Bitmap) (columnar.Datum, error)
	switch k.op {
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

	res, err := kernel(rb.scratch, arr, k.scalar, sel.bits)
	if err != nil {
		return sel, fmt.Errorf("comparing column %d: %w", columnIndex, err)
	}
	out, err := selectionFromComparison(rb.scratch, res, k.nullPasses, sel)
	if err != nil {
		return sel, fmt.Errorf("comparing column %d: %w", columnIndex, err)
	}
	return out, nil
}

// evalRowwiseRecordBatch returns the rows of sel that the leaf passes. The leaf
// reads only the column at columnIndex, and arr holds that column for the record
// batch. evalRowwiseRecordBatch runs leaf.evalRow on each selected row, so it
// serves the leaves that have no kernel.
func evalRowwiseRecordBatch(rb *recordBatch, columnIndex int, arr columnar.Array, leaf compiledPredicate, sel selection) selection {
	var (
		out = newSelection(rb.scratch, rb.n, false)
		row = Row{Values: make([]Value, columnIndex+1)}
	)
	sel.forEach(func(i int) {
		row.Values[columnIndex] = valueAt(arr, i)
		if leaf.evalRow(row) {
			out.set(i)
		}
	})
	return out
}

// compareNullPasses reports whether a null value passes the comparison op
// against value.
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

// int64ValuesOf returns the set of p as an int64 lookup, if the column of p is
// int64 and the set can test an int64 without a [Value]. It returns nil
// otherwise.
func int64ValuesOf(p InPredicate) int64Lookup {
	if p.Column.ColumnDesc().Type.Physical != datasetmd.PHYSICAL_TYPE_INT64 {
		return nil
	}

	set, _ := p.Values.(int64Lookup)
	return set
}
