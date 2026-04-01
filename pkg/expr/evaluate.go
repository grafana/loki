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
// The return type of Evaluate depends on the expression provided. See the
// documentation for implementations of Expression for what they produce when
// evaluated.
func Evaluate(alloc *memory.Allocator, expr Expression, batch *columnar.RecordBatch) (columnar.Datum, error) {
	// Selection defines which rows are evaluated. Selected rows have a true value
	// in the selection vector. When selection has Len() == 0, all rows are selected.
	// When selection has Len() > 0, only rows with a true bit are selected. Rows that
	// are not selected are treated as null in array results.
	//
	// We hide selection vector from the caller because we don't support selection on expressions that are columns.
	// E.g., evaluateWithSelection(alloc, &expr.Column{...}, batch, onlyFirstAndThirdRows) will return all the rows.
	// This is not a problem though as selection vector can be not "allSelected" only when the expression contains a
	// BinOp that can be short-circuited.
	allSelected := memory.Bitmap{}

	return evaluateWithSelection(alloc, expr, batch, allSelected)
}

func evaluateWithSelection(alloc *memory.Allocator, expr Expression, batch *columnar.RecordBatch, selection memory.Bitmap) (columnar.Datum, error) {
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

		return batch.Column(int64(columnIndex)), nil

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
		value, err := evaluateWithSelection(alloc, expr.Value, batch, selection)
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
	case BinaryOpMatchRegex, BinaryOpIn:
		return evaluateSpecialBinary(alloc, expr, batch, selection)
	}

	// TODO(rfratto): If expr.Op is [BinaryOpAND] or [BinaryOpOR], we can
	// propagate selection vectors to avoid unnecessary evaluations.
	left, err := evaluateWithSelection(alloc, expr.Left, batch, selection)
	if err != nil {
		return nil, err
	}

	right, err := evaluateWithSelection(alloc, expr.Right, batch, selection)
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

// evaluateSpecialBinary evaluates binary expressions for which one of the
// arguments does not evaluate into an expression of its own.
func evaluateSpecialBinary(alloc *memory.Allocator, expr *Binary, batch *columnar.RecordBatch, selection memory.Bitmap) (columnar.Datum, error) {
	switch expr.Op {
	case BinaryOpMatchRegex:
		left, err := evaluateWithSelection(alloc, expr.Left, batch, selection)
		if err != nil {
			return nil, err
		}

		right, ok := expr.Right.(*Regexp)
		if !ok {
			return nil, fmt.Errorf("right-hand side of regex match operation must be a regexp, got %T", expr.Right)
		}

		return compute.RegexpMatch(alloc, left, right.Expression, selection)
	case BinaryOpIn:
		left, err := evaluateWithSelection(alloc, expr.Left, batch, selection)
		if err != nil {
			return nil, err
		}

		right, ok := expr.Right.(*ValueSet)
		if !ok {
			return nil, fmt.Errorf("right-hand side of in operation must be a ValueSet, got %T", expr.Right)
		}

		return compute.IsMember(alloc, left, right.Values, selection)
	}

	return nil, fmt.Errorf("unexpected binary operator %s", expr.Op)
}
