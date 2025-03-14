package logql

import (
	"math"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
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
		false,
	)
	require.NoError(t, err)
	require.Equal(t, true, math.IsNaN(binOp.F))
}
func TestDefaultEvaluator_Sortable(t *testing.T) {
	logqlSort := `sort(rate(({app=~"foo|bar"} |~".+bar")[1m])) `
	sortable, err := Sortable(LiteralParams{queryString: logqlSort, queryExpr: syntax.MustParseExpr(logqlSort)})
	if err != nil {
		t.Fatal(err)
	}
	require.Equal(t, true, sortable)

	logqlSum := `sum(rate(({app=~"foo|bar"} |~".+bar")[1m])) `
	sortableSum, err := Sortable(LiteralParams{queryString: logqlSum, queryExpr: syntax.MustParseExpr(logqlSum)})
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
			op, err := syntax.MergeBinOp(tc.op, tc.lhs, tc.rhs, false, false, false)
			require.NoError(t, err)
			require.Equal(t, tc.expected, op)
			op2, err := syntax.MergeBinOp(tc.op, tc.lhs, tc.rhs, false, false, true)
			require.NoError(t, err)
			require.Equal(t, tc.expected, op2)

			op3, err := syntax.MergeBinOp(tc.op, tc.lhs, nil, false, false, true)
			require.NoError(t, err)
			require.Nil(t, op3)

			//  test filtered variants
			if tc.expected.F == 0 {
				//  ensure zeroed predicates are filtered out

				op, err := syntax.MergeBinOp(tc.op, tc.lhs, tc.rhs, false, true, false)
				require.NoError(t, err)
				require.Nil(t, op)
				op2, err := syntax.MergeBinOp(tc.op, tc.lhs, tc.rhs, false, true, true)
				require.NoError(t, err)
				require.Nil(t, op2)

				// for vector-vector comparisons, ensure that nil right hand sides
				// translate into nil results
				op3, err := syntax.MergeBinOp(tc.op, tc.lhs, nil, false, true, true)
				require.NoError(t, err)
				require.Nil(t, op3)

			}
		})
	}
}

func TestEmptyNestedEvaluator(t *testing.T) {

	for _, tc := range []struct {
		desc string
		ev   StepEvaluator
	}{
		{
			desc: "LiteralStepEvaluator",
			ev:   &LiteralStepEvaluator{nextEv: &emptyEvaluator{}},
		},
		{
			desc: "LabelReplaceEvaluator",
			ev:   &LabelReplaceEvaluator{nextEvaluator: &emptyEvaluator{}},
		},
		{
			desc: "BinOpStepEvaluator",
			ev:   &BinOpStepEvaluator{rse: &emptyEvaluator{}},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			ok, _, _ := tc.ev.Next()
			require.False(t, ok)
		})
	}

}

func TestLiteralStepEvaluator(t *testing.T) {
	cases := []struct {
		name     string
		expr     *LiteralStepEvaluator
		expected []float64
	}{
		{
			name: "vector op scalar",
			// e.g: sum(count_over_time({app="foo"}[1m])) > 20
			expr: &LiteralStepEvaluator{
				nextEv:   newReturnVectorEvaluator([]float64{20, 21, 22, 23}),
				val:      20,
				inverted: true, //  set to true, because literal expression is not on left.
				op:       ">",
			},
			expected: []float64{21, 22, 23},
		},
		{
			name: "scalar op vector",
			// e.g: 20 < sum(count_over_time({app="foo"}[1m]))
			expr: &LiteralStepEvaluator{
				nextEv:   newReturnVectorEvaluator([]float64{20, 21, 22, 23}),
				val:      20,
				inverted: false,
				op:       "<",
			},
			expected: []float64{21, 22, 23},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, _, got := tc.expr.Next()
			require.True(t, ok)
			vecs := got.SampleVector()
			gotSamples := make([]float64, 0, len(vecs))

			for _, v := range vecs {
				gotSamples = append(gotSamples, v.F)
			}

			assert.Equal(t, tc.expected, gotSamples)
		})
	}
}

type emptyEvaluator struct{}

func (*emptyEvaluator) Next() (ok bool, ts int64, r StepResult) {
	return false, 0, nil
}

func (*emptyEvaluator) Close() error {
	return nil
}

func (*emptyEvaluator) Error() error {
	return nil
}

func (*emptyEvaluator) Explain(Node) {}

// returnVectorEvaluator returns elements of vector
// passed in, everytime it's `Next()` is called. Used for testing.
type returnVectorEvaluator struct {
	vec promql.Vector
}

func (e *returnVectorEvaluator) Next() (ok bool, ts int64, r StepResult) {
	return true, 0, SampleVector(e.vec)
}

func (*returnVectorEvaluator) Close() error {
	return nil
}

func (*returnVectorEvaluator) Error() error {
	return nil
}

func (*returnVectorEvaluator) Explain(Node) {

}

func newReturnVectorEvaluator(vec []float64) *returnVectorEvaluator {
	testTime := time.Now().Unix()

	pvec := make([]promql.Sample, 0, len(vec))

	for _, v := range vec {
		pvec = append(pvec, promql.Sample{
			T:      testTime,
			F:      v,
			Metric: labels.FromStrings("foo", "bar"),
		})
	}

	return &returnVectorEvaluator{
		vec: pvec,
	}
}
