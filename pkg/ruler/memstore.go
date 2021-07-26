package ruler

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/cortexproject/cortex/pkg/querier/series"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/exemplar"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/rules"
	"github.com/prometheus/prometheus/storage"
)

const (
	AlertForStateMetricName = "ALERTS_FOR_STATE"
	statusSuccess           = "success"
	statusFailure           = "failure"
)

type NoopAppender struct{}

func (a NoopAppender) Appender(_ context.Context) storage.Appender             { return a }
func (a NoopAppender) Add(l labels.Labels, t int64, v float64) (uint64, error) { return 0, nil }
func (a NoopAppender) Append(ref uint64, l labels.Labels, t int64, v float64) (uint64, error) {
	return 0, errors.New("unimplemented")
}
func (a NoopAppender) Commit() error   { return nil }
func (a NoopAppender) Rollback() error { return nil }
func (a NoopAppender) AppendExemplar(ref uint64, l labels.Labels, e exemplar.Exemplar) (uint64, error) {
	return 0, errors.New("unimplemented")
}

func ForStateMetric(base labels.Labels, alertName string) labels.Labels {
	b := labels.NewBuilder(base)
	b.Set(labels.MetricName, AlertForStateMetricName)
	b.Set(labels.AlertName, alertName)
	return b.Labels()
}

type memstoreMetrics struct {
	evaluations *prometheus.CounterVec
	samples     prometheus.Gauge       // in memory samples
	cacheHits   *prometheus.CounterVec // cache hits on in memory samples
}

func newMemstoreMetrics(r prometheus.Registerer) *memstoreMetrics {
	return &memstoreMetrics{
		evaluations: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki",
			Name:      "ruler_memory_for_state_evaluations_total",
		}, []string{"status", "tenant"}),
		samples: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: "loki",
			Name:      "ruler_memory_samples",
		}),
		cacheHits: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki",
			Name:      "ruler_memory_for_state_cache_hits_total",
		}, []string{"tenant"}),
	}
}

type RuleIter interface {
	AlertingRules() []*rules.AlertingRule
}

type MemStore struct {
	mtx       sync.Mutex
	userID    string
	queryFunc rules.QueryFunc
	metrics   *memstoreMetrics
	mgr       RuleIter
	logger    log.Logger
	rules     map[string]*RuleCache

	initiated       chan struct{}
	done            chan struct{}
	cleanupInterval time.Duration
}

func NewMemStore(userID string, queryFunc rules.QueryFunc, metrics *memstoreMetrics, cleanupInterval time.Duration, logger log.Logger) *MemStore {
	s := &MemStore{
		userID:          userID,
		metrics:         metrics,
		queryFunc:       queryFunc,
		logger:          log.With(logger, "subcomponent", "MemStore", "user", userID),
		cleanupInterval: cleanupInterval,
		rules:           make(map[string]*RuleCache),

		initiated: make(chan struct{}), // blocks execution until Start() is called
		done:      make(chan struct{}),
	}
	return s

}

// Calling Start will set the RuleIter, unblock the MemStore, and start the run() function in a separate goroutine.
func (m *MemStore) Start(iter RuleIter) {
	m.mgr = iter
	close(m.initiated)
	go m.run()
}

func (m *MemStore) Stop() {
	select {
	case <-m.initiated:
	default:
		// If initiated is blocked, the MemStore has yet to start: easy no-op.
		return
	}

	// Need to nil all series & decrement gauges
	m.mtx.Lock()
	defer m.mtx.Unlock()

	select {
	// ensures Stop() is idempotent
	case <-m.done:
		return
	default:
		for ruleKey, cache := range m.rules {
			// Force cleanup of all samples older than time.Now (all of them).
			_ = cache.CleanupOldSamples(time.Now())
			delete(m.rules, ruleKey)
		}
		close(m.done)
	}
}

