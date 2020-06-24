package ruler

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/querier/series"
	"github.com/cortexproject/cortex/pkg/ruler/rules"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/storage"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"
)

func TestMemHistoryAppender(t *testing.T) {

	for _, tc := range []struct {
		desc     string
		err      bool
		expected storage.Appender
		rule     rules.Rule
	}{
		{
			desc:     "nil rule returns NoopAppender",
			err:      false,
			expected: NoopAppender{},
			rule:     nil,
		},
		{
			desc:     "recording rule errors",
			err:      true,
			expected: nil,
			rule:     &rules.RecordingRule{},
		},
		{
			desc:     "alerting rule returns ForStateAppender",
			err:      false,
			expected: NewForStateAppender(rules.NewAlertingRule("foo", nil, 0, nil, nil, nil, true, nil)),
			rule:     rules.NewAlertingRule("foo", nil, 0, nil, nil, nil, true, nil),
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			hist := NewMemHistory("abc", time.Minute, nil)

			app, err := hist.Appender(tc.rule)
			if tc.err {
				require.NotNil(t, err)
			}
			require.Equal(t, tc.expected, app)
		})
	}
}

// func TestMemHistoryRestoreForState(t *testing.T) {}

func TestMemHistoryRestoreForState(t *testing.T) {
	opts := &rules.ManagerOptions{
		QueryFunc: rules.QueryFunc(func(ctx context.Context, q string, t time.Time) (promql.Vector, error) {
			// always return the requested time
			return promql.Vector{promql.Sample{
				Point: promql.Point{
					T: util.TimeToMillis(t),
					V: float64(util.TimeToMillis(t)),
				},
				Metric: mustParseLabels(`{foo="bar", __name__="something"}`),
			}}, nil
		}),
		Context: user.InjectOrgID(context.Background(), "abc"),
		Logger:  log.NewNopLogger(),
		Metrics: rules.NewGroupMetrics(nil),
	}

	ts := time.Now().Round(time.Millisecond)
	rule := newRule("rule1", "query", `{foo="bar"}`, time.Minute)

	hist := NewMemHistory("abc", time.Minute, opts)
	hist.RestoreForState(ts, rule)

	app, err := hist.Appender(rule)
	require.Nil(t, err)
	casted := app.(*ForStateAppender)

	q, err := casted.Querier(context.Background(), 0, util.TimeToMillis(ts))
	require.Nil(t, err)
	set, _, err := q.Select(
		false,
		nil,
		labels.MustNewMatcher(labels.MatchEqual, labels.MetricName, rules.AlertForStateMetricName),
		labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"),
	)
	require.Nil(t, err)
	require.Equal(t, true, set.Next())
	s := set.At()
	require.Equal(t, `{__name__="ALERTS_FOR_STATE", alertname="rule1", foo="bar"}`, s.Labels().String())
	iter := s.Iterator()
	require.Equal(t, true, iter.Next())
	x, y := iter.At()
	adjusted := ts.Add(-rule.Duration()) // Adjusted for the forDuration lookback.
	require.Equal(t, util.TimeToMillis(adjusted), x)
	require.Equal(t, float64(util.TimeToMillis(adjusted)), y)
	require.Equal(t, false, iter.Next())

	// TODO: ensure extra labels are propagated?
}

func TestMemHistoryStop(t *testing.T) {
	hist := NewMemHistory("abc", time.Millisecond, nil)
	<-time.After(2 * time.Millisecond) // allow it to start ticking (not strictly required for this test)
	hist.Stop()
	// ensure idempotency
	hist.Stop()

	// ensure ticker is cleaned up
	select {
	case <-time.After(10 * time.Millisecond):
		t.Fatalf("done channel not closed")
	case <-hist.done:
	}
}

type stringer string

func (s stringer) String() string { return string(s) }

