package ruler

import (
	"context"
	"errors"
	"time"

	"github.com/cortexproject/cortex/pkg/querier/series"
	"github.com/cortexproject/cortex/pkg/ruler/rules"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/storage"
)

type Metrics struct {
	groupMetrics *rules.Metrics
}

func NewMetrics(registerer prometheus.Registerer, groupMetrics *rules.Metrics) *Metrics {
	if groupMetrics == nil {
		groupMetrics = rules.NewGroupMetrics(registerer)
	}
	return &Metrics{
		groupMetrics: groupMetrics,
	}
}

type MemHistory struct {
	userId    string
	opts      *rules.ManagerOptions
	appenders map[*rules.AlertingRule]*ForStateAppender
	metrics   *Metrics
}

func NewMemHistory(userId string, opts *rules.ManagerOptions) *MemHistory {
	return &MemHistory{
		userId:    userId,
		opts:      opts,
		appenders: make(map[*rules.AlertingRule]*ForStateAppender),
		metrics:   NewMetrics(opts.Registerer, opts.Metrics),
	}
}

// Implement rules.Appendable
func (m *MemHistory) Appender(rule rules.Rule) (storage.Appender, error) {
	if rule == nil {
		return NoopAppender{}, nil
	}

	alertRule, ok := rule.(*rules.AlertingRule)
	if !ok {
		return nil, errors.New("unimplemented: MemHistory only accepts AlertingRules")
	}

	if app, ok := m.appenders[alertRule]; ok {
		return app, nil
	}

	app := NewForStateAppender(alertRule)
	m.appenders[alertRule] = app
	return app, nil
}

func (m *MemHistory) RestoreForState(ts time.Time, alertRule *rules.AlertingRule) {
	appender, err := m.Appender(alertRule)
	if err != nil {
		level.Error(m.opts.Logger).Log("msg", "Could not find an Appender for rule", "err", err)
		return
	}

	app := appender.(*ForStateAppender)

	// Here we artificially populate the evaluation at `now-forDuration`.
	// Note: We lose granularity here across restarts because we don't persist series. This is an approximation
	// of whether the alert condition was positive during this period. This means after restarts, we may lose up
	// to the ForDuration in alert granularity.
	// TODO: Do we want this to instead evaluate forDuration/interval times?
	vec, err := m.opts.QueryFunc(m.opts.Context, alertRule.Query().String(), ts.Add(-alertRule.Duration()))
	if err != nil {
		alertRule.SetHealth(rules.HealthBad)
		alertRule.SetLastError(err)
		m.metrics.groupMetrics.FailedEvaluate()
	}

	for _, smpl := range vec {
		if _, err := app.Add(smpl.Metric, smpl.T, smpl.V); err != nil {
			level.Error(m.opts.Logger).Log("msg", "error appending to MemHistory", "err", err)
			return
		}
	}

	// Now that we've evaluated the rule and written the results to our in memory appender,
	// delegate to the default implementation.
	rules.NewMetricsHistory(app, m.opts).RestoreForState(ts, alertRule)

}

type ForStateAppender struct {
	rule *rules.AlertingRule
	data map[uint64]*series.ConcreteSeries
}

func NewForStateAppender(rule *rules.AlertingRule) *ForStateAppender {
	return &ForStateAppender{
		rule: rule,
		data: make(map[uint64]*series.ConcreteSeries),
	}
}

func (m *ForStateAppender) Add(ls labels.Labels, t int64, v float64) (uint64, error) {
	for _, l := range ls {
		if l.Name == labels.MetricName && l.Value != rules.AlertForStateMetricName {
			// This is not an ALERTS_FOR_STATE metric, skip
			return 0, nil
		}
	}

	fp := ls.Hash()

	if s, ok := m.data[fp]; ok {
		s.Add(model.SamplePair{
			Timestamp: model.Time(t),
			Value:     model.SampleValue(v),
		})

		// release all older references that are no longer needed
		s.TrimStart(time.Now().Add(-m.rule.Duration()))
		return 0, nil
	}
	m.data[fp] = series.NewConcreteSeries(ls, []model.SamplePair{{Timestamp: model.Time(t), Value: model.SampleValue(v)}})
	return 0, nil

}

func (m *ForStateAppender) AddFast(ls labels.Labels, _ uint64, t int64, v float64) error {
	_, err := m.Add(ls, t, v)
	return err

}

func (m *ForStateAppender) Commit() error { return nil }

func (m *ForStateAppender) Rollback() error { return nil }

// implement storage.Queryable
func (m *ForStateAppender) Querier(ctx context.Context, mint, _ int64) (storage.Querier, error) {
	// These are never realisticallly bounded by maxt, so we omit it.
	return ForStateAppenderQuerier{
		mint:             mint,
		ForStateAppender: m,
	}, nil

}

// TimeFromMillis is a helper to turn milliseconds -> time.Time
func TimeFromMillis(ms int64) time.Time {
	return time.Unix(0, ms*int64(time.Millisecond/time.Nanosecond))
}

// ForStateAppenderQuerier wraps a **ForStateAppender and implements storage.Querier
type ForStateAppenderQuerier struct {
	mint int64
	*ForStateAppender
}

// Select returns a set of series that matches the given label matchers.
func (q ForStateAppenderQuerier) Select(params *storage.SelectParams, matchers ...*labels.Matcher) (storage.SeriesSet, storage.Warnings, error) {
	var filtered []storage.Series
outer:
	for _, s := range q.data {
		for _, matcher := range matchers {
			if !matcher.Matches(s.Labels().Get(matcher.Name)) {
				continue outer
			}

			iter := s.Iterator()

			seekTo := q.mint
			if seekTo < params.Start {
				seekTo = params.Start
			}
			if !iter.Seek(seekTo) {
				continue
			}

			var samples []model.SamplePair

			for iter.Next() {
				t, v := iter.At()
				if t > params.End {
					break
				}

				samples = append(samples, model.SamplePair{
					Timestamp: model.Time(t),
					Value:     model.SampleValue(v),
				})

			}

			if len(samples) != 0 {
				filtered = append(filtered, series.NewConcreteSeries(s.Labels(), samples))
			}
		}
	}

	return series.NewConcreteSeriesSet(filtered), nil, nil
}

// SelectSorted returns a sorted set of series that matches the given label matchers.
func (ForStateAppenderQuerier) SelectSorted(params *storage.SelectParams, matchers ...*labels.Matcher) (storage.SeriesSet, storage.Warnings, error) {
	return nil, nil, errors.New("unimplemented")
}

// LabelValues returns all potential values for a label name.
func (ForStateAppenderQuerier) LabelValues(name string) ([]string, storage.Warnings, error) {
	return nil, nil, errors.New("unimplemented")
}

// LabelNames returns all the unique label names present in the block in sorted order.
func (ForStateAppenderQuerier) LabelNames() ([]string, storage.Warnings, error) {
	return nil, nil, errors.New("unimplemented")
}

// Close releases the resources of the Querier.
func (ForStateAppenderQuerier) Close() error { return nil }
