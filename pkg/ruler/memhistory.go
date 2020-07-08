package ruler

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/cortexproject/cortex/pkg/querier/series"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/rules"
	"github.com/prometheus/prometheus/storage"
)

type Metrics struct {
	Series  prometheus.Gauge // in memory series
	Samples prometheus.Gauge // in memory samples
}

func NewMetrics(r prometheus.Registerer) *Metrics {
	return &Metrics{
		Series: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: "loki",
			Name:      "ruler_memory_series",
		}),
		Samples: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: "loki",
			Name:      "ruler_memory_samples",
		}),
	}
}

type MemHistory struct {
	mtx       sync.RWMutex
	userId    string
	opts      *rules.ManagerOptions
	appenders map[*rules.AlertingRule]*ForStateAppender
	metrics   *Metrics

	done            chan struct{}
	cleanupInterval time.Duration
}

func NewMemHistory(userId string, cleanupInterval time.Duration, opts *rules.ManagerOptions, metrics *Metrics) *MemHistory {
	hist := &MemHistory{
		userId:    userId,
		opts:      opts,
		appenders: make(map[*rules.AlertingRule]*ForStateAppender),
		metrics:   metrics,

		cleanupInterval: cleanupInterval,
		done:            make(chan struct{}),
	}
	go hist.run()
	return hist
}

func (m *MemHistory) Stop() {
	select {
	// ensures Stop() is idempotent
	case <-m.done:
		return
	default:
		close(m.done)
		return
	}
}

// run periodically cleans up old series/samples to ensure memory consumption doesn't grow unbounded.
func (m *MemHistory) run() {
	t := time.NewTicker(m.cleanupInterval)
	for {
		select {
		case <-m.done:
			t.Stop()
			return
		case <-t.C:
			m.mtx.Lock()
			for rule, app := range m.appenders {
				if rem := app.CleanupOldSamples(); rem == 0 {
					delete(m.appenders, rule)
				}

			}
			m.mtx.Unlock()
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

	app := NewForStateAppender(alertRule, m.metrics)
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
	start := time.Now()
	adjusted := ts.Add(-alertRule.Duration())

	level.Info(m.opts.Logger).Log(
		"msg", "restoring synthetic for state",
		"adjusted_ts", adjusted,
		"rule", alertRule.Name(),
		"query", alertRule.Query().String(),
		"rule_duration", alertRule.Duration(),
		"tenant", m.userId,
	)
	vec, err := m.opts.QueryFunc(m.opts.Context, alertRule.Query().String(), adjusted)
	m.opts.Metrics.IncrementEvaluations()
	if err != nil {
		alertRule.SetHealth(rules.HealthBad)
		alertRule.SetLastError(err)
		m.opts.Metrics.FailedEvaluate()
	}

	for _, smpl := range vec {
		forStateSample := alertRule.ForStateSample(
			&rules.Alert{
				Labels:   smpl.Metric,
				ActiveAt: ts,
				Value:    smpl.V,
			},
			util.TimeFromMillis(smpl.T),
			smpl.V,
		)

		if _, err := app.Add(forStateSample.Metric, forStateSample.T, forStateSample.V); err != nil {
			level.Error(m.opts.Logger).Log("msg", "error appending to MemHistory", "err", err)
			return
		}
	}
	level.Info(m.opts.Logger).Log(
		"msg", "resolved synthetic for_state",
		"rule", alertRule.Name(),
		"n_samples", len(vec),
		"tenant", m.userId,
	)
	m.opts.Metrics.EvalDuration(time.Since(start))

	// Now that we've evaluated the rule and written the results to our in memory appender,
	// delegate to the default implementation.
	rules.NewMetricsHistory(app, m.opts).RestoreForState(ts, alertRule)

}

type ForStateAppender struct {
	mtx     sync.Mutex
	metrics *Metrics
	rule    *rules.AlertingRule
	data    map[uint64]*series.ConcreteSeries
}

func NewForStateAppender(rule *rules.AlertingRule, metrics *Metrics) *ForStateAppender {
	return &ForStateAppender{
		rule:    rule,
		data:    make(map[uint64]*series.ConcreteSeries),
		metrics: metrics,
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
		priorLn := s.Len()
		s.Add(model.SamplePair{
			Timestamp: model.Time(t),
			Value:     model.SampleValue(v),
		})
		m.metrics.Samples.Add(float64(s.Len() - priorLn))

		return 0, nil
	}
	m.data[fp] = series.NewConcreteSeries(ls, []model.SamplePair{{Timestamp: model.Time(t), Value: model.SampleValue(v)}})
	m.metrics.Series.Inc()
	m.metrics.Samples.Inc()
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
		priorLn := s.Len()
		s.TrimStart(time.Now().Add(-m.rule.Duration() * oldEvaluationFactor))
		m.metrics.Samples.Add(float64(s.Len() - priorLn))
		if s.Len() == 0 {
			m.metrics.Series.Dec()
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
func (q ForStateAppenderQuerier) Select(sortSeries bool, params *storage.SelectHints, matchers ...*labels.Matcher) storage.SeriesSet {
	// TODO: implement sorted selects (currently unused).
	if sortSeries {
		return storage.NoopSeriesSet()

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

	return series.NewConcreteSeriesSet(filtered)
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
