package logql

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	promql_parser "github.com/prometheus/prometheus/promql/parser"
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
	evaluator := NewDefaultEvaluator(querier, 0, 0, NoLimits)

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
	stepEvaluator, err := evaluator.NewStepEvaluator(context.Background(), evaluator, sampleExpr, sampleParams, true)
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
			ev:   &BinOpStepEvaluator{rse: &emptyEvaluator{}, lse: &emptyEvaluator{}},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			ok, _, _ := tc.ev.Next()
			require.False(t, ok)
		})
	}

}

func TestBinOpStepEvaluator_Next(t *testing.T) {
	t.Run("returns false and records the error when duplicate right-hand labels make the match many-to-many", func(t *testing.T) {
		rse := newReturnVectorEvaluator([]float64{1, 2})
		lse := newReturnVectorEvaluator([]float64{1})

		ev := &BinOpStepEvaluator{
			rse: rse,
			lse: lse,
			expr: &syntax.BinOpExpr{
				Op:   syntax.OpTypeAdd,
				Opts: &syntax.BinOpOptions{VectorMatching: &syntax.VectorMatching{}},
			},
		}

		ok, _, _ := ev.Next()
		require.False(t, ok)
		require.Error(t, ev.Error())
	})

	t.Run("a second call after an error stays exhausted and does not call rse or lse again", func(t *testing.T) {
		rseCalls, lseCalls := 0, 0
		rse := &fakeEvaluator{
			ok: true,
			result: SampleVector{
				{Metric: labels.FromStrings("foo", "bar"), F: 1},
				{Metric: labels.FromStrings("foo", "bar"), F: 2},
			},
			onNext: func() error { rseCalls++; return nil },
		}
		lse := &fakeEvaluator{
			ok:     true,
			result: SampleVector{{Metric: labels.FromStrings("foo", "bar"), F: 1}},
			onNext: func() error { lseCalls++; return nil },
		}

		ev := &BinOpStepEvaluator{
			rse: rse,
			lse: lse,
			expr: &syntax.BinOpExpr{
				Op:   syntax.OpTypeAdd,
				Opts: &syntax.BinOpOptions{VectorMatching: &syntax.VectorMatching{}},
			},
		}

		ok, _, _ := ev.Next()
		require.False(t, ok)
		require.Equal(t, 1, rseCalls)
		require.Equal(t, 1, lseCalls)

		ok, _, _ = ev.Next()
		require.False(t, ok)
		require.Equal(t, 1, rseCalls)
		require.Equal(t, 1, lseCalls)
		require.Error(t, ev.Error())
	})

	t.Run("surfaces an error that lives only in a child evaluator and skips the other child", func(t *testing.T) {
		lseCalls := 0
		rseErr := errors.New("rse error present before Next")
		rse := &fakeEvaluator{err: rseErr}
		lse := &fakeEvaluator{
			ok:     true,
			result: SampleVector{{Metric: labels.FromStrings("foo", "bar"), F: 1}},
			onNext: func() error { lseCalls++; return nil },
		}

		ev := &BinOpStepEvaluator{
			rse: rse,
			lse: lse,
			expr: &syntax.BinOpExpr{
				Op:   syntax.OpTypeAdd,
				Opts: &syntax.BinOpOptions{VectorMatching: &syntax.VectorMatching{}},
			},
		}

		ok, _, _ := ev.Next()
		require.False(t, ok)
		require.Equal(t, 0, lseCalls)
		require.ErrorIs(t, ev.Error(), rseErr)

		ok, _, _ = ev.Next()
		require.False(t, ok)
		require.Equal(t, 0, lseCalls)
		require.ErrorIs(t, ev.Error(), rseErr)
	})

	t.Run("does not call rse when lse already carries a known error", func(t *testing.T) {
		rseCalls := 0
		lseErr := errors.New("lse error present before Next")
		rse := &fakeEvaluator{
			ok:     true,
			result: SampleVector{{Metric: labels.FromStrings("foo", "bar"), F: 1}},
			onNext: func() error { rseCalls++; return nil },
		}
		lse := &fakeEvaluator{err: lseErr}

		ev := &BinOpStepEvaluator{
			rse: rse,
			lse: lse,
			expr: &syntax.BinOpExpr{
				Op:   syntax.OpTypeAdd,
				Opts: &syntax.BinOpOptions{VectorMatching: &syntax.VectorMatching{}},
			},
		}

		ok, _, _ := ev.Next()
		require.False(t, ok)
		require.Equal(t, 0, rseCalls)
		require.ErrorIs(t, ev.Error(), lseErr)

		ok, _, _ = ev.Next()
		require.False(t, ok)
		require.Equal(t, 0, rseCalls)
		require.ErrorIs(t, ev.Error(), lseErr)
	})

	t.Run("a second call after rse reveals an error mid-step stays exhausted and does not call either child again", func(t *testing.T) {
		rseCalls, lseCalls := 0, 0
		rseErr := errors.New("rse failed mid-iteration")
		rse := &fakeEvaluator{
			ok:     true,
			result: SampleVector{{Metric: labels.FromStrings("foo", "bar"), F: 1}},
			onNext: func() error { rseCalls++; return rseErr },
		}
		lse := &fakeEvaluator{
			ok:     true,
			result: SampleVector{{Metric: labels.FromStrings("foo", "bar"), F: 1}},
			onNext: func() error { lseCalls++; return nil },
		}

		ev := &BinOpStepEvaluator{
			rse: rse,
			lse: lse,
			expr: &syntax.BinOpExpr{
				Op:   syntax.OpTypeAdd,
				Opts: &syntax.BinOpOptions{VectorMatching: &syntax.VectorMatching{}},
			},
		}

		ok, _, _ := ev.Next()
		require.False(t, ok)
		require.Equal(t, 1, rseCalls)
		require.Equal(t, 0, lseCalls)

		ok, _, _ = ev.Next()
		require.False(t, ok)
		require.Equal(t, 1, rseCalls)
		require.Equal(t, 0, lseCalls)
		require.ErrorIs(t, ev.Error(), rseErr)
	})

	t.Run("a second call after lse reveals an error mid-step stays exhausted and does not call either child again", func(t *testing.T) {
		rseCalls, lseCalls := 0, 0
		lseErr := errors.New("lse failed mid-iteration")
		rse := &fakeEvaluator{
			ok:     true,
			result: SampleVector{{Metric: labels.FromStrings("foo", "bar"), F: 1}},
			onNext: func() error { rseCalls++; return nil },
		}
		lse := &fakeEvaluator{
			ok:     true,
			result: SampleVector{{Metric: labels.FromStrings("foo", "bar"), F: 1}},
			onNext: func() error { lseCalls++; return lseErr },
		}

		ev := &BinOpStepEvaluator{
			rse: rse,
			lse: lse,
			expr: &syntax.BinOpExpr{
				Op:   syntax.OpTypeAdd,
				Opts: &syntax.BinOpOptions{VectorMatching: &syntax.VectorMatching{}},
			},
		}

		ok, _, _ := ev.Next()
		require.False(t, ok)
		require.Equal(t, 1, rseCalls)
		require.Equal(t, 1, lseCalls)

		ok, _, _ = ev.Next()
		require.False(t, ok)
		require.Equal(t, 1, rseCalls)
		require.Equal(t, 1, lseCalls)
		require.ErrorIs(t, ev.Error(), lseErr)
	})
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

func TestNewTimestampFirstRangeAggEvaluator(t *testing.T) {
	newIterator := func() (iter.PeekingSampleIterator, *bool) {
		var closed bool
		it := iter.NewPeekingSampleIterator(iter.SampleIteratorWithClose(iter.NoopSampleIterator, func() error {
			closed = true
			return nil
		}))
		return it, &closed
	}

	q := LiteralParams{
		start: time.Unix(0, 0),
		end:   time.Unix(60, 0),
		step:  15 * time.Second,
	}

	t.Run("closes the sample iterator when the operation is unsupported", func(t *testing.T) {
		it, closed := newIterator()
		expr := &syntax.RangeAggregationExpr{
			Left:      &syntax.LogRangeExpr{Interval: time.Minute},
			Operation: "not-a-real-operation",
		}

		_, err := newTimestampFirstRangeAggEvaluator(context.Background(), it, expr, q, 0, false)
		require.Error(t, err)
		require.True(t, *closed)
	})

	t.Run("leaves the sample iterator open when the evaluator is built successfully", func(t *testing.T) {
		it, closed := newIterator()
		expr := &syntax.RangeAggregationExpr{
			Left:      &syntax.LogRangeExpr{Interval: time.Minute},
			Operation: syntax.OpRangeTypeCount,
		}

		ev, err := newTimestampFirstRangeAggEvaluator(context.Background(), it, expr, q, 0, false)
		require.NoError(t, err)
		require.False(t, *closed)

		require.NoError(t, ev.Close())
		require.True(t, *closed)
	})
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

	nextEvFactory := SampleEvaluatorFunc(func(_ context.Context, _ SampleEvaluatorFactory, _ syntax.SampleExpr, _ Params, _ bool) (StepEvaluator, error) {
		return &emptyEvaluator{}, nil
	})

	_, err := newVectorAggEvaluator(context.Background(), nextEvFactory, expr, nil, 0)
	require.NoError(t, err)
	require.Equal(t, []string{"b", "a"}, expr.Grouping.Groups)
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

// tenantStreamFirstLimits enables stream-first execution for the listed tenants only.
type tenantStreamFirstLimits struct {
	fakeLimits
	enabled map[string]bool
}

func (l tenantStreamFirstLimits) StreamFirstExecutionEnabled(userID string) bool {
	return l.enabled[userID]
}

// seriesQuerier returns the given series one after the other, whatever their order. It records
// the last sample request and counts the samples read.
type seriesQuerier struct {
	series       []logproto.Series
	sampleParams SelectSampleParams
	samplesRead  int
}

func (q *seriesQuerier) SelectLogs(context.Context, SelectLogParams) (iter.EntryIterator, error) {
	return iter.NoopEntryIterator, nil
}

func (q *seriesQuerier) SelectSamples(_ context.Context, params SelectSampleParams) (iter.SampleIterator, error) {
	q.sampleParams = params
	its := make([]iter.SampleIterator, 0, len(q.series))
	for _, s := range q.series {
		its = append(its, iter.NewSeriesIterator(s))
	}
	return &countingSampleIterator{SampleIterator: iter.NewChainedSampleIterator(its), count: &q.samplesRead}, nil
}

// countingSampleIterator counts the samples its caller reads.
type countingSampleIterator struct {
	iter.SampleIterator
	count *int
}

func (it *countingSampleIterator) Next() bool {
	if !it.SampleIterator.Next() {
		return false
	}
	*it.count++
	return true
}

func TestSampleOrderFor(t *testing.T) {
	limits := tenantStreamFirstLimits{enabled: map[string]bool{"a": true, "b": true}}

	fullExpr := func(query string) syntax.SampleExpr {
		return syntax.MustParseExpr(query).(syntax.SampleExpr)
	}

	tenantContext := func(tenantID string) context.Context {
		return user.InjectOrgID(context.Background(), tenantID)
	}

	count := fullExpr(`sum by (app) (count_over_time({app="foo"}[1m]))`)

	t.Run("returns stream-first for a root sum of count_over_time when the tenant enables it", func(t *testing.T) {
		got := sampleOrderFor(tenantContext("a"), count, limits, true)
		require.Equal(t, logproto.SAMPLE_ORDER_BY_STREAM, got)
	})

	t.Run("returns timestamp-first when the tenant does not enable it", func(t *testing.T) {
		got := sampleOrderFor(tenantContext("other"), count, limits, true)
		require.Equal(t, logproto.SAMPLE_ORDER_BY_TIMESTAMP, got)
	})

	t.Run("returns stream-first for a multi-tenant query when every tenant enables it", func(t *testing.T) {
		got := sampleOrderFor(tenantContext("a|b"), count, limits, true)
		require.Equal(t, logproto.SAMPLE_ORDER_BY_STREAM, got)
	})

	t.Run("returns timestamp-first for a multi-tenant query when one tenant does not enable it", func(t *testing.T) {
		got := sampleOrderFor(tenantContext("a|other"), count, limits, true)
		require.Equal(t, logproto.SAMPLE_ORDER_BY_TIMESTAMP, got)
	})

	t.Run("returns timestamp-first for an expression that is not the root", func(t *testing.T) {
		got := sampleOrderFor(tenantContext("a"), count, limits, false)
		require.Equal(t, logproto.SAMPLE_ORDER_BY_TIMESTAMP, got)
	})

	t.Run("returns timestamp-first for a range aggregation other than count_over_time", func(t *testing.T) {
		got := sampleOrderFor(tenantContext("a"), fullExpr(`sum(rate({app="foo"}[1m]))`), limits, true)
		require.Equal(t, logproto.SAMPLE_ORDER_BY_TIMESTAMP, got)
	})

	t.Run("returns timestamp-first for a bare range aggregation without a sum", func(t *testing.T) {
		got := sampleOrderFor(tenantContext("a"), fullExpr(`count_over_time({app="foo"}[1m])`), limits, true)
		require.Equal(t, logproto.SAMPLE_ORDER_BY_TIMESTAMP, got)
	})

	t.Run("returns timestamp-first when the context has no tenant", func(t *testing.T) {
		got := sampleOrderFor(context.Background(), count, limits, true)
		require.Equal(t, logproto.SAMPLE_ORDER_BY_TIMESTAMP, got)
	})
}

func TestDefaultEvaluator_NewStepEvaluator(t *testing.T) {
	start := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	ctx := user.InjectOrgID(context.Background(), "a")

	limitsWithMaxSeries := func(maxSeries int) Limits {
		return tenantStreamFirstLimits{fakeLimits: fakeLimits{maxSeries: maxSeries, timeout: time.Minute}, enabled: map[string]bool{"a": true}}
	}
	execute := func(t *testing.T, limits Limits, querier Querier, query string, step time.Duration) (promql_parser.Value, error) {
		t.Helper()
		params, err := NewLiteralParams(query, start, start.Add(2*time.Minute), step, 0, logproto.FORWARD, 0, nil, nil)
		require.NoError(t, err)
		res, err := NewEngine(EngineOpts{}, querier, limits, nil).Query(params).Exec(ctx)
		return res.Data, err
	}
	requestedOrder := func(t *testing.T, query string) logproto.SampleOrder {
		t.Helper()
		querier := &seriesQuerier{}
		_, err := execute(t, limitsWithMaxSeries(100), querier, query, time.Minute)
		require.NoError(t, err)
		require.NotNil(t, querier.sampleParams.SampleQueryRequest)
		return querier.sampleParams.Order
	}

	// streamFirstSeries returns two series per app, both with the labels {app=...}. Each series is
	// ordered by time, and the series follow one another, as a stream-first source returns them.
	streamFirstSeries := func(apps ...string) []logproto.Series {
		var out []logproto.Series
		for _, app := range apps {
			for range 2 {
				out = append(out, logproto.Series{
					Labels: fmt.Sprintf(`{app=%q}`, app),
					Samples: []logproto.Sample{
						{Timestamp: start.Add(10 * time.Second).UnixNano(), Value: 1},
						{Timestamp: start.Add(70 * time.Second).UnixNano(), Value: 1},
						{Timestamp: start.Add(110 * time.Second).UnixNano(), Value: 1},
					},
				})
			}
		}
		return out
	}

	t.Run("requests stream-first order for a root sum of count_over_time", func(t *testing.T) {
		require.Equal(t, logproto.SAMPLE_ORDER_BY_STREAM, requestedOrder(t, `sum by (app) (count_over_time({app="foo"}[1m]))`))
	})

	t.Run("requests stream-first order for a tenant without a series limit", func(t *testing.T) {
		querier := &seriesQuerier{}
		_, err := execute(t, limitsWithMaxSeries(0), querier, `sum by (app) (count_over_time({app="foo"}[1m]))`, time.Minute)
		require.NoError(t, err)
		require.Equal(t, logproto.SAMPLE_ORDER_BY_STREAM, querier.sampleParams.Order)
	})

	t.Run("requests timestamp-first order for count_over_time without a sum", func(t *testing.T) {
		require.Equal(t, logproto.SAMPLE_ORDER_BY_TIMESTAMP, requestedOrder(t, `count_over_time({app="foo"}[1m])`))
	})

	t.Run("requests timestamp-first order for sum of rate", func(t *testing.T) {
		require.Equal(t, logproto.SAMPLE_ORDER_BY_TIMESTAMP, requestedOrder(t, `sum(rate({app="foo"}[1m]))`))
	})

	t.Run("requests timestamp-first order for a sum of count_over_time nested in another aggregation", func(t *testing.T) {
		require.Equal(t, logproto.SAMPLE_ORDER_BY_TIMESTAMP, requestedOrder(t, `topk(1, sum by (app) (count_over_time({app="foo"}[1m])))`))
	})

	t.Run("counts every sample of streams returned one after the other", func(t *testing.T) {
		querier := &seriesQuerier{series: streamFirstSeries("foo")}
		got, err := execute(t, limitsWithMaxSeries(100), querier, `sum by (app) (count_over_time({app="foo"}[1m]))`, time.Minute)
		require.NoError(t, err)

		want := promql.Matrix{{
			Metric: labels.FromStrings("app", "foo"),
			Floats: []promql.FPoint{
				{T: start.Add(time.Minute).UnixMilli(), F: 2},
				{T: start.Add(2 * time.Minute).UnixMilli(), F: 4},
			},
		}}
		require.Equal(t, want, got)
	})

	t.Run("succeeds for a multi-tenant query whose output fits the series limit while its per-tenant series do not", func(t *testing.T) {
		var series []logproto.Series
		for _, tenantID := range []string{"a", "b"} {
			series = append(series, logproto.Series{
				Labels:  fmt.Sprintf(`{__tenant_id__=%q, app="foo"}`, tenantID),
				Samples: []logproto.Sample{{Timestamp: start.Add(10 * time.Second).UnixNano(), Value: 1}},
			})
		}
		limits := tenantStreamFirstLimits{fakeLimits: fakeLimits{maxSeries: 1, timeout: time.Minute}, enabled: map[string]bool{"a": true, "b": true}}
		querier := &seriesQuerier{series: series}
		params, err := NewLiteralParams(`sum by (app) (count_over_time({app="foo"}[1m]))`, start, start.Add(2*time.Minute), time.Minute, 0, logproto.FORWARD, 0, nil, nil)
		require.NoError(t, err)

		res, err := NewEngine(EngineOpts{}, querier, limits, nil).Query(params).Exec(user.InjectOrgID(context.Background(), "a|b"))
		require.NoError(t, err)
		require.Equal(t, logproto.SAMPLE_ORDER_BY_STREAM, querier.sampleParams.Order)
		require.Equal(t, promql.Matrix{{
			Metric: labels.FromStrings("app", "foo"),
			Floats: []promql.FPoint{{T: start.Add(time.Minute).UnixMilli(), F: 2}},
		}}, res.Data)
	})

	t.Run("fails at the first series over the tenant's series limit without reading the remaining samples", func(t *testing.T) {
		querier := &seriesQuerier{series: streamFirstSeries("foo", "bar")}
		_, err := execute(t, limitsWithMaxSeries(1), querier, `sum by (app) (count_over_time({app=~"foo|bar"}[1m]))`, time.Minute)
		require.ErrorIs(t, err, logqlmodel.ErrLimit)

		totalSamples := 0
		for _, s := range querier.series {
			totalSamples += len(s.Samples)
		}
		require.Less(t, querier.samplesRead, totalSamples)
	})
}