func mustParseLabels(s string) labels.Labels {
	labels, err := parser.ParseMetric(s)
	if err != nil {
		panic(fmt.Sprintf("failed to parse %s", s))
	}

	return labels
}

func newRule(name, qry, ls string, forDur time.Duration) *rules.AlertingRule {
	return rules.NewAlertingRule(name, stringer(qry), forDur, mustParseLabels(ls), nil, nil, false, log.NewNopLogger())
}

func TestForStateAppenderAdd(t *testing.T) {
	app := NewForStateAppender(newRule("foo", "query", `{foo="bar"}`, time.Minute))
	require.Equal(t, map[uint64]*series.ConcreteSeries{}, app.data)

	// create first series
	first := mustParseLabels(`{foo="bar", bazz="buzz", __name__="ALERTS_FOR_STATE"}`)
	_, err := app.Add(first, 1, 1)
	require.Nil(t, err)
	require.Equal(t, map[uint64]*series.ConcreteSeries{
		first.Hash(): series.NewConcreteSeries(
			first, []model.SamplePair{{Timestamp: model.Time(1), Value: model.SampleValue(1)}},
		),
	}, app.data)

	// create second series
	second := mustParseLabels(`{foo="bar", bazz="barf", __name__="ALERTS_FOR_STATE"}`)
	_, err = app.Add(second, 1, 1)
	require.Nil(t, err)

	require.Equal(t, map[uint64]*series.ConcreteSeries{
		first.Hash(): series.NewConcreteSeries(
			first, []model.SamplePair{{Timestamp: model.Time(1), Value: model.SampleValue(1)}},
		),
		second.Hash(): series.NewConcreteSeries(
			second, []model.SamplePair{{Timestamp: model.Time(1), Value: model.SampleValue(1)}},
		),
	}, app.data)

	// append first series
	_, err = app.Add(first, 3, 3)
	require.Nil(t, err)

	require.Equal(t, map[uint64]*series.ConcreteSeries{
		first.Hash(): series.NewConcreteSeries(
			first, []model.SamplePair{
				{Timestamp: model.Time(1), Value: model.SampleValue(1)},
				{Timestamp: model.Time(3), Value: model.SampleValue(3)},
			},
		),
		second.Hash(): series.NewConcreteSeries(
			second, []model.SamplePair{{Timestamp: model.Time(1), Value: model.SampleValue(1)}},
		),
	}, app.data)

	// insert new points at correct position
	_, err = app.Add(first, 2, 2)
	require.Nil(t, err)

	require.Equal(t, map[uint64]*series.ConcreteSeries{
		first.Hash(): series.NewConcreteSeries(
			first, []model.SamplePair{
				{Timestamp: model.Time(1), Value: model.SampleValue(1)},
				{Timestamp: model.Time(2), Value: model.SampleValue(2)},
				{Timestamp: model.Time(3), Value: model.SampleValue(3)},
			},
		),
		second.Hash(): series.NewConcreteSeries(
			second, []model.SamplePair{{Timestamp: model.Time(1), Value: model.SampleValue(1)}},
		),
	}, app.data)

	// ignore non ALERTS_FOR_STATE metrics
	_, err = app.Add(mustParseLabels(`{foo="bar", bazz="barf", __name__="test"}`), 1, 1)
	require.Nil(t, err)

	require.Equal(t, map[uint64]*series.ConcreteSeries{
		first.Hash(): series.NewConcreteSeries(
			first, []model.SamplePair{
				{Timestamp: model.Time(1), Value: model.SampleValue(1)},
				{Timestamp: model.Time(2), Value: model.SampleValue(2)},
				{Timestamp: model.Time(3), Value: model.SampleValue(3)},
			},
		),
		second.Hash(): series.NewConcreteSeries(
			second, []model.SamplePair{{Timestamp: model.Time(1), Value: model.SampleValue(1)}},
		),
	}, app.data)
}

