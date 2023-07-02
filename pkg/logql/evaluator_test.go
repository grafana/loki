package logql

import (
	"math"
	"testing"

	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logql/syntax"
)

func TestDefaultEvaluator_DivideByZero(t *testing.T) {
	op, err := syntax.MergeBinOp(syntax.OpTypeDiv,
		&promql.Sample{
			T: 1, F: 1,
		},
		&promql.Sample{
			T: 1, F: 0,
		},
		false,
		false,
	)
	require.NoError(t, err)

	require.Equal(t, true, math.IsNaN(op.F))
	binOp, err := syntax.MergeBinOp(syntax.OpTypeMod,
		&promql.Sample{
			T: 1, F: 1,
		},
		&promql.Sample{
			T: 1, F: 0,
		},
		false,
		false,
	)
	require.NoError(t, err)
	require.Equal(t, true, math.IsNaN(binOp.F))
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
				F: 1,
			},
			&promql.Sample{
				F: 1,
			},
			&promql.Sample{
				F: 1,
			},
		},
		{
			`eq_1`,
			syntax.OpTypeCmpEQ,
			&promql.Sample{
				F: 1,
			},
			&promql.Sample{
				F: 0,
			},
			&promql.Sample{
				F: 0,
			},
		},
		{
			`neq_0`,
			syntax.OpTypeNEQ,
			&promql.Sample{
				F: 0,
			},
			&promql.Sample{
				F: 1,
			},
			&promql.Sample{
				F: 1,
			},
		},
		{
			`neq_1`,
			syntax.OpTypeNEQ,
			&promql.Sample{
				F: 1,
			},
			&promql.Sample{
				F: 1,
			},
			&promql.Sample{
				F: 0,
			},
		},
		{
			`gt_0`,
			syntax.OpTypeGT,
			&promql.Sample{
				F: 1,
			},
			&promql.Sample{
				F: 1,
			},
			&promql.Sample{
				F: 0,
			},
		},
		{
			`gt_1`,
			syntax.OpTypeGT,
			&promql.Sample{
				F: 1,
			},
			&promql.Sample{
				F: 0,
			},
			&promql.Sample{
				F: 1,
			},
		},
		{
			`lt_0`,
			syntax.OpTypeLT,
			&promql.Sample{
				F: 1,
			},
			&promql.Sample{
				F: 1,
			},
			&promql.Sample{
				F: 0,
			},
		},
		{
			`lt_1`,
			syntax.OpTypeLT,
			&promql.Sample{
				F: 0,
			},
			&promql.Sample{
				F: 1,
			},
			&promql.Sample{
				F: 1,
			},
		},
		{
			`gte_0`,
			syntax.OpTypeGTE,
			&promql.Sample{
				F: 1,
			},
			&promql.Sample{
				F: 1,
			},
			&promql.Sample{
				F: 1,
			},
		},
		{
			`gt_1`,
			syntax.OpTypeGTE,
			&promql.Sample{
				F: 0,
			},
			&promql.Sample{
				F: 1,
			},
			&promql.Sample{
				F: 0,
			},
		},
		{
			`lte_0`,
			syntax.OpTypeLTE,
			&promql.Sample{
				F: 0,
			},
			&promql.Sample{
				F: 0,
			},
			&promql.Sample{
				F: 1,
			},
		},
		{
			`lte_1`,
			syntax.OpTypeLTE,
			&promql.Sample{
				F: 1,
			},
			&promql.Sample{
				F: 0,
			},
			&promql.Sample{
				F: 0,
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			// comparing a binop should yield the unfiltered (non-nil variant) regardless
			// of whether this is a vector-vector comparison or not.
			op, err := syntax.MergeBinOp(tc.op, tc.lhs, tc.rhs, false, false)
			require.NoError(t, err)
			require.Equal(t, tc.expected, op)
			op2, err := syntax.MergeBinOp(tc.op, tc.lhs, tc.rhs, false, true)
			require.NoError(t, err)
			require.Equal(t, tc.expected, op2)

			op3, err := syntax.MergeBinOp(tc.op, tc.lhs, nil, false, true)
			require.NoError(t, err)
			require.Nil(t, op3)

			//  test filtered variants
			if tc.expected.F == 0 {
				//  ensure zeroed predicates are filtered out

				op, err := syntax.MergeBinOp(tc.op, tc.lhs, tc.rhs, true, false)
				require.NoError(t, err)
				require.Nil(t, op)
				op2, err := syntax.MergeBinOp(tc.op, tc.lhs, tc.rhs, true, true)
				require.NoError(t, err)
				require.Nil(t, op2)

				// for vector-vector comparisons, ensure that nil right hand sides
				// translate into nil results
				op3, err := syntax.MergeBinOp(tc.op, tc.lhs, nil, true, true)
				require.NoError(t, err)
				require.Nil(t, op3)

			}
		})
	}
}
