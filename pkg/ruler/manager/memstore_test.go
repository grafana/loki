package manager

import (
	"context"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/rules"
	"github.com/stretchr/testify/require"
)

var (
	NilMetrics = NewMetrics(nil)
	NilLogger  = log.NewNopLogger()
)

func labelsToMatchers(ls labels.Labels) (res []*labels.Matcher) {
	for _, l := range ls {
		res = append(res, labels.MustNewMatcher(labels.MatchEqual, l.Name, l.Value))
	}
	return res
}

type MockRuleIter []*rules.AlertingRule

func (xs MockRuleIter) AlertingRules() []*rules.AlertingRule { return xs }

func testStore(queryFunc rules.QueryFunc, itv time.Duration) *MemStore {
	return NewMemStore("test", queryFunc, NilMetrics, itv, NilLogger)

}

func TestSelectRestores(t *testing.T) {
	ruleName := "testrule"
	ars := []*rules.AlertingRule{
		rules.NewAlertingRule(
			ruleName,
			&parser.StringLiteral{Val: "unused"},
			time.Minute,
			labels.FromMap(map[string]string{"foo": "bar"}),
			nil,
			nil,
			false,
			NilLogger,
		),
	}

	callCount := 0
	fn := rules.QueryFunc(func(ctx context.Context, qs string, t time.Time) (promql.Vector, error) {
		callCount++
		return promql.Vector{
			promql.Sample{
				Metric: labels.FromMap(map[string]string{
					labels.MetricName: "some_metric",
					"foo":             "bar",  // from the AlertingRule.labels spec
					"bazz":            "buzz", // an extra label
				}),
				Point: promql.Point{
					T: util.TimeToMillis(t),
					V: 1,
				},
			},
			promql.Sample{
				Metric: labels.FromMap(map[string]string{
					labels.MetricName: "some_metric",
					"foo":             "bar",  // from the AlertingRule.labels spec
					"bazz":            "bork", // an extra label (second variant)
				}),
				Point: promql.Point{
					T: util.TimeToMillis(t),
					V: 1,
				},
			},
		}, nil
	})

	store := testStore(fn, time.Minute)
	store.Start(MockRuleIter(ars))

	now := util.TimeToMillis(time.Now())

	q, err := store.Querier(context.Background(), 0, now)
	require.Nil(t, err)

	ls := ForStateMetric(labels.FromMap(map[string]string{
		"foo":  "bar",
		"bazz": "buzz",
	}), ruleName)

	// First call evaluates the rule at ts-ForDuration and populates the cache
	sset := q.Select(false, nil, labelsToMatchers(ls)...)

	require.Equal(t, true, sset.Next())
	require.Equal(t, ls, sset.At().Labels())
	iter := sset.At().Iterator()
	require.Equal(t, true, iter.Next())
	ts, v := iter.At()
	require.Equal(t, now, ts)
	require.Equal(t, float64(now), v)
	require.Equal(t, false, iter.Next())
	require.Equal(t, false, sset.Next())

	// Second call uses cache
	ls = ForStateMetric(labels.FromMap(map[string]string{
		"foo":  "bar",
		"bazz": "bork",
	}), ruleName)

	sset = q.Select(false, nil, labelsToMatchers(ls)...)
	require.Equal(t, true, sset.Next())
	require.Equal(t, ls, sset.At().Labels())
	iter = sset.At().Iterator()
	require.Equal(t, true, iter.Next())
	ts, v = iter.At()
	require.Equal(t, now, ts)
	require.Equal(t, float64(now), v)
	require.Equal(t, false, iter.Next())
	require.Equal(t, false, sset.Next())
	require.Equal(t, 1, callCount)

	// Third call uses cache & has no match
	ls = ForStateMetric(labels.FromMap(map[string]string{
		"foo":  "bar",
		"bazz": "unknown",
	}), ruleName)

	sset = q.Select(false, nil, labelsToMatchers(ls)...)
	require.Equal(t, false, sset.Next())
	require.Equal(t, 1, callCount)
}

func TestMemstoreStart(t *testing.T) {
	ruleName := "testrule"
	ars := []*rules.AlertingRule{
		rules.NewAlertingRule(
			ruleName,
			&parser.StringLiteral{Val: "unused"},
			time.Minute,
			labels.FromMap(map[string]string{"foo": "bar"}),
			nil,
			nil,
			false,
			NilLogger,
		),
	}

	fn := rules.QueryFunc(func(ctx context.Context, qs string, t time.Time) (promql.Vector, error) {
		return nil, nil
	})

	store := testStore(fn, time.Minute)

	store.Start(MockRuleIter(ars))
}

func TestMemStoreStopBeforeStart(t *testing.T) {
	store := testStore(nil, time.Minute)
	done := make(chan struct{})
	go func() {
		store.Stop()
		done <- struct{}{}
	}()
	select {
	case <-time.After(time.Millisecond):
		t.FailNow()
	case <-done:
	}
}

func TestMemstoreBlocks(t *testing.T) {
	ruleName := "testrule"
	ars := []*rules.AlertingRule{
		rules.NewAlertingRule(
			ruleName,
			&parser.StringLiteral{Val: "unused"},
			time.Minute,
			labels.FromMap(map[string]string{"foo": "bar"}),
			nil,
			nil,
			false,
			NilLogger,
		),
	}

	fn := rules.QueryFunc(func(ctx context.Context, qs string, t time.Time) (promql.Vector, error) {
		return nil, nil
	})

	store := testStore(fn, time.Minute)

	done := make(chan struct{})
	go func() {
		store.Querier(context.Background(), 0, 1)
		done <- struct{}{}
	}()

	select {
	case <-time.After(time.Millisecond):
	case <-done:
		t.FailNow()
	}

	store.Start(MockRuleIter(ars))
	select {
	case <-done:
	case <-time.After(time.Millisecond):
		t.FailNow()
	}

}
