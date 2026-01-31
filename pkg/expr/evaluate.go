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
		return evaluateUnary(alloc, expr, batch)

	case *Binary:
		return evaluateBinary(alloc, expr, batch)

	default:
		panic(fmt.Sprintf("unexpected expression type %T", expr))
	}
}

func evaluateUnary(alloc *memory.Allocator, expr *Unary, batch *columnar.RecordBatch) (columnar.Datum, error) {
	switch expr.Op {
	case UnaryOpNOT:
		value, err := Evaluate(alloc, expr.Value, batch)
		if err != nil {
			return nil, err
		}
		return compute.Not(alloc, value)
	}

	return nil, fmt.Errorf("unexpected unary operator %s", expr.Op)
}

func evaluateBinary(alloc *memory.Allocator, expr *Binary, batch *columnar.RecordBatch) (columnar.Datum, error) {
	// TODO(rfratto): If expr.Op is [BinaryOpAND] or [BinaryOpOR], we can
	// propagate selection vectors to avoid unnecessary evaluations.
	left, err := Evaluate(alloc, expr.Left, batch)
	if err != nil {
		return nil, err
	}

	right, err := Evaluate(alloc, expr.Right, batch)
	if err != nil {
		return nil, err
	}

	switch expr.Op {
	case BinaryOpEQ:
		return compute.Equals(alloc, left, right)
	case BinaryOpNEQ:
		return compute.NotEquals(alloc, left, right)
	case BinaryOpGT:
		return compute.GreaterThan(alloc, left, right)
	case BinaryOpGTE:
		return compute.GreaterOrEqual(alloc, left, right)
	case BinaryOpLT:
		return compute.LessThan(alloc, left, right)
	case BinaryOpLTE:
		return compute.LessOrEqual(alloc, left, right)
	case BinaryOpAND:
		return compute.And(alloc, left, right)
	case BinaryOpOR:
		return compute.Or(alloc, left, right)
	case BinaryOpHasSubstrIgnoreCase:
		return compute.SubstrInsensitive(alloc, left, right)
	}

	return nil, fmt.Errorf("unexpected binary operator %s", expr.Op)
}
