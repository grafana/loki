package expr_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/expr"
	"github.com/grafana/loki/v3/pkg/memory"
)

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
