package expr_test

import (
	"testing"

	"github.com/grafana/regexp"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/columnar/types"
	"github.com/grafana/loki/v3/pkg/expr"
	"github.com/grafana/loki/v3/pkg/memory"
)

// TestEvaluate performs a basic end-to-end test of expression evaluation.
func TestEvaluate(t *testing.T) {
	var alloc memory.Allocator

	record := columnartest.Struct(t, &alloc,
		columnartest.Field("name", types.KindUTF8, "Peter", "Paul", "Mary"),
		columnartest.Field("age", types.KindUint64, 30, 25, 43),
	)

	// (name != "Paul" AND age > 25)
	e := &expr.Binary{
		Left: &expr.Binary{
			Left:  &expr.Column{Name: "name"},
			Op:    expr.BinaryOpNEQ,
			Right: &expr.Constant{Value: columnartest.Scalar(t, types.KindUTF8, "Paul")},
		},
		Op: expr.BinaryOpAND,
		Right: &expr.Binary{
			Left:  &expr.Column{Name: "age"},
			Op:    expr.BinaryOpGT,
			Right: &expr.Constant{Value: columnartest.Scalar(t, types.KindUint64, 25)},
		},
	}

	expect := columnartest.Array(t, types.KindBool, &alloc, true, false, true)

	result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
	require.NoError(t, err)
	columnartest.RequireDatumsEqual(t, expect, result, memory.Bitmap{})
}

func TestEvaluate_Constant(t *testing.T) {
	var alloc memory.Allocator

	e := &expr.Constant{Value: columnartest.Scalar(t, types.KindUint64, 42)}

	expect := columnartest.Scalar(t, types.KindUint64, 42)

	result, err := expr.Evaluate(&alloc, e, nil, memory.Bitmap{})
	require.NoError(t, err)
	columnartest.RequireDatumsEqual(t, expect, result, memory.Bitmap{})
}

func TestEvaluate_Column(t *testing.T) {
	var alloc memory.Allocator

	record := columnartest.Struct(t, &alloc,
		columnartest.Field("name", types.KindUTF8, "Alice", "Bob", "Charlie"),
		columnartest.Field("age", types.KindUint64, 30, 25, 35),
		columnartest.Field("city", types.KindUTF8, "NYC", "LA", "SF"),
	)

	t.Run("existing column", func(t *testing.T) {
		e := &expr.Column{Name: "age"}

		expect := columnartest.Array(t, types.KindUint64, &alloc, 30, 25, 35)

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result, memory.Bitmap{})
	})

	t.Run("non-existing column", func(t *testing.T) {
		e := &expr.Column{Name: "nonexistent"}

		expect := columnartest.Array(t, types.KindNull, &alloc, nil, nil, nil)

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result, memory.Bitmap{})
	})
}

func TestEvaluate_Column_Struct(t *testing.T) {
	var alloc memory.Allocator

	schema := columnar.NewSchema([]columnar.Column{{Name: "x"}, {Name: "y"}})
	input := columnar.NewStruct(schema, []columnar.Array{
		columnartest.Array(t, types.KindInt64, &alloc, int64(10), int64(20)),
		columnartest.Array(t, types.KindUTF8, &alloc, "a", "b"),
	}, 2, memory.Bitmap{})

	// Resolve column "x".
	result, err := expr.Evaluate(&alloc, &expr.Column{Name: "x"}, input, memory.Bitmap{})
	require.NoError(t, err)
	columnartest.RequireArraysEqual(t,
		columnartest.Array(t, types.KindInt64, &alloc, int64(10), int64(20)),
		result.(columnar.Array),
		memory.Bitmap{},
	)

	// Unknown column resolves to null.
	result, err = expr.Evaluate(&alloc, &expr.Column{Name: "missing"}, input, memory.Bitmap{})
	require.NoError(t, err)
	require.Equal(t, types.KindNull, result.Kind())
	require.Equal(t, 2, result.(*columnar.Null).Len())
}

func TestEvaluate_Extract(t *testing.T) {
	var alloc memory.Allocator

	record := columnartest.Struct(t, &alloc,
		columnartest.Field("name", types.KindUTF8, "Alice", "Bob", "Charlie"),
		columnartest.Field("age", types.KindUint64, 30, 25, 35),
	)

	t.Run("existing field", func(t *testing.T) {
		e := &expr.Extract{Name: "age", Value: &expr.Identity{}}

		expect := columnartest.Array(t, types.KindUint64, &alloc, 30, 25, 35)

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result, memory.Bitmap{})
	})

	t.Run("missing field", func(t *testing.T) {
		e := &expr.Extract{Name: "missing", Value: &expr.Identity{}}

		expect := columnartest.Array(t, types.KindNull, &alloc, nil, nil, nil)

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result, memory.Bitmap{})
	})

	t.Run("non-struct value", func(t *testing.T) {
		e := &expr.Extract{
			Name:  "x",
			Value: &expr.Constant{Value: columnartest.Scalar(t, types.KindUTF8, "hello")},
		}

		_, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.Error(t, err)
	})
}

