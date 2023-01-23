package logql

import (
	"math"
	"testing"

	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logql/syntax"
)

func TestDefaultEvaluator_DivideByZero(t *testing.T) {
	require.Equal(t, true, math.IsNaN(syntax.MergeBinOp(syntax.OpTypeDiv,
		&promql.Sample{
			Point: promql.Point{T: 1, V: 1},
		},
		&promql.Sample{
			Point: promql.Point{T: 1, V: 0},
		},
		false,
		false,
	).Point.V))

	require.Equal(t, true, math.IsNaN(syntax.MergeBinOp(syntax.OpTypeMod,
		&promql.Sample{
			Point: promql.Point{T: 1, V: 1},
		},
		&promql.Sample{
			Point: promql.Point{T: 1, V: 0},
		},
		false,
		false,
	).Point.V))
}
func TestDefaultEvaluator_Sortable(t *testing.T) {
	logqlSort := `sort(rate(({app=~"foo|bar"} |~".+bar")[1m])) `
	sortable, err := Sortable(LiteralParams{qs: logqlSort})
	if err != nil {
		t.Fatal(err)
	}
	require.Equal(t, true, sortable)

	logqlSum := `sum(rate(({app=~"foo|bar"} |~".+bar")[1m])) `
	sortableSum, err := Sortable(LiteralParams{qs: logqlSum})
	if err != nil {
		t.Fatal(err)
	}
	require.Equal(t, false, sortableSum)

}
func TestEvaluator_mergeBinOpComparisons(t *testing.T) {
	for _, tc := range []struct {
		desc     string
		op       string
		lhs, rhs *promql.Sample
		expected *promql.Sample
	}{
		{
			`eq_0`,
			syntax.OpTypeCmpEQ,
			&promql.Sample{
				Point: promql.Point{V: 1},
			},
			&promql.Sample{
				Point: promql.Point{V: 1},
			},
			&promql.Sample{
				Point: promql.Point{V: 1},
			},
		},
		{
			`eq_1`,
			syntax.OpTypeCmpEQ,
			&promql.Sample{
				Point: promql.Point{V: 1},
			},
			&promql.Sample{
				Point: promql.Point{V: 0},
			},
			&promql.Sample{
				Point: promql.Point{V: 0},
			},
		},
		{
			`neq_0`,
			syntax.OpTypeNEQ,
			&promql.Sample{
				Point: promql.Point{V: 0},
			},
			&promql.Sample{
				Point: promql.Point{V: 1},
			},
			&promql.Sample{
				Point: promql.Point{V: 1},
			},
		},
		{
			`neq_1`,
			syntax.OpTypeNEQ,
			&promql.Sample{
				Point: promql.Point{V: 1},
			},
			&promql.Sample{
				Point: promql.Point{V: 1},
			},
			&promql.Sample{
				Point: promql.Point{V: 0},
			},
		},
		{
			`gt_0`,
			syntax.OpTypeGT,
			&promql.Sample{
				Point: promql.Point{V: 1},
			},
			&promql.Sample{
				Point: promql.Point{V: 1},
			},
			&promql.Sample{
				Point: promql.Point{V: 0},
			},
		},
		{
			`gt_1`,
			syntax.OpTypeGT,
			&promql.Sample{
				Point: promql.Point{V: 1},
			},
			&promql.Sample{
				Point: promql.Point{V: 0},
			},
			&promql.Sample{
				Point: promql.Point{V: 1},
			},
		},
		{
			`lt_0`,
			syntax.OpTypeLT,
			&promql.Sample{
				Point: promql.Point{V: 1},
			},
			&promql.Sample{
				Point: promql.Point{V: 1},
			},
			&promql.Sample{
				Point: promql.Point{V: 0},
			},
		},
		{
			`lt_1`,
			syntax.OpTypeLT,
			&promql.Sample{
				Point: promql.Point{V: 0},
			},
			&promql.Sample{
				Point: promql.Point{V: 1},
			},
			&promql.Sample{
				Point: promql.Point{V: 1},
			},
		},
		{
			`gte_0`,
			syntax.OpTypeGTE,
			&promql.Sample{
				Point: promql.Point{V: 1},
			},
			&promql.Sample{
				Point: promql.Point{V: 1},
			},
			&promql.Sample{
				Point: promql.Point{V: 1},
			},
		},
		{
			`gt_1`,
			syntax.OpTypeGTE,
			&promql.Sample{
				Point: promql.Point{V: 0},
			},
			&promql.Sample{
				Point: promql.Point{V: 1},
			},
			&promql.Sample{
				Point: promql.Point{V: 0},
			},
		},
		{
			`lte_0`,
			syntax.OpTypeLTE,
			&promql.Sample{
				Point: promql.Point{V: 0},
			},
			&promql.Sample{
				Point: promql.Point{V: 0},
			},
			&promql.Sample{
				Point: promql.Point{V: 1},
			},
		},
		{
			`lte_1`,
			syntax.OpTypeLTE,
			&promql.Sample{
				Point: promql.Point{V: 1},
			},
			&promql.Sample{
				Point: promql.Point{V: 0},
			},
			&promql.Sample{
				Point: promql.Point{V: 0},
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			// comparing a binop should yield the unfiltered (non-nil variant) regardless
			// of whether this is a vector-vector comparison or not.
			require.Equal(t, tc.expected, syntax.MergeBinOp(tc.op, tc.lhs, tc.rhs, false, false))
			require.Equal(t, tc.expected, syntax.MergeBinOp(tc.op, tc.lhs, tc.rhs, false, true))

			require.Nil(t, syntax.MergeBinOp(tc.op, tc.lhs, nil, false, true))

			//  test filtered variants
			if tc.expected.V == 0 {
				//  ensure zeroed predicates are filtered out
				require.Nil(t, syntax.MergeBinOp(tc.op, tc.lhs, tc.rhs, true, false))
				require.Nil(t, syntax.MergeBinOp(tc.op, tc.lhs, tc.rhs, true, true))

				// for vector-vector comparisons, ensure that nil right hand sides
				// translate into nil results
				require.Nil(t, syntax.MergeBinOp(tc.op, tc.lhs, nil, true, true))

			}
		})
	}
}