// run periodically cleans up old series/samples to ensure memory consumption doesn't grow unbounded.
func (m *MemStore) run() {
	<-m.initiated
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

			for ruleKey, cache := range m.rules {
				dur, ok := holdDurs[ruleKey]

				// rule is no longer being tracked, remove it
				if !ok {
					_ = cache.CleanupOldSamples(time.Now())
					delete(m.rules, ruleKey)
					continue
				}

				// trim older samples out of tracking bounds, doubled to buffer.
				if empty := cache.CleanupOldSamples(time.Now().Add(-2 * dur)); empty {
					delete(m.rules, ruleKey)
				}

			}

			m.mtx.Unlock()
		}
	}
}

// implement storage.Queryable. It is only called with the desired ts as maxtime. Mint is
// parameterized via the outage tolerance, but since we're synthetically generating these,
// we only care about the desired time.
func (m *MemStore) Querier(ctx context.Context, mint, maxt int64) (storage.Querier, error) {
	<-m.initiated
	return &memStoreQuerier{
		ts:       util.TimeFromMillis(maxt),
		MemStore: m,
		ctx:      ctx,
	}, nil

}

type memStoreQuerier struct {
	ts  time.Time
	ctx context.Context
	*MemStore
}

// Select implements storage.Querier but takes advantage of the fact that it's only called when restoring for state
// in order to lookup & cache previous rule evaluations. This results in a sort of synthetic metric store.
func (m *memStoreQuerier) Select(sortSeries bool, params *storage.SelectHints, matchers ...*labels.Matcher) storage.SeriesSet {
	b := labels.NewBuilder(nil)
	var ruleKey string
	for _, matcher := range matchers {
		// Since Select is only called to restore the for state of an alert, we can deduce two things:
		// 1) The matchers will all be in the form {foo="bar"}. This means we can construct the cache entry from these matchers.
		// 2) The alertname label value can be used to discover the rule this query is associated with.
		b.Set(matcher.Name, matcher.Value)
		if matcher.Name == labels.AlertName && matcher.Type == labels.MatchEqual {
			ruleKey = matcher.Value
		}
	}
	ls := b.Labels()
	if ruleKey == "" {
		level.Error(m.logger).Log("msg", "Select called in an unexpected fashion without alertname or ALERTS_FOR_STATE labels")
		return storage.NoopSeriesSet()
	}

	var rule *rules.AlertingRule

	// go fetch the rule via the alertname
	for _, x := range m.mgr.AlertingRules() {
		if x.Name() == ruleKey {
			rule = x
			break
		}
	}

	// should not happen
	if rule == nil {
		level.Error(m.logger).Log("msg", "failure trying to restore for state for untracked alerting rule", "name", ruleKey)
		return storage.NoopSeriesSet()
	}

	level.Debug(m.logger).Log("msg", "restoring for state via evaluation", "rule", ruleKey)

	m.mtx.Lock()
	defer m.mtx.Unlock()
	cache, ok := m.rules[ruleKey]

	// no timestamp results are cached for this rule at all; Create it.
	if !ok {
		cache = NewRuleCache(m.metrics)
		m.rules[ruleKey] = cache
	}

	smpl, cached := cache.Get(m.ts, ls)
	if cached {
		m.metrics.cacheHits.WithLabelValues(m.userID).Inc()
		level.Debug(m.logger).Log("msg", "result cached", "rule", ruleKey)
		// Assuming the result is cached but the desired series is not in the result, it wouldn't be considered active.
		if smpl == nil {
			return storage.NoopSeriesSet()
		}

		// If the labelset is cached we can consider it active. Return the for state sample active immediately.
		return series.NewConcreteSeriesSet(
			[]storage.Series{
				series.NewConcreteSeries(smpl.Metric, []model.SamplePair{
					{Timestamp: model.Time(util.TimeToMillis(m.ts)), Value: model.SampleValue(smpl.V)},
				}),
			},
		)
	}

	// see if alert condition had any inhabitants at ts-forDuration. We can assume it's still firing because
	// that's the only condition under which this is queried (via RestoreForState).
	vec, err := m.queryFunc(m.ctx, rule.Query().String(), m.ts.Add(-rule.HoldDuration()))
	if err != nil {
		level.Info(m.logger).Log("msg", "error querying for rule", "rule", ruleKey, "err", err.Error())
		m.metrics.evaluations.WithLabelValues(statusFailure, m.userID).Inc()
		return storage.NoopSeriesSet()
	}
	m.metrics.evaluations.WithLabelValues(statusSuccess, m.userID).Inc()
	level.Debug(m.logger).Log("msg", "rule state successfully restored", "rule", ruleKey, "len", len(vec))

	// translate the result into the ALERTS_FOR_STATE series for caching,
	// considered active & written at the timetamp requested
	forStateVec := make(promql.Vector, 0, len(vec))
	for _, smpl := range vec {

		ts := util.TimeToMillis(m.ts)

		forStateVec = append(forStateVec, promql.Sample{
			Metric: ForStateMetric(smpl.Metric, rule.Name()),
			Point: promql.Point{
				T: ts,
				V: float64(ts),
			},
		})

	}

	// cache the result of the evaluation at this timestamp
	cache.Set(m.ts, forStateVec)

	// Finally return the series if it exists.
	// Calling cache.Get leverages the existing code to return only single sample.
	smpl, ok = cache.Get(m.ts, ls)
	if !ok || smpl == nil {
		return storage.NoopSeriesSet()
	}
	// If the labelset is cached we can consider it active. Return the for state sample active immediately.
	return series.NewConcreteSeriesSet(
		[]storage.Series{
			series.NewConcreteSeries(smpl.Metric, []model.SamplePair{
				{Timestamp: model.Time(util.TimeToMillis(m.ts)), Value: model.SampleValue(smpl.V)},
			}),
		},
	)
}