func TestForStateAppenderCleanup(t *testing.T) {
	app := NewForStateAppender(newRule("foo", "query", `{foo="bar"}`, time.Minute))
	now := time.Now()

	// create ls series
	ls := mustParseLabels(`{foo="bar", bazz="buzz", __name__="ALERTS_FOR_STATE"}`)
	_, err := app.Add(ls, util.TimeToMillis(now.Add(-3*time.Minute)), 1)
	require.Nil(t, err)
	_, err = app.Add(ls, util.TimeToMillis(now.Add(-1*time.Minute)), 2)
	require.Nil(t, err)

	rem := app.CleanupOldSamples()
	require.Equal(t, 1, rem)

	require.Equal(t, map[uint64]*series.ConcreteSeries{
		ls.Hash(): series.NewConcreteSeries(
			ls, []model.SamplePair{
				{Timestamp: model.Time(util.TimeToMillis(now.Add(-1 * time.Minute))), Value: model.SampleValue(2)},
			},
		),
	}, app.data)

}

func TestForStateAppenderQuerier(t *testing.T) {
	app := NewForStateAppender(newRule("foo", "query", `{foo="bar"}`, time.Minute))
	now := time.Now()

	// create ls series
	ls := mustParseLabels(`{foo="bar", bazz="buzz", __name__="ALERTS_FOR_STATE"}`)
	_, err := app.Add(ls, util.TimeToMillis(now.Add(-3*time.Minute)), 1)
	require.Nil(t, err)
	_, err = app.Add(ls, util.TimeToMillis(now.Add(-1*time.Minute)), 2)
	require.Nil(t, err)
	_, err = app.Add(ls, util.TimeToMillis(now.Add(1*time.Minute)), 3)
	require.Nil(t, err)

	// never included due to bounds
	_, err = app.Add(mustParseLabels(`{foo="bar", bazz="blip", __name__="ALERTS_FOR_STATE"}`), util.TimeToMillis(now.Add(-2*time.Hour)), 3)
	require.Nil(t, err)

	// should succeed with nil selecthints
	q, err := app.Querier(context.Background(), util.TimeToMillis(now.Add(-2*time.Minute)), util.TimeToMillis(now))
	require.Nil(t, err)

	set, _, err := q.Select(
		false,
		nil,
		labels.MustNewMatcher(labels.MatchEqual, labels.MetricName, rules.AlertForStateMetricName),
	)
	require.Nil(t, err)
	require.Equal(
		t,
		series.NewConcreteSeriesSet(
			[]storage.Series{
				series.NewConcreteSeries(ls, []model.SamplePair{
					{Timestamp: model.Time(util.TimeToMillis(now.Add(-1 * time.Minute))), Value: model.SampleValue(2)},
				}),
			},
		),
		set,
	)

	// // should be able to minimize selection window via hints
	q, err = app.Querier(context.Background(), util.TimeToMillis(now.Add(-time.Hour)), util.TimeToMillis(now.Add(time.Hour)))
	require.Nil(t, err)
	set2, _, err := q.Select(
		false,
		&storage.SelectHints{
			Start: util.TimeToMillis(now.Add(-2 * time.Minute)),
			End:   util.TimeToMillis(now),
		},
		labels.MustNewMatcher(labels.MatchEqual, labels.MetricName, rules.AlertForStateMetricName),
	)
	require.Nil(t, err)
	require.Equal(
		t,
		series.NewConcreteSeriesSet(
			[]storage.Series{
				series.NewConcreteSeries(ls, []model.SamplePair{
					{Timestamp: model.Time(util.TimeToMillis(now.Add(-1 * time.Minute))), Value: model.SampleValue(2)},
				}),
			},
		),
		set2,
	)

	// requiring sorted results should err (unsupported)
	_, _, err = q.Select(
		true,
		nil,
		labels.MustNewMatcher(labels.MatchEqual, labels.MetricName, rules.AlertForStateMetricName),
	)
	require.NotNil(t, err)

}
