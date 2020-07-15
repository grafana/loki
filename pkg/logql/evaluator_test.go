package logql

import (
	"math"
	"testing"

	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"
)

func TestDefaultEvaluator_DivideByZero(t *testing.T) {

	require.Equal(t, true, math.IsNaN(mergeBinOp(OpTypeDiv,
		&promql.Sample{
			Point: promql.Point{T: 1, V: 1},
		},
		&promql.Sample{
			Point: promql.Point{T: 1, V: 0},
		},
		false,
		false,
	).Point.V))

	require.Equal(t, true, math.IsNaN(mergeBinOp(OpTypeMod,
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

func TestEvaluator_mergeBinOpComparisons(t *testing.T) {
	for _, tc := range []struct {
		desc     string
		op       string
		lhs, rhs *promql.Sample
		expected *promql.Sample
	}{
		{
			`eq_0`,
			OpTypeCmpEQ,
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
			OpTypeCmpEQ,
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
			OpTypeNEQ,
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
			OpTypeNEQ,
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
			OpTypeGT,
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
			OpTypeGT,
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
			OpTypeLT,
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
			OpTypeLT,
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
			OpTypeGTE,
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
			OpTypeGTE,
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
			OpTypeLTE,
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
			OpTypeLTE,
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
			require.Equal(t, tc.expected, mergeBinOp(tc.op, tc.lhs, tc.rhs, false, false))
			require.Equal(t, tc.expected, mergeBinOp(tc.op, tc.lhs, tc.rhs, false, true))

			// vector-vector comparing when not filtering should propagate the labels for nil right hand side matches,
			// but set the value to zero.
			require.Equal(
				t,
				&promql.Sample{
					Point: promql.Point{V: 0},
				},
				mergeBinOp(tc.op, tc.lhs, nil, false, true),
			)

			//  test filtered variants
			if tc.expected.V == 0 {
				//  ensure zeroed predicates are filtered out
				require.Nil(t, mergeBinOp(tc.op, tc.lhs, tc.rhs, true, false))
				require.Nil(t, mergeBinOp(tc.op, tc.lhs, tc.rhs, true, true))

				// for vector-vector comparisons, ensure that nil right hand sides
				// translate into nil results
				require.Nil(t, mergeBinOp(tc.op, tc.lhs, nil, true, true))

			}

		})
	}
}

func Test_MergeBinOpVectors_Filter(t *testing.T) {
	res := mergeBinOp(
		OpTypeGT,
		&promql.Sample{
			Point: promql.Point{V: 2},
		},
		&promql.Sample{
			Point: promql.Point{V: 0},
		},
		true,
		true,
	)

	// ensure we return the left hand side's value (2) instead of the
	// comparison operator's result (1: the truthy answer)
	require.Equal(t, &promql.Sample{
		Point: promql.Point{V: 2},
	}, res)
}