// LabelValues returns all potential values for a label name.
func (*memStoreQuerier) LabelValues(name string, matchers ...*labels.Matcher) ([]string, storage.Warnings, error) {
	return nil, nil, errors.New("unimplemented")
}

// LabelNames returns all the unique label names present in the block in sorted order.
func (*memStoreQuerier) LabelNames(matchers ...*labels.Matcher) ([]string, storage.Warnings, error) {
	return nil, nil, errors.New("unimplemented")
}

// Close releases the resources of the Querier.
func (*memStoreQuerier) Close() error { return nil }

type RuleCache struct {
	mtx     sync.Mutex
	metrics *memstoreMetrics
	data    map[int64]map[uint64]promql.Sample
}

func NewRuleCache(metrics *memstoreMetrics) *RuleCache {
	return &RuleCache{
		data:    make(map[int64]map[uint64]promql.Sample),
		metrics: metrics,
	}
}

func (c *RuleCache) Set(ts time.Time, vec promql.Vector) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	tsMap, ok := c.data[ts.UnixNano()]
	if !ok {
		tsMap = make(map[uint64]promql.Sample)
		c.data[ts.UnixNano()] = tsMap
	}

	for _, sample := range vec {
		tsMap[sample.Metric.Hash()] = sample
	}
	c.metrics.samples.Add(float64(len(vec)))
}

// Get returns ok if that timestamp's result is cached.
func (c *RuleCache) Get(ts time.Time, ls labels.Labels) (*promql.Sample, bool) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	match, ok := c.data[ts.UnixNano()]
	if !ok {
		return nil, false
	}

	smp, ok := match[ls.Hash()]
	if !ok {
		return nil, true
	}
	return &smp, true

}

// CleanupOldSamples removes samples that are outside of the rule's `For` duration.
func (c *RuleCache) CleanupOldSamples(olderThan time.Time) (empty bool) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	ns := olderThan.UnixNano()

	// This could be more efficient (logarithmic instead of linear)
	for ts, tsMap := range c.data {
		if ts < ns {
			delete(c.data, ts)
			c.metrics.samples.Add(-float64(len(tsMap)))
		}

	}
	return len(c.data) == 0
}
