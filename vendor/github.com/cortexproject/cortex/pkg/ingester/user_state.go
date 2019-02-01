package ingester

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/segmentio/fasthash/fnv1a"

	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/ingester/index"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/extract"
	"github.com/cortexproject/cortex/pkg/util/spanlogger"
	"github.com/cortexproject/cortex/pkg/util/validation"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/user"
)

var (
	memSeries = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "cortex_ingester_memory_series",
		Help: "The current number of series in memory.",
	})
	memUsers = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "cortex_ingester_memory_users",
		Help: "The current number of users in memory.",
	})
	memSeriesCreatedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "cortex_ingester_memory_series_created_total",
		Help: "The total number of series that were created per user.",
	}, []string{"user"})
	memSeriesRemovedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "cortex_ingester_memory_series_removed_total",
		Help: "The total number of series that were removed per user.",
	}, []string{"user"})
)

type userStates struct {
	states sync.Map
	limits *validation.Overrides
	cfg    Config
}

type userState struct {
	limits              *validation.Overrides
	userID              string
	fpLocker            *fingerprintLocker
	fpToSeries          *seriesMap
	mapper              *fpMapper
	index               *index.InvertedIndex
	ingestedAPISamples  *ewmaRate
	ingestedRuleSamples *ewmaRate

	seriesInMetric []metricCounterShard

	memSeriesCreatedTotal prometheus.Counter
	memSeriesRemovedTotal prometheus.Counter
}

const metricCounterShards = 128

type metricCounterShard struct {
	mtx sync.Mutex
	m   map[string]int
}

func newUserStates(limits *validation.Overrides, cfg Config) *userStates {
	return &userStates{
		limits: limits,
		cfg:    cfg,
	}
}

func (us *userStates) cp() map[string]*userState {
	states := map[string]*userState{}
	us.states.Range(func(key, value interface{}) bool {
		states[key.(string)] = value.(*userState)
		return true
	})
	return states
}

func (us *userStates) gc() {
	us.states.Range(func(key, value interface{}) bool {
		state := value.(*userState)
		if state.fpToSeries.length() == 0 {
			us.states.Delete(key)
		}
		return true
	})
}

func (us *userStates) updateRates() {
	us.states.Range(func(key, value interface{}) bool {
		state := value.(*userState)
		state.ingestedAPISamples.tick()
		state.ingestedRuleSamples.tick()
		return true
	})
}

func (us *userStates) get(userID string) (*userState, bool) {
	state, ok := us.states.Load(userID)
	if !ok {
		return nil, ok
	}
	return state.(*userState), ok
}

func (us *userStates) getViaContext(ctx context.Context) (*userState, bool, error) {
	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("no user id")
	}
	state, ok := us.get(userID)
	return state, ok, nil
}

func (us *userStates) getOrCreateSeries(ctx context.Context, labels labelPairs) (*userState, model.Fingerprint, *memorySeries, error) {
	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("no user id")
	}

	state, ok := us.get(userID)
	if !ok {

		seriesInMetric := make([]metricCounterShard, 0, metricCounterShards)
		for i := 0; i < metricCounterShards; i++ {
			seriesInMetric = append(seriesInMetric, metricCounterShard{
				m: map[string]int{},
			})
		}

		// Speculatively create a userState object and try to store it
		// in the map.  Another goroutine may have got there before
		// us, in which case this userState will be discarded
		state = &userState{
			userID:              userID,
			limits:              us.limits,
			fpToSeries:          newSeriesMap(),
			fpLocker:            newFingerprintLocker(16 * 1024),
			index:               index.New(),
			ingestedAPISamples:  newEWMARate(0.2, us.cfg.RateUpdatePeriod),
			ingestedRuleSamples: newEWMARate(0.2, us.cfg.RateUpdatePeriod),
			seriesInMetric:      seriesInMetric,

			memSeriesCreatedTotal: memSeriesCreatedTotal.WithLabelValues(userID),
			memSeriesRemovedTotal: memSeriesRemovedTotal.WithLabelValues(userID),
		}
		state.mapper = newFPMapper(state.fpToSeries)
		stored, ok := us.states.LoadOrStore(userID, state)
		if !ok {
			memUsers.Inc()
		}
		state = stored.(*userState)
	}

	fp, series, err := state.getSeries(labels)
	return state, fp, series, err
}

