package expr_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/columnar/types"
	"github.com/grafana/loki/v3/pkg/expr"
	"github.com/grafana/loki/v3/pkg/memory"
)

func TestReduce_Leaf(t *testing.T) {
	tests := []struct {
		name       string
		expression expr.Expression
	}{
		{name: "constant", expression: &expr.Constant{}},
		{name: "identity", expression: &expr.Identity{}},
		{name: "column", expression: &expr.Column{Name: "foo"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expected := &columnar.NumberScalar[int64]{Value: 5}

			actual, err := expr.Reduce(nil, tc.expression, func(got expr.Expression) (columnar.Datum, error) {
				require.Same(t, tc.expression, got)
				return expected, nil
			}, memory.Bitmap{})
			require.NoError(t, err)

			require.Same(t, expected, actual)
		})
	}
}

func TestReduce_ExtractPropagatesStructValidity(t *testing.T) {
	var alloc memory.Allocator
	input := columnartest.StructWithValidity(t, &alloc, []bool{true, false, true},
		columnartest.Field("foo", types.KindInt64, int64(10), int64(20), int64(30)),
	)

	actual, err := expr.Reduce(&alloc, &expr.Extract{
		Name:  "foo",
		Value: &expr.Identity{},
	}, func(e expr.Expression) (columnar.Datum, error) {
		require.IsType(t, &expr.Identity{}, e)
		return input, nil
	}, memory.Bitmap{})
	require.NoError(t, err)

	columnartest.RequireDatumsEqual(t,
		columnartest.Array(t, types.KindInt64, &alloc, int64(10), nil, int64(30)),
		actual,
		memory.Bitmap{},
	)
}

func TestReduce_DuplicateFieldNames(t *testing.T) {
	var alloc memory.Allocator

	tests := map[string]expr.Expression{
		"make struct": &expr.MakeStruct{
			Names:  []string{"x", "x"},
			Values: []expr.Expression{&expr.Identity{}, &expr.Identity{}},
		},
		"include": &expr.Include{
			Names: []string{"x", "x"},
			Value: &expr.Identity{},
		},
	}
	for name, expression := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := expr.Reduce(&alloc, expression, func(expr.Expression) (columnar.Datum, error) {
				return nil, nil
			}, memory.Bitmap{})
			require.ErrorContains(t, err, "duplicate field name")
		})
	}
}
