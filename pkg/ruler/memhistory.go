package ruler

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/cortexproject/cortex/pkg/querier/series"
	"github.com/cortexproject/cortex/pkg/ruler/rules"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/storage"
)

type MemHistory struct {
	mtx       sync.RWMutex
	userId    string
	opts      *rules.ManagerOptions
	appenders map[*rules.AlertingRule]*ForStateAppender

	done            chan struct{}
	cleanupInterval time.Duration
}

func NewMemHistory(userId string, opts *rules.ManagerOptions) *MemHistory {
	hist := &MemHistory{
		userId:    userId,
		opts:      opts,
		appenders: make(map[*rules.AlertingRule]*ForStateAppender),

		cleanupInterval: 5 * time.Minute, // TODO: make configurable
	}
	go hist.run()
	return hist
}

func (m *MemHistory) run() {
	for range time.NewTicker(m.cleanupInterval).C {
		m.mtx.Lock()
		defer m.mtx.Unlock()
		for rule, app := range m.appenders {
			if rem := app.CleanupOldSamples(); rem == 0 {
				delete(m.appenders, rule)
			}

		}
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

	m.mtx.Lock()
	defer m.mtx.Unlock()

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
		m.opts.Metrics.FailedEvaluate()
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
	mtx  sync.Mutex
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

	m.mtx.Lock()
	defer m.mtx.Unlock()

	fp := ls.Hash()

	if s, ok := m.data[fp]; ok {
		s.Add(model.SamplePair{
			Timestamp: model.Time(t),
			Value:     model.SampleValue(v),
		})

		return 0, nil
	}
	m.data[fp] = series.NewConcreteSeries(ls, []model.SamplePair{{Timestamp: model.Time(t), Value: model.SampleValue(v)}})
	return 0, nil

}

// CleanupOldSamples removes samples that are outside of the rule's `For` duration.
func (m *ForStateAppender) CleanupOldSamples() (seriesRemaining int) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	// TODO: make this factor configurable?
	// Basically, buffer samples in memory up to ruleDuration * oldEvaluationFactor.
	oldEvaluationFactor := time.Duration(2)

	for fp, s := range m.data {
		// release all older references that are no longer needed.
		s.TrimStart(time.Now().Add(-m.rule.Duration() * oldEvaluationFactor))
		if s.Len() == 0 {
			delete(m.data, fp)
		}
	}

	return len(m.data)

}

func (m *ForStateAppender) AddFast(ref uint64, t int64, v float64) error {
	return errors.New("unimplemented")
}

func (m *ForStateAppender) Commit() error { return nil }

func (m *ForStateAppender) Rollback() error { return nil }

// implement storage.Queryable
func (m *ForStateAppender) Querier(ctx context.Context, mint, maxt int64) (storage.Querier, error) {
	return ForStateAppenderQuerier{
		mint:             mint,
		maxt:             maxt,
		ForStateAppender: m,
	}, nil

}

// ForStateAppenderQuerier wraps a **ForStateAppender and implements storage.Querier
type ForStateAppenderQuerier struct {
	mint, maxt int64
	*ForStateAppender
}

// Select returns a set of series that matches the given label matchers.
func (q ForStateAppenderQuerier) Select(sortSeries bool, params *storage.SelectHints, matchers ...*labels.Matcher) (storage.SeriesSet, storage.Warnings, error) {
	if sortSeries {
		return nil, nil, errors.New("ForStateAppenderQuerier does not support sorted selects")

	}
	q.mtx.Lock()
	defer q.mtx.Unlock()

	seekTo := q.mint
	if params != nil && seekTo < params.Start {
		seekTo = params.Start
	}

	maxt := q.maxt
	if params != nil && params.End < maxt {
		maxt = params.End
	}

	var filtered []storage.Series
outer:
	for _, s := range q.data {
		for _, matcher := range matchers {
			if !matcher.Matches(s.Labels().Get(matcher.Name)) {
				continue outer
			}

			iter := s.Iterator()
			var samples []model.SamplePair
			for ok := iter.Seek(seekTo); ok; ok = iter.Next() {
				t, v := iter.At()
				if t > maxt {
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