func (u *userState) getSeries(metric labelPairs) (model.Fingerprint, *memorySeries, error) {
	rawFP := client.FastFingerprint(metric)
	u.fpLocker.Lock(rawFP)
	fp := u.mapper.mapFP(rawFP, metric)
	if fp != rawFP {
		u.fpLocker.Unlock(rawFP)
		u.fpLocker.Lock(fp)
	}

	series, ok := u.fpToSeries.get(fp)
	if ok {
		return fp, series, nil
	}

	// There's theoretically a relatively harmless race here if multiple
	// goroutines get the length of the series map at the same time, then
	// all proceed to add a new series. This is likely not worth addressing,
	// as this should happen rarely (all samples from one push are added
	// serially), and the overshoot in allowed series would be minimal.
	if u.fpToSeries.length() >= u.limits.MaxSeriesPerUser(u.userID) {
		u.fpLocker.Unlock(fp)
		validation.DiscardedSamples.WithLabelValues(validation.RateLimited, u.userID).Inc()
		return fp, nil, httpgrpc.Errorf(http.StatusTooManyRequests, "per-user series limit (%d) exceeded", u.limits.MaxSeriesPerUser(u.userID))
	}

	metricName, err := extract.MetricNameFromLabelPairs(metric)
	if err != nil {
		u.fpLocker.Unlock(fp)
		return fp, nil, err
	}

	if !u.canAddSeriesFor(string(metricName)) {
		u.fpLocker.Unlock(fp)
		validation.DiscardedSamples.WithLabelValues(validation.RateLimited, u.userID).Inc()
		return fp, nil, httpgrpc.Errorf(http.StatusTooManyRequests, "per-metric series limit (%d) exceeded for %s: %s", u.limits.MaxSeriesPerMetric(u.userID), metricName, metric)
	}

	u.memSeriesCreatedTotal.Inc()
	memSeries.Inc()

	series = newMemorySeries(metric)
	u.fpToSeries.put(fp, series)
	u.index.Add(metric, fp)

	return fp, series, nil
}

func (u *userState) canAddSeriesFor(metric string) bool {
	shard := &u.seriesInMetric[util.HashFP(model.Fingerprint(fnv1a.HashString64(string(metric))))%metricCounterShards]
	shard.mtx.Lock()
	defer shard.mtx.Unlock()

	if shard.m[metric] >= u.limits.MaxSeriesPerMetric(u.userID) {
		return false
	}
	shard.m[metric]++
	return true
}

func (u *userState) removeSeries(fp model.Fingerprint, metric labelPairs) {
	u.fpToSeries.del(fp)
	u.index.Delete(metric, fp)

	metricNameB, err := extract.MetricNameFromLabelPairs(metric)
	if err != nil {
		// Series without a metric name should never be able to make it into
		// the ingester's memory storage.
		panic(err)
	}
	metricName := string(metricNameB)

	shard := &u.seriesInMetric[util.HashFP(model.Fingerprint(fnv1a.HashString64(string(metricName))))%metricCounterShards]
	shard.mtx.Lock()
	defer shard.mtx.Unlock()

	shard.m[metricName]--
	if shard.m[metricName] == 0 {
		delete(shard.m, metricName)
	}

	u.memSeriesRemovedTotal.Inc()
	memSeries.Dec()
}

// forSeriesMatching passes all series matching the given matchers to the provided callback.
// Deals with locking and the quirks of zero-length matcher values.
func (u *userState) forSeriesMatching(ctx context.Context, allMatchers []*labels.Matcher, callback func(context.Context, model.Fingerprint, *memorySeries) error) error {
	log, ctx := spanlogger.New(ctx, "forSeriesMatching")
	defer log.Finish()

	filters, matchers := util.SplitFiltersAndMatchers(allMatchers)
	fps := u.index.Lookup(matchers)
	if len(fps) > u.limits.MaxSeriesPerQuery(u.userID) {
		return httpgrpc.Errorf(http.StatusRequestEntityTooLarge, "exceeded maximum number of series in a query")
	}

	level.Debug(log).Log("series", len(fps))

	// fps is sorted, lock them in order to prevent deadlocks
outer:
	for _, fp := range fps {
		if err := ctx.Err(); err != nil {
			return err
		}

		u.fpLocker.Lock(fp)
		series, ok := u.fpToSeries.get(fp)
		if !ok {
			u.fpLocker.Unlock(fp)
			continue
		}

		for _, filter := range filters {
			if !filter.Matches(string(series.metric.valueForName([]byte(filter.Name)))) {
				u.fpLocker.Unlock(fp)
				continue outer
			}
		}

		err := callback(ctx, fp, series)
		u.fpLocker.Unlock(fp)
		if err != nil {
			return err
		}
	}

	return nil
}