func TestEvaluate_Include(t *testing.T) {
	var alloc memory.Allocator

	record := columnartest.Struct(t, &alloc,
		columnartest.Field("name", types.KindUTF8, "Alice", "Bob", "Charlie"),
		columnartest.Field("age", types.KindUint64, 30, 25, 35),
		columnartest.Field("city", types.KindUTF8, "NYC", "LA", "SF"),
	)

	t.Run("subset of fields", func(t *testing.T) {
		e := &expr.Include{Names: []string{"name", "city"}, Value: &expr.Identity{}}

		expect := columnartest.Struct(t, &alloc,
			columnartest.Field("name", types.KindUTF8, "Alice", "Bob", "Charlie"),
			columnartest.Field("city", types.KindUTF8, "NYC", "LA", "SF"),
		)

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result, memory.Bitmap{})
	})

	t.Run("missing names skipped", func(t *testing.T) {
		e := &expr.Include{Names: []string{"name", "missing"}, Value: &expr.Identity{}}

		expect := columnartest.Struct(t, &alloc,
			columnartest.Field("name", types.KindUTF8, "Alice", "Bob", "Charlie"),
		)

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result, memory.Bitmap{})
	})

	t.Run("non-struct value", func(t *testing.T) {
		e := &expr.Include{
			Names: []string{"x"},
			Value: &expr.Constant{Value: columnartest.Scalar(t, types.KindUTF8, "hello")},
		}

		_, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.Error(t, err)
	})
}

func TestEvaluate_Exclude(t *testing.T) {
	var alloc memory.Allocator

	record := columnartest.Struct(t, &alloc,
		columnartest.Field("name", types.KindUTF8, "Alice", "Bob", "Charlie"),
		columnartest.Field("age", types.KindUint64, 30, 25, 35),
		columnartest.Field("city", types.KindUTF8, "NYC", "LA", "SF"),
	)

	t.Run("exclude some fields", func(t *testing.T) {
		e := &expr.Exclude{Names: []string{"age"}, Value: &expr.Identity{}}

		expect := columnartest.Struct(t, &alloc,
			columnartest.Field("name", types.KindUTF8, "Alice", "Bob", "Charlie"),
			columnartest.Field("city", types.KindUTF8, "NYC", "LA", "SF"),
		)

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result, memory.Bitmap{})
	})

	t.Run("exclude non-existing names", func(t *testing.T) {
		e := &expr.Exclude{Names: []string{"missing"}, Value: &expr.Identity{}}

		expect := columnartest.Struct(t, &alloc,
			columnartest.Field("name", types.KindUTF8, "Alice", "Bob", "Charlie"),
			columnartest.Field("age", types.KindUint64, 30, 25, 35),
			columnartest.Field("city", types.KindUTF8, "NYC", "LA", "SF"),
		)

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result, memory.Bitmap{})
	})

	t.Run("exclude all fields", func(t *testing.T) {
		e := &expr.Exclude{Names: []string{"name", "age", "city"}, Value: &expr.Identity{}}

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)

		s := result.(*columnar.Struct)
		require.Equal(t, 0, s.NumFields())
		require.Equal(t, 3, s.Len())
	})

	t.Run("non-struct value", func(t *testing.T) {
		e := &expr.Exclude{
			Names: []string{"x"},
			Value: &expr.Constant{Value: columnartest.Scalar(t, types.KindUTF8, "hello")},
		}

		_, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.Error(t, err)
	})
}

func TestEvaluate_MakeStruct(t *testing.T) {
	var alloc memory.Allocator

	record := columnartest.Struct(t, &alloc,
		columnartest.Field("name", types.KindUTF8, "Alice", "Bob", "Charlie"),
		columnartest.Field("age", types.KindUint64, 30, 25, 35),
	)

	t.Run("construct from columns", func(t *testing.T) {
		e := &expr.MakeStruct{
			Names:  []string{"who", "years"},
			Values: []expr.Expression{&expr.Column{Name: "name"}, &expr.Column{Name: "age"}},
		}

		expect := columnartest.Struct(t, &alloc,
			columnartest.Field("who", types.KindUTF8, "Alice", "Bob", "Charlie"),
			columnartest.Field("years", types.KindUint64, 30, 25, 35),
		)

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result, memory.Bitmap{})
	})

	t.Run("mismatched names and values", func(t *testing.T) {
		e := &expr.MakeStruct{
			Names:  []string{"a", "b"},
			Values: []expr.Expression{&expr.Column{Name: "name"}},
		}

		_, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.Error(t, err)
	})

	t.Run("duplicate names rejected", func(t *testing.T) {
		e := &expr.MakeStruct{
			Names:  []string{"x", "x"},
			Values: []expr.Expression{&expr.Column{Name: "name"}, &expr.Column{Name: "age"}},
		}

		_, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.Error(t, err)
	})

	t.Run("scalar value rejected", func(t *testing.T) {
		e := &expr.MakeStruct{
			Names:  []string{"x"},
			Values: []expr.Expression{&expr.Constant{Value: columnartest.Scalar(t, types.KindUTF8, "hello")}},
		}

		_, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.Error(t, err)
	})
}

