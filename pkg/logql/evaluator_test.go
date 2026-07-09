package logql

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
)

type hintCapturingQuerier struct {
	logParams    SelectLogParams
	sampleParams SelectSampleParams
}

func (q *hintCapturingQuerier) SelectLogs(_ context.Context, params SelectLogParams) (iter.EntryIterator, error) {
	q.logParams = params
	return iter.NoopEntryIterator, nil
}

func (q *hintCapturingQuerier) SelectSamples(_ context.Context, params SelectSampleParams) (iter.SampleIterator, error) {
	q.sampleParams = params
	return iter.NoopSampleIterator, nil
}

func TestDefaultEvaluatorPropagatesHintRanges(t *testing.T) {
	start := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	hintRanges := []logproto.HintTimeRange{{
		Start: start.Add(5 * time.Minute),
		End:   start.Add(10 * time.Minute),
	}}
	querier := &hintCapturingQuerier{}
	evaluator := NewDefaultEvaluator(querier, 0, 0)

	logQuery := `{app="foo"} |= "error"`
	logExpr := syntax.MustParseExpr(logQuery).(syntax.LogSelectorExpr)
	logParams := LiteralParams{
		queryString: logQuery,
		queryExpr:   logExpr,
		start:       start,
		end:         start.Add(time.Hour),
		hintRanges:  hintRanges,
	}
	iterator, err := evaluator.NewIterator(context.Background(), logExpr, logParams)
	require.NoError(t, err)
	require.NoError(t, iterator.Close())
	require.Equal(t, hintRanges, querier.logParams.HintRanges)

	sampleQuery := `rate({app="foo"}[1m])`
	sampleExpr := syntax.MustParseExpr(sampleQuery).(syntax.SampleExpr)
	sampleParams := LiteralParams{
		queryString: sampleQuery,
		queryExpr:   sampleExpr,
		start:       start,
		end:         start.Add(time.Hour),
		step:        time.Minute,
		hintRanges:  hintRanges,
	}
	stepEvaluator, err := evaluator.NewStepEvaluator(context.Background(), evaluator, sampleExpr, sampleParams)
	require.NoError(t, err)
	require.NoError(t, stepEvaluator.Close())
	require.Equal(t, hintRanges, querier.sampleParams.HintRanges)
}

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

// TestNewVectorAggEvaluator_DoesNotMutateGroupingInPlace ensure the expression
// groups are not mutated in place. Another VectorAggregationExpr evaluated concurrently
// (e.g. the sum/count legs of a sharded avg_over_time) may hold the same
// Grouping.Groups backing slice, so newVectorAggEvaluator() must sort a
// private copy rather than the AST node's own slice.
func TestNewVectorAggEvaluator_DoesNotMutateGroupingInPlace(t *testing.T) {
	expr := &syntax.VectorAggregationExpr{
		Left:      &syntax.VectorExpr{Val: 1},
		Operation: syntax.OpTypeSum,
		Grouping:  &syntax.Grouping{Groups: []string{"b", "a"}},
	}

	nextEvFactory := SampleEvaluatorFunc(func(_ context.Context, _ SampleEvaluatorFactory, _ syntax.SampleExpr, _ Params) (StepEvaluator, error) {
		return &emptyEvaluator{}, nil
	})

	_, err := newVectorAggEvaluator(context.Background(), nextEvFactory, expr, nil, 0)
	require.NoError(t, err)
	require.Equal(t, []string{"b", "a"}, expr.Grouping.Groups)
}

func TestVectorAggEvaluator_MaxOutputSeries(t *testing.T) {
	expr, err := syntax.ParseSampleExpr(`sum by (foo) (count_over_time({app="x"}[1m]))`)
	require.NoError(t, err)
	aggExpr := expr.(*syntax.VectorAggregationExpr)

	// a step vector with n distinct values of "foo" produces n aggregation groups.
	mkVec := func(n int) promql.Vector {
		vec := make(promql.Vector, 0, n)
		for i := range n {
			vec = append(vec, promql.Sample{T: 0, F: 1, Metric: labels.FromStrings("foo", fmt.Sprintf("v%d", i))})
		}
		return vec
	}

	newAgg := func(maxSeries int, vec promql.Vector) *VectorAggEvaluator {
		e := &VectorAggEvaluator{
			nextEvaluator:    &returnVectorEvaluator{vec: vec},
			expr:             aggExpr,
			exprSortedGroups: aggExpr.Grouping.Groups,
			buf:              make([]byte, 0, 1024),
			lb:               labels.NewBuilder(labels.EmptyLabels()),
		}
		e.SetMaxOutputSeries(maxSeries)
		return e
	}

	t.Run("fails fast when a single step exceeds the limit", func(t *testing.T) {
		e := newAgg(3, mkVec(5))
		ok, _, _ := e.Next()
		require.False(t, ok)
		require.Error(t, e.Error())
		require.True(t, errors.Is(e.Error(), logqlmodel.ErrLimit))
	})

	t.Run("passes when at the limit", func(t *testing.T) {
		e := newAgg(3, mkVec(3))
		ok, _, r := e.Next()
		require.True(t, ok)
		require.NoError(t, e.Error())
		require.Len(t, r.SampleVector(), 3)
	})

	t.Run("no limit when unset", func(t *testing.T) {
		e := newAgg(0, mkVec(5))
		ok, _, r := e.Next()
		require.True(t, ok)
		require.NoError(t, e.Error())
		require.Len(t, r.SampleVector(), 5)
	})
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

func (*emptyEvaluator) SetMaxOutputSeries(int) {}

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

func (*returnVectorEvaluator) SetMaxOutputSeries(int) {}

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
