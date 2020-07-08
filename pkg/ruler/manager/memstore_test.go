package manager

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/querier/series"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/rules"
	"github.com/prometheus/prometheus/storage"
	"github.com/stretchr/testify/require"
)

type NoopAppender struct{}

func (a NoopAppender) Appender() (storage.Appender, error)                     { return a, nil }
func (a NoopAppender) Add(l labels.Labels, t int64, v float64) (uint64, error) { return 0, nil }
func (a NoopAppender) AddFast(ref uint64, t int64, v float64) error {
	return errors.New("unimplemented")
}
func (a NoopAppender) Commit() error   { return nil }
func (a NoopAppender) Rollback() error { return nil }

func TestMemStoreStop(t *testing.T) {
	hist := NewMemStore(&rules.Manager{}, time.Millisecond, NewMetrics(nil))
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

func mustParseLabels(s string) labels.Labels {
	labels, err := parser.ParseMetric(s)
	if err != nil {
		panic(fmt.Sprintf("failed to parse %s", s))
	}

	return labels
}

func newRule(name, qry, ls string, forDur time.Duration) *rules.AlertingRule {
	return rules.NewAlertingRule(name, &parser.StringLiteral{Val: qry}, forDur, mustParseLabels(ls), nil, nil, false, log.NewNopLogger())
}

func TestForStateAppenderAdd(t *testing.T) {
	app := NewForStateAppender(NewMetrics(nil))
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
	app := NewForStateAppender(NewMetrics(nil))
	now := time.Now()

	// create ls series
	ls := mustParseLabels(`{foo="bar", bazz="buzz", __name__="ALERTS_FOR_STATE"}`)
	_, err := app.Add(ls, util.TimeToMillis(now.Add(-3*time.Minute)), 1)
	require.Nil(t, err)
	_, err = app.Add(ls, util.TimeToMillis(now.Add(-1*time.Minute)), 2)
	require.Nil(t, err)

	rem := app.CleanupOldSamples(time.Minute)
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
	app := NewForStateAppender(NewMetrics(nil))
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
	q := app.Querier(context.Background(), util.TimeToMillis(now.Add(-2*time.Minute)), util.TimeToMillis(now))

	set := q.Select(
		false,
		nil,
		labels.MustNewMatcher(labels.MatchEqual, labels.MetricName, AlertForStateMetricName),
	)
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
	q = app.Querier(context.Background(), util.TimeToMillis(now.Add(-time.Hour)), util.TimeToMillis(now.Add(time.Hour)))
	set2 := q.Select(
		false,
		&storage.SelectHints{
			Start: util.TimeToMillis(now.Add(-2 * time.Minute)),
			End:   util.TimeToMillis(now),
		},
		labels.MustNewMatcher(labels.MatchEqual, labels.MetricName, AlertForStateMetricName),
	)
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
}