func TestEvaluate_Unary(t *testing.T) {
	var alloc memory.Allocator

	record := columnartest.Struct(t, &alloc,
		columnartest.Field("active", types.KindBool, true, false, true),
	)

	e := &expr.Unary{
		Op:    expr.UnaryOpNOT,
		Value: &expr.Column{Name: "active"},
	}

	expect := columnartest.Array(t, types.KindBool, &alloc, false, true, false)

	result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
	require.NoError(t, err)
	columnartest.RequireDatumsEqual(t, expect, result, memory.Bitmap{})
}

func TestEvaluate_Binary(t *testing.T) {
	var alloc memory.Allocator

	record := columnartest.Struct(t, &alloc,
		columnartest.Field("name", types.KindUTF8, "Alice", "Bob", "Charlie"),
		columnartest.Field("age", types.KindUint64, 30, 25, 35),
		columnartest.Field("active", types.KindBool, true, false, true),
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
			right:  &expr.Constant{Value: columnartest.Scalar(t, types.KindUint64, 30)},
			expect: columnartest.Array(t, types.KindBool, &alloc, true, false, false),
		},
		{
			op:     expr.BinaryOpNEQ,
			left:   &expr.Column{Name: "age"},
			right:  &expr.Constant{Value: columnartest.Scalar(t, types.KindUint64, 30)},
			expect: columnartest.Array(t, types.KindBool, &alloc, false, true, true),
		},
		{
			op:     expr.BinaryOpGT,
			left:   &expr.Column{Name: "age"},
			right:  &expr.Constant{Value: columnartest.Scalar(t, types.KindUint64, 25)},
			expect: columnartest.Array(t, types.KindBool, &alloc, true, false, true),
		},
		{
			op:     expr.BinaryOpGTE,
			left:   &expr.Column{Name: "age"},
			right:  &expr.Constant{Value: columnartest.Scalar(t, types.KindUint64, 30)},
			expect: columnartest.Array(t, types.KindBool, &alloc, true, false, true),
		},
		{
			op:     expr.BinaryOpLT,
			left:   &expr.Column{Name: "age"},
			right:  &expr.Constant{Value: columnartest.Scalar(t, types.KindUint64, 30)},
			expect: columnartest.Array(t, types.KindBool, &alloc, false, true, false),
		},
		{
			op:     expr.BinaryOpLTE,
			left:   &expr.Column{Name: "age"},
			right:  &expr.Constant{Value: columnartest.Scalar(t, types.KindUint64, 30)},
			expect: columnartest.Array(t, types.KindBool, &alloc, true, true, false),
		},
		{
			op:     expr.BinaryOpAND,
			left:   &expr.Column{Name: "active"},
			right:  &expr.Constant{Value: columnartest.Scalar(t, types.KindBool, true)},
			expect: columnartest.Array(t, types.KindBool, &alloc, true, false, true),
		},
		{
			op:     expr.BinaryOpOR,
			left:   &expr.Column{Name: "active"},
			right:  &expr.Constant{Value: columnartest.Scalar(t, types.KindBool, false)},
			expect: columnartest.Array(t, types.KindBool, &alloc, true, false, true),
		},
		{
			op:     expr.BinaryOpMatchRegex,
			left:   &expr.Column{Name: "name"},
			right:  &expr.Regexp{Expression: regexp.MustCompile("(?i)((al|ch).*)")},
			expect: columnartest.Array(t, types.KindBool, &alloc, true, false, true),
		},
		{
			op:     expr.BinaryOpHasSubstr,
			left:   &expr.Column{Name: "name"},
			right:  &expr.Constant{Value: columnartest.Scalar(t, types.KindUTF8, "li")},
			expect: columnartest.Array(t, types.KindBool, &alloc, true, false, true),
		},
		{
			op:     expr.BinaryOpHasSubstrIgnoreCase,
			left:   &expr.Column{Name: "name"},
			right:  &expr.Constant{Value: columnartest.Scalar(t, types.KindUTF8, "LI")},
			expect: columnartest.Array(t, types.KindBool, &alloc, true, false, true),
		},
	}

	for _, tt := range tests {
		t.Run(tt.op.String(), func(t *testing.T) {
			e := &expr.Binary{
				Left:  tt.left,
				Op:    tt.op,
				Right: tt.right,
			}

			result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
			require.NoError(t, err)
			columnartest.RequireDatumsEqual(t, tt.expect, result, memory.Bitmap{})
		})
	}
}
