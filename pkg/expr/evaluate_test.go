package expr_test

import (
	"testing"

	"github.com/grafana/regexp"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/expr"
	"github.com/grafana/loki/v3/pkg/memory"
)

// TestEvaluate performs a basic end-to-end test of expression evaluation.
func TestEvaluate(t *testing.T) {
	var alloc memory.Allocator

	record := columnar.NewRecordBatch(
		columnar.NewSchema([]columnar.Column{
			{Name: "name"},
			{Name: "age"},
		}),
		3, // row count
		[]columnar.Array{
			columnartest.Array(t, columnar.KindUTF8, &alloc, "Peter", "Paul", "Mary"),
			columnartest.Array(t, columnar.KindUint64, &alloc, 30, 25, 43),
		},
	)

	// (name != "Paul" AND age > 25)
	e := &expr.Binary{
		Left: &expr.Binary{
			Left:  &expr.Column{Name: "name"},
			Op:    expr.BinaryOpNEQ,
			Right: &expr.Constant{Value: columnartest.Scalar(t, columnar.KindUTF8, "Paul")},
		},
		Op: expr.BinaryOpAND,
		Right: &expr.Binary{
			Left:  &expr.Column{Name: "age"},
			Op:    expr.BinaryOpGT,
			Right: &expr.Constant{Value: columnartest.Scalar(t, columnar.KindUint64, 25)},
		},
	}

	expect := columnartest.Array(t, columnar.KindBool, &alloc, true, false, true)

	result, err := expr.Evaluate(&alloc, e, record)
	require.NoError(t, err)
	columnartest.RequireDatumsEqual(t, expect, result)
}

func TestEvaluate_Constant(t *testing.T) {
	var alloc memory.Allocator

	e := &expr.Constant{Value: columnartest.Scalar(t, columnar.KindUint64, 42)}

	expect := columnartest.Scalar(t, columnar.KindUint64, 42)

	result, err := expr.Evaluate(&alloc, e, nil)
	require.NoError(t, err)
	columnartest.RequireDatumsEqual(t, expect, result)
}

func TestEvaluate_Column(t *testing.T) {
	var alloc memory.Allocator

	record := columnar.NewRecordBatch(
		columnar.NewSchema([]columnar.Column{
			{Name: "name"},
			{Name: "age"},
			{Name: "city"},
		}),
		3, // row count
		[]columnar.Array{
			columnartest.Array(t, columnar.KindUTF8, &alloc, "Alice", "Bob", "Charlie"),
			columnartest.Array(t, columnar.KindUint64, &alloc, 30, 25, 35),
			columnartest.Array(t, columnar.KindUTF8, &alloc, "NYC", "LA", "SF"),
		},
	)

	t.Run("existing column", func(t *testing.T) {
		e := &expr.Column{Name: "age"}

		expect := columnartest.Array(t, columnar.KindUint64, &alloc, 30, 25, 35)

		result, err := expr.Evaluate(&alloc, e, record)
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result)
	})

	t.Run("non-existing column", func(t *testing.T) {
		e := &expr.Column{Name: "nonexistent"}

		expect := columnartest.Array(t, columnar.KindNull, &alloc, nil, nil, nil)

		result, err := expr.Evaluate(&alloc, e, record)
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result)
	})
}

func TestEvaluate_Unary(t *testing.T) {
	var alloc memory.Allocator

	record := columnar.NewRecordBatch(
		columnar.NewSchema([]columnar.Column{
			{Name: "active"},
		}),
		3, // row count
		[]columnar.Array{
			columnartest.Array(t, columnar.KindBool, &alloc, true, false, true),
		},
	)

	e := &expr.Unary{
		Op:    expr.UnaryOpNOT,
		Value: &expr.Column{Name: "active"},
	}

	expect := columnartest.Array(t, columnar.KindBool, &alloc, false, true, false)

	result, err := expr.Evaluate(&alloc, e, record)
	require.NoError(t, err)
	columnartest.RequireDatumsEqual(t, expect, result)
}

