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
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/storage"
	"github.com/stretchr/testify/require"
)

func TestNewMemHistory(t *testing.T) {
	userID := "abc"
	expected := &MemHistory{
		userId:    userID,
		appenders: make(map[*rules.AlertingRule]*ForStateAppender),

		cleanupInterval: 5 * time.Minute,
	}
	require.Equal(t, expected, NewMemHistory(userID, nil))

}

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
			hist := NewMemHistory("abc", nil)

			app, err := hist.Appender(tc.rule)
			if tc.err {
				require.NotNil(t, err)
			}
			require.Equal(t, tc.expected, app)
		})
	}
}

func TestMemHistoryRestoreForState(t *testing.T) {}

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
