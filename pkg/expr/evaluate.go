package expr

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/compute"
	"github.com/grafana/loki/v3/pkg/memory"
)

// Evaluate processes expr against the provided batch, producing a datum as a
// result using alloc.
//
// Selection defines which rows are evaluated. Selected rows have a true value
// in the selection vector. When selection has Len() == 0, all rows are selected.
// When selection has Len() > 0, only rows with a true bit are selected. Rows that
// are not selected are treated as null in array results.
//
// The return type of Evaluate depends on the expression provided. See the
// documentation for implementations of Expression for what they produce when
// evaluated.
func Evaluate(alloc *memory.Allocator, expr Expression, batch *columnar.RecordBatch, selection memory.Bitmap) (columnar.Datum, error) {
	nrows := 0
	if batch != nil {
		nrows = int(batch.NumRows())
	}
	if selection.Len() > 0 && selection.Len() != nrows {
		return nil, fmt.Errorf("selection length mismatch: %d != %d", selection.Len(), nrows)
	}

	switch expr := expr.(type) {
	case *Constant:
		return expr.Value, nil

	case *Column:
		columnIndex := -1
		if schema := batch.Schema(); schema != nil {
			_, columnIndex = schema.ColumnIndex(expr.Name)
		}

		if columnIndex == -1 {
			validity := memory.NewBitmap(alloc, int(batch.NumRows()))
			validity.AppendCount(false, int(batch.NumRows()))
			return columnar.NewNull(validity), nil
		}
		col := batch.Column(int64(columnIndex))
		// Apply selection to column data. Note: compute functions also apply
		// selection, resulting in idempotent double application which is safe.
		return applySelectionToColumn(alloc, col, selection)

	case *Unary:
		return evaluateUnary(alloc, expr, batch, selection)

	case *Binary:
		return evaluateBinary(alloc, expr, batch, selection)

	case *Regexp:
		return nil, fmt.Errorf("regexp can only be evaluated as the right-hand side of regex match operations")

	default:
		panic(fmt.Sprintf("unexpected expression type %T", expr))
	}
}

func evaluateUnary(alloc *memory.Allocator, expr *Unary, batch *columnar.RecordBatch, selection memory.Bitmap) (columnar.Datum, error) {
	switch expr.Op {
	case UnaryOpNOT:
		value, err := Evaluate(alloc, expr.Value, batch, selection)
		if err != nil {
			return nil, err
		}
		return compute.Not(alloc, value, selection)
	default:
		return nil, fmt.Errorf("unexpected unary operator %s", expr.Op)
	}
}

func evaluateBinary(alloc *memory.Allocator, expr *Binary, batch *columnar.RecordBatch, selection memory.Bitmap) (columnar.Datum, error) {
	// Check for special operators that need different handling of their arguments.
	switch expr.Op {
	case BinaryOpMatchRegex:
		return evaluateSpecialBinary(alloc, expr, batch, selection)
	}

	// TODO(rfratto): If expr.Op is [BinaryOpAND] or [BinaryOpOR], we can
	// propagate selection vectors to avoid unnecessary evaluations.
	left, err := Evaluate(alloc, expr.Left, batch, selection)
	if err != nil {
		return nil, err
	}

	right, err := Evaluate(alloc, expr.Right, batch, selection)
	if err != nil {
		return nil, err
	}

	switch expr.Op {
	case BinaryOpEQ:
		return compute.Equals(alloc, left, right, selection)
	case BinaryOpNEQ:
		return compute.NotEquals(alloc, left, right, selection)
	case BinaryOpGT:
		return compute.GreaterThan(alloc, left, right, selection)
	case BinaryOpGTE:
		return compute.GreaterOrEqual(alloc, left, right, selection)
	case BinaryOpLT:
		return compute.LessThan(alloc, left, right, selection)
	case BinaryOpLTE:
		return compute.LessOrEqual(alloc, left, right, selection)
	case BinaryOpAND:
		return compute.And(alloc, left, right, selection)
	case BinaryOpOR:
		return compute.Or(alloc, left, right, selection)
	case BinaryOpHasSubstr:
		return compute.Substr(alloc, left, right, selection)
	case BinaryOpHasSubstrIgnoreCase:
		return compute.SubstrInsensitive(alloc, left, right, selection)
	default:
		return nil, fmt.Errorf("unexpected binary operator %s", expr.Op)
	}
}

// applySelectionToColumn applies a selection bitmap to column data by marking
// unselected rows as null. This delegates to compute package functions to avoid
// code duplication.
func applySelectionToColumn(alloc *memory.Allocator, col columnar.Datum, selection memory.Bitmap) (columnar.Datum, error) {
	if selection.Len() == 0 {
		return col, nil
	}

	// Scalars don't need selection applied
	arr, ok := col.(columnar.Array)
	if !ok {
		return col, nil
	}

	// Apply selection using compute package utilities based on array type
	switch a := arr.(type) {
	case *columnar.Bool:
		return compute.ApplySelectionToBoolArray(alloc, a, selection)
	case *columnar.Number[int64]:
		return compute.ApplySelectionToNumberArray(alloc, a, selection)
	case *columnar.Number[uint64]:
		return compute.ApplySelectionToNumberArray(alloc, a, selection)
	case *columnar.UTF8:
		return compute.ApplySelectionToUTF8Array(alloc, a, selection)
	case *columnar.Null:
		return col, nil
	default:
		return nil, fmt.Errorf("unsupported column type for selection: %T", a)
	}
}

// evaluateSpecialBinary evaluates binary expressions for which one of the
// arguments does not evaluate into an expression of its own.
func evaluateSpecialBinary(alloc *memory.Allocator, expr *Binary, batch *columnar.RecordBatch, selection memory.Bitmap) (columnar.Datum, error) {
	switch expr.Op {
	case BinaryOpMatchRegex:
		left, err := Evaluate(alloc, expr.Left, batch, selection)
		if err != nil {
			return nil, err
		}

		right, ok := expr.Right.(*Regexp)
		if !ok {
			return nil, fmt.Errorf("right-hand side of regex match operation must be a regexp, got %T", expr.Right)
		}

		return compute.RegexpMatch(alloc, left, right.Expression, selection)
	}

	return nil, fmt.Errorf("unexpected binary operator %s", expr.Op)
}