func TestEvaluate_Binary(t *testing.T) {
	var alloc memory.Allocator

	record := columnar.NewRecordBatch(
		columnar.NewSchema([]columnar.Column{
			{Name: "name"},
			{Name: "age"},
			{Name: "active"},
		}),
		3, // row count
		[]columnar.Array{
			columnartest.Array(t, columnar.KindUTF8, &alloc, "Alice", "Bob", "Charlie"),
			columnartest.Array(t, columnar.KindUint64, &alloc, 30, 25, 35),
			columnartest.Array(t, columnar.KindBool, &alloc, true, false, true),
		},
	)

	tests := []struct {
		op     expr.BinaryOp
		left   expr.Expression
		right  expr.Expression
		expect columnar.Datum
	}{
		{
			op:     expr.BinaryOpEQ,
			left:   &expr.Column{Name: "age"},
			right:  &expr.Constant{Value: columnartest.Scalar(t, columnar.KindUint64, 30)},
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, false),
		},
		{
			op:     expr.BinaryOpNEQ,
			left:   &expr.Column{Name: "age"},
			right:  &expr.Constant{Value: columnartest.Scalar(t, columnar.KindUint64, 30)},
			expect: columnartest.Array(t, columnar.KindBool, &alloc, false, true, true),
		},
		{
			op:     expr.BinaryOpGT,
			left:   &expr.Column{Name: "age"},
			right:  &expr.Constant{Value: columnartest.Scalar(t, columnar.KindUint64, 25)},
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, true),
		},
		{
			op:     expr.BinaryOpGTE,
			left:   &expr.Column{Name: "age"},
			right:  &expr.Constant{Value: columnartest.Scalar(t, columnar.KindUint64, 30)},
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, true),
		},
		{
			op:     expr.BinaryOpLT,
			left:   &expr.Column{Name: "age"},
			right:  &expr.Constant{Value: columnartest.Scalar(t, columnar.KindUint64, 30)},
			expect: columnartest.Array(t, columnar.KindBool, &alloc, false, true, false),
		},
		{
			op:     expr.BinaryOpLTE,
			left:   &expr.Column{Name: "age"},
			right:  &expr.Constant{Value: columnartest.Scalar(t, columnar.KindUint64, 30)},
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, true, false),
		},
		{
			op:     expr.BinaryOpAND,
			left:   &expr.Column{Name: "active"},
			right:  &expr.Constant{Value: columnartest.Scalar(t, columnar.KindBool, true)},
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, true),
		},
		{
			op:     expr.BinaryOpOR,
			left:   &expr.Column{Name: "active"},
			right:  &expr.Constant{Value: columnartest.Scalar(t, columnar.KindBool, false)},
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, true),
		},
		{
			op:     expr.BinaryOpMatchRegex,
			left:   &expr.Column{Name: "name"},
			right:  &expr.Regexp{Expression: regexp.MustCompile("(?i)((al|ch).*)")},
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, true),
		},
		{
			op:     expr.BinaryOpHasSubstr,
			left:   &expr.Column{Name: "name"},
			right:  &expr.Constant{Value: columnartest.Scalar(t, columnar.KindUTF8, "li")},
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, true),
		},
		{
			op:     expr.BinaryOpHasSubstrIgnoreCase,
			left:   &expr.Column{Name: "name"},
			right:  &expr.Constant{Value: columnartest.Scalar(t, columnar.KindUTF8, "LI")},
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, true),
		},
	}

	for _, tt := range tests {
		t.Run(tt.op.String(), func(t *testing.T) {
			e := &expr.Binary{
				Left:  tt.left,
				Op:    tt.op,
				Right: tt.right,
			}

			result, err := expr.Evaluate(&alloc, e, record)
			require.NoError(t, err)
			columnartest.RequireDatumsEqual(t, tt.expect, result)
		})
	}
}
