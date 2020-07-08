package manager

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/cortexproject/cortex/pkg/querier/series"
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

type MemStore struct {
	mtx       sync.Mutex
	queryFunc rules.QueryFunc
	appenders map[string]*ForStateAppender
	metrics   *Metrics
	mgr       *rules.Manager

	cleanupInterval time.Duration
	done            chan struct{}
}

func NewMemStore(mgr *rules.Manager, cleanupInterval time.Duration, metrics *Metrics) *MemStore {
	s := &MemStore{
		mgr:       mgr,
		appenders: make(map[string]*ForStateAppender),
		metrics:   metrics,

		cleanupInterval: cleanupInterval,
		done:            make(chan struct{}),
	}
	go s.run()
	return s
}

func (m *MemStore) Stop() {
	// Need to nil all series & decrement gauges
	m.mtx.Lock()
	defer m.mtx.Unlock()

	select {
	// ensures Stop() is idempotent
	case <-m.done:
		return
	default:
		for ruleKey, app := range m.appenders {
			// Force cleanup of all samples older than time.Now (all of them).
			_ = app.CleanupOldSamples(0)
			delete(m.appenders, ruleKey)
		}
		close(m.done)
	}
}

// run periodically cleans up old series/samples to ensure memory consumption doesn't grow unbounded.
func (m *MemStore) run() {
	t := time.NewTicker(m.cleanupInterval)
	for {
		select {
		case <-m.done:
			t.Stop()
			return
		case <-t.C:
			m.mtx.Lock()
			holdDurs := make(map[string]time.Duration)
			for _, rule := range m.mgr.AlertingRules() {
				holdDurs[rule.Name()] = rule.HoldDuration()
			}

			for ruleKey, app := range m.appenders {
				dur, ok := holdDurs[ruleKey]

				// rule is no longer being tracked, remove it
				if !ok {
					_ = app.CleanupOldSamples(0)
					delete(m.appenders, ruleKey)
					continue
				}

				// trim older samples out of tracking bounds, doubled to buffer.
				if rem := app.CleanupOldSamples(2 * dur); rem == 0 {
					delete(m.appenders, ruleKey)
				}

			}

			m.mtx.Unlock()
		}
	}
}

type ForStateAppender struct {
	mtx     sync.Mutex
	metrics *Metrics
	data    map[uint64]*series.ConcreteSeries
}

func NewForStateAppender(metrics *Metrics) *ForStateAppender {
	return &ForStateAppender{
		data:    make(map[uint64]*series.ConcreteSeries),
		metrics: metrics,
	}
}

func (m *ForStateAppender) Add(ls labels.Labels, t int64, v float64) (uint64, error) {
	for _, l := range ls {
		if l.Name == labels.MetricName && l.Value != AlertForStateMetricName {
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
func (m *ForStateAppender) CleanupOldSamples(lookback time.Duration) (seriesRemaining int) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	for fp, s := range m.data {
		// release all older references that are no longer needed.
		priorLn := s.Len()
		s.TrimStart(time.Now().Add(-lookback))
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
func (m *ForStateAppender) Querier(ctx context.Context, mint, maxt int64) ForStateAppenderQuerier {
	return ForStateAppenderQuerier{
		mint:             mint,
		maxt:             maxt,
		ForStateAppender: m,
	}
}

// ForStateAppenderQuerier wraps a *ForStateAppender and implements storage.Querier
type ForStateAppenderQuerier struct {
	mint, maxt int64
	*ForStateAppender
}

// Select returns a set of series that matches the given label matchers.
func (q ForStateAppenderQuerier) Select(sortSeries bool, params *storage.SelectHints, matchers ...*labels.Matcher) storage.SeriesSet {
	// TODO: implement sorted selects (currently unused/ignored).
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
