package ruler

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/rulefmt"

	"github.com/go-kit/log"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/rules"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/util"
)

const ruleName = "testrule"

func labelsToMatchers(ls labels.Labels) (res []*labels.Matcher) {
	for _, l := range ls {
		res = append(res, labels.MustNewMatcher(labels.MatchEqual, l.Name, l.Value))
	}
	return res
}

type MockRuleIter []rulefmt.Rule

func (xs MockRuleIter) AlertingRules() []rulefmt.Rule { return xs }

func testStore(queryFunc rules.QueryFunc) *MemStore {
	return NewMemStore("test", queryFunc, newMemstoreMetrics(nil), time.Minute, log.NewNopLogger())

}

func TestSelectRestores(t *testing.T) {
	forDuration := time.Minute
	ars := []rulefmt.Rule{
		{
			Alert:  ruleName,
			Expr:   "unused",
			For:    model.Duration(forDuration),
			Labels: map[string]string{"foo": "bar"},
		},
	}

	callCount := 0
	fn := rules.QueryFunc(func(_ context.Context, _ string, t time.Time) (promql.Vector, error) {
		callCount++
		return promql.Vector{
			promql.Sample{
				Metric: labels.FromMap(map[string]string{
					labels.MetricName: "some_metric",
					"foo":             "bar",  // from the AlertingRule.labels spec
					"bazz":            "buzz", // an extra label
				}),
				T: util.TimeToMillis(t),
				F: 1,
			},
			promql.Sample{
				Metric: labels.FromMap(map[string]string{
					labels.MetricName: "some_metric",
					"foo":             "bar",  // from the AlertingRule.labels spec
					"bazz":            "bork", // an extra label (second variant)
				}),
				T: util.TimeToMillis(t),
				F: 1,
			},
		}, nil
	})

	store := testStore(fn)
	store.Start(MockRuleIter(ars))

	tNow := time.Now()
	now := util.TimeToMillis(tNow)

	q, err := store.Querier(0, now)
	require.Nil(t, err)

	ls := ForStateMetric(labels.FromMap(map[string]string{
		"foo":  "bar",
		"bazz": "buzz",
	}), ruleName)

	// First call evaluates the rule at ts-ForDuration and populates the cache
	sset := q.Select(context.Background(), false, nil, labelsToMatchers(ls)...)

	require.Equal(t, true, sset.Next())
	require.Equal(t, ls, sset.At().Labels())
	iter := sset.At().Iterator(nil)
	require.Equal(t, chunkenc.ValFloat, iter.Next())
	ts, v := iter.At()
	require.Equal(t, now, ts)
	require.Equal(t, float64(tNow.Add(-forDuration).Unix()), v)
	require.Equal(t, chunkenc.ValNone, iter.Next())
	require.Equal(t, false, sset.Next())

	// Second call uses cache
	ls = ForStateMetric(labels.FromMap(map[string]string{
		"foo":  "bar",
		"bazz": "bork",
	}), ruleName)

	sset = q.Select(context.Background(), false, nil, labelsToMatchers(ls)...)
	require.Equal(t, true, sset.Next())
	require.Equal(t, ls, sset.At().Labels())
	iter = sset.At().Iterator(iter)
	require.Equal(t, chunkenc.ValFloat, iter.Next())
	ts, v = iter.At()
	require.Equal(t, now, ts)
	require.Equal(t, float64(tNow.Add(-forDuration).Unix()), v)
	require.Equal(t, chunkenc.ValNone, iter.Next())
	require.Equal(t, false, sset.Next())
	require.Equal(t, 1, callCount)

	// Third call uses cache & has no match
	ls = ForStateMetric(labels.FromMap(map[string]string{
		"foo":  "bar",
		"bazz": "unknown",
	}), ruleName)

	sset = q.Select(context.Background(), false, nil, labelsToMatchers(ls)...)
	require.Equal(t, false, sset.Next())
	require.Equal(t, 1, callCount)
}

func TestMemstoreStart(_ *testing.T) {
	ars := []rulefmt.Rule{
		{
			Alert:  ruleName,
			Expr:   "unused",
			For:    model.Duration(time.Minute),
			Labels: map[string]string{"foo": "bar"},
		},
	}

	fn := rules.QueryFunc(func(_ context.Context, _ string, _ time.Time) (promql.Vector, error) {
		return nil, nil
	})

	store := testStore(fn)

	store.Start(MockRuleIter(ars))
}

func TestMemStoreStopBeforeStart(t *testing.T) {
	store := testStore(nil)
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
	ars := []rulefmt.Rule{
		{
			Alert:  ruleName,
			Expr:   "unused",
			For:    model.Duration(time.Minute),
			Labels: map[string]string{"foo": "bar"},
		},
	}

	fn := rules.QueryFunc(func(_ context.Context, _ string, _ time.Time) (promql.Vector, error) {
		return nil, nil
	})

	store := testStore(fn)

	done := make(chan struct{})
	go func() {
		_, _ = store.Querier(0, 1)
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
