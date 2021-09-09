package purger

import (
	"context"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql/parser"

	util_log "github.com/grafana/loki/pkg/util/log"
)

const tombstonesReloadDuration = 5 * time.Minute

type tombstonesLoaderMetrics struct {
	cacheGenLoadFailures       prometheus.Counter
	deleteRequestsLoadFailures prometheus.Counter
}

func newtombstonesLoaderMetrics(r prometheus.Registerer) *tombstonesLoaderMetrics {
	m := tombstonesLoaderMetrics{}

	m.cacheGenLoadFailures = promauto.With(r).NewCounter(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "tombstones_loader_cache_gen_load_failures_total",
		Help:      "Total number of failures while loading cache generation number using tombstones loader",
	})
	m.deleteRequestsLoadFailures = promauto.With(r).NewCounter(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "tombstones_loader_cache_delete_requests_load_failures_total",
		Help:      "Total number of failures while loading delete requests using tombstones loader",
	})

	return &m
}

// TombstonesSet holds all the pending delete requests for a user
type TombstonesSet struct {
	tombstones                               []DeleteRequest
	oldestTombstoneStart, newestTombstoneEnd model.Time // Used as optimization to find whether we want to iterate over tombstones or not
}

// Used for easier injection of mocks.
type DeleteStoreAPI interface {
	getCacheGenerationNumbers(ctx context.Context, user string) (*cacheGenNumbers, error)
	GetPendingDeleteRequestsForUser(ctx context.Context, id string) ([]DeleteRequest, error)
}

// TombstonesLoader loads delete requests and gen numbers from store and keeps checking for updates.
// It keeps checking for changes in gen numbers, which also means changes in delete requests and reloads specific users delete requests.
type TombstonesLoader struct {
	tombstones    map[string]*TombstonesSet
	tombstonesMtx sync.RWMutex

	cacheGenNumbers    map[string]*cacheGenNumbers
	cacheGenNumbersMtx sync.RWMutex

	deleteStore DeleteStoreAPI
	metrics     *tombstonesLoaderMetrics
	quit        chan struct{}
}

// NewTombstonesLoader creates a TombstonesLoader
func NewTombstonesLoader(deleteStore DeleteStoreAPI, registerer prometheus.Registerer) *TombstonesLoader {
	tl := TombstonesLoader{
		tombstones:      map[string]*TombstonesSet{},
		cacheGenNumbers: map[string]*cacheGenNumbers{},
		deleteStore:     deleteStore,
		metrics:         newtombstonesLoaderMetrics(registerer),
	}
	go tl.loop()

	return &tl
}

// Stop stops TombstonesLoader
func (tl *TombstonesLoader) Stop() {
	close(tl.quit)
}

func (tl *TombstonesLoader) loop() {
	if tl.deleteStore == nil {
		return
	}

	tombstonesReloadTimer := time.NewTicker(tombstonesReloadDuration)
	for {
		select {
		case <-tombstonesReloadTimer.C:
			err := tl.reloadTombstones()
			if err != nil {
				level.Error(util_log.Logger).Log("msg", "error reloading tombstones", "err", err)
			}
		case <-tl.quit:
			return
		}
	}
}

func (tl *TombstonesLoader) reloadTombstones() error {
	updatedGenNumbers := make(map[string]*cacheGenNumbers)
	tl.cacheGenNumbersMtx.RLock()

	// check for updates in loaded gen numbers
	for userID, oldGenNumbers := range tl.cacheGenNumbers {
		newGenNumbers, err := tl.deleteStore.getCacheGenerationNumbers(context.Background(), userID)
		if err != nil {
			tl.cacheGenNumbersMtx.RUnlock()
			return err
		}

		if *oldGenNumbers != *newGenNumbers {
			updatedGenNumbers[userID] = newGenNumbers
		}
	}

	tl.cacheGenNumbersMtx.RUnlock()

	// in frontend we load only cache gen numbers so short circuit here if there are no loaded deleted requests
	// first call to GetPendingTombstones would avoid doing this.
	tl.tombstonesMtx.RLock()
	if len(tl.tombstones) == 0 {
		tl.tombstonesMtx.RUnlock()
		return nil
	}
	tl.tombstonesMtx.RUnlock()

	// for all the updated gen numbers, reload delete requests
	for userID, genNumbers := range updatedGenNumbers {
		err := tl.loadPendingTombstones(userID)
		if err != nil {
			return err
		}

		tl.cacheGenNumbersMtx.Lock()
		tl.cacheGenNumbers[userID] = genNumbers
		tl.cacheGenNumbersMtx.Unlock()
	}

	return nil
}

// GetPendingTombstones returns all pending tombstones
func (tl *TombstonesLoader) GetPendingTombstones(userID string) (*TombstonesSet, error) {
	tl.tombstonesMtx.RLock()

	tombstoneSet, isOK := tl.tombstones[userID]
	if isOK {
		tl.tombstonesMtx.RUnlock()
		return tombstoneSet, nil
	}

	tl.tombstonesMtx.RUnlock()
	err := tl.loadPendingTombstones(userID)
	if err != nil {
		return nil, err
	}

	tl.tombstonesMtx.RLock()
	defer tl.tombstonesMtx.RUnlock()

	return tl.tombstones[userID], nil
}

// GetPendingTombstones returns all pending tombstones
func (tl *TombstonesLoader) GetPendingTombstonesForInterval(userID string, from, to model.Time) (*TombstonesSet, error) {
	allTombstones, err := tl.GetPendingTombstones(userID)
	if err != nil {
		return nil, err
	}

	if !allTombstones.HasTombstonesForInterval(from, to) {
		return &TombstonesSet{}, nil
	}

	filteredSet := TombstonesSet{oldestTombstoneStart: model.Now()}

	for _, tombstone := range allTombstones.tombstones {
		if !intervalsOverlap(model.Interval{Start: from, End: to}, model.Interval{Start: tombstone.StartTime, End: tombstone.EndTime}) {
			continue
		}

		filteredSet.tombstones = append(filteredSet.tombstones, tombstone)

		if tombstone.StartTime < filteredSet.oldestTombstoneStart {
			filteredSet.oldestTombstoneStart = tombstone.StartTime
		}

		if tombstone.EndTime > filteredSet.newestTombstoneEnd {
			filteredSet.newestTombstoneEnd = tombstone.EndTime
		}
	}

	return &filteredSet, nil
}

func (tl *TombstonesLoader) loadPendingTombstones(userID string) error {
	if tl.deleteStore == nil {
		tl.tombstonesMtx.Lock()
		defer tl.tombstonesMtx.Unlock()

		tl.tombstones[userID] = &TombstonesSet{oldestTombstoneStart: 0, newestTombstoneEnd: 0}
		return nil
	}

	pendingDeleteRequests, err := tl.deleteStore.GetPendingDeleteRequestsForUser(context.Background(), userID)
	if err != nil {
		tl.metrics.deleteRequestsLoadFailures.Inc()
		return errors.Wrap(err, "error loading delete requests")
	}

	tombstoneSet := TombstonesSet{tombstones: pendingDeleteRequests, oldestTombstoneStart: model.Now()}
	for i := range tombstoneSet.tombstones {
		tombstoneSet.tombstones[i].Matchers = make([][]*labels.Matcher, len(tombstoneSet.tombstones[i].Selectors))

		for j, selector := range tombstoneSet.tombstones[i].Selectors {
			tombstoneSet.tombstones[i].Matchers[j], err = parser.ParseMetricSelector(selector)

			if err != nil {
				tl.metrics.deleteRequestsLoadFailures.Inc()
				return errors.Wrapf(err, "error parsing metric selector")
			}
		}

		if tombstoneSet.tombstones[i].StartTime < tombstoneSet.oldestTombstoneStart {
			tombstoneSet.oldestTombstoneStart = tombstoneSet.tombstones[i].StartTime
		}

		if tombstoneSet.tombstones[i].EndTime > tombstoneSet.newestTombstoneEnd {
			tombstoneSet.newestTombstoneEnd = tombstoneSet.tombstones[i].EndTime
		}
	}

	tl.tombstonesMtx.Lock()
	defer tl.tombstonesMtx.Unlock()
	tl.tombstones[userID] = &tombstoneSet

	return nil
}

// GetStoreCacheGenNumber returns store cache gen number for a user
func (tl *TombstonesLoader) GetStoreCacheGenNumber(tenantIDs []string) string {
	return tl.getCacheGenNumbersPerTenants(tenantIDs).store
}

// GetResultsCacheGenNumber returns results cache gen number for a user
func (tl *TombstonesLoader) GetResultsCacheGenNumber(tenantIDs []string) string {
	return tl.getCacheGenNumbersPerTenants(tenantIDs).results
}

func (tl *TombstonesLoader) getCacheGenNumbersPerTenants(tenantIDs []string) *cacheGenNumbers {
	var result cacheGenNumbers

	if len(tenantIDs) == 0 {
		return &result
	}

	// keep the maximum value that's currently in result
	var maxResults, maxStore int

	for pos, tenantID := range tenantIDs {
		numbers := tl.getCacheGenNumbers(tenantID)

		// handle first tenant in the list
		if pos == 0 {
			// short cut if there is only one tenant
			if len(tenantIDs) == 1 {
				return numbers
			}

			// set first tenant string whatever happens next
			result.results = numbers.results
			result.store = numbers.store
		}

		// set results number string if it's higher than the ones before
		if numbers.results != "" {
			results, err := strconv.Atoi(numbers.results)
			if err != nil {
				level.Error(util_log.Logger).Log("msg", "error parsing resultsCacheGenNumber", "user", tenantID, "err", err)
			} else if maxResults < results {
				maxResults = results
				result.results = numbers.results
			}
		}

		// set store number string if it's higher than the ones before
		if numbers.store != "" {
			store, err := strconv.Atoi(numbers.store)
			if err != nil {
				level.Error(util_log.Logger).Log("msg", "error parsing storeCacheGenNumber", "user", tenantID, "err", err)
			} else if maxStore < store {
				maxStore = store
				result.store = numbers.store
			}
		}
	}

	return &result
}

func (tl *TombstonesLoader) getCacheGenNumbers(userID string) *cacheGenNumbers {
	tl.cacheGenNumbersMtx.RLock()
	if genNumbers, isOK := tl.cacheGenNumbers[userID]; isOK {
		tl.cacheGenNumbersMtx.RUnlock()
		return genNumbers
	}

	tl.cacheGenNumbersMtx.RUnlock()

	if tl.deleteStore == nil {
		tl.cacheGenNumbersMtx.Lock()
		defer tl.cacheGenNumbersMtx.Unlock()

		tl.cacheGenNumbers[userID] = &cacheGenNumbers{}
		return tl.cacheGenNumbers[userID]
	}

	genNumbers, err := tl.deleteStore.getCacheGenerationNumbers(context.Background(), userID)
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "error loading cache generation numbers", "err", err)
		tl.metrics.cacheGenLoadFailures.Inc()
		return &cacheGenNumbers{}
	}

	tl.cacheGenNumbersMtx.Lock()
	defer tl.cacheGenNumbersMtx.Unlock()

	tl.cacheGenNumbers[userID] = genNumbers
	return genNumbers
}

// GetDeletedIntervals returns non-overlapping, sorted  deleted intervals.
func (ts TombstonesSet) GetDeletedIntervals(lbls labels.Labels, from, to model.Time) []model.Interval {
	if len(ts.tombstones) == 0 || to < ts.oldestTombstoneStart || from > ts.newestTombstoneEnd {
		return nil
	}

	var deletedIntervals []model.Interval
	requestedInterval := model.Interval{Start: from, End: to}

	for i := range ts.tombstones {
		overlaps, overlappingInterval := getOverlappingInterval(requestedInterval,
			model.Interval{Start: ts.tombstones[i].StartTime, End: ts.tombstones[i].EndTime})

		if !overlaps {
			continue
		}

		matches := false
		for _, matchers := range ts.tombstones[i].Matchers {
			if labels.Selector(matchers).Matches(lbls) {
				matches = true
				break
			}
		}

		if !matches {
			continue
		}

		if overlappingInterval == requestedInterval {
			// whole interval deleted
			return []model.Interval{requestedInterval}
		}

		deletedIntervals = append(deletedIntervals, overlappingInterval)
	}

	if len(deletedIntervals) == 0 {
		return nil
	}

	return mergeIntervals(deletedIntervals)
}

// Len returns number of tombstones that are there
func (ts TombstonesSet) Len() int {
	return len(ts.tombstones)
}

// HasTombstonesForInterval tells whether there are any tombstones which overlapping given interval
func (ts TombstonesSet) HasTombstonesForInterval(from, to model.Time) bool {
	if len(ts.tombstones) == 0 || to < ts.oldestTombstoneStart || from > ts.newestTombstoneEnd {
		return false
	}

	return true
}

// sorts and merges overlapping intervals
func mergeIntervals(intervals []model.Interval) []model.Interval {
	if len(intervals) <= 1 {
		return intervals
	}

	mergedIntervals := make([]model.Interval, 0, len(intervals))
	sort.Slice(intervals, func(i, j int) bool {
		return intervals[i].Start < intervals[j].Start
	})

	ongoingTrFrom, ongoingTrTo := intervals[0].Start, intervals[0].End
	for i := 1; i < len(intervals); i++ {
		// if there is no overlap add it to mergedIntervals
		if intervals[i].Start > ongoingTrTo {
			mergedIntervals = append(mergedIntervals, model.Interval{Start: ongoingTrFrom, End: ongoingTrTo})
			ongoingTrFrom = intervals[i].Start
			ongoingTrTo = intervals[i].End
			continue
		}

		// there is an overlap but check whether existing time range is bigger than the current one
		if intervals[i].End > ongoingTrTo {
			ongoingTrTo = intervals[i].End
		}
	}

	// add the last time range
	mergedIntervals = append(mergedIntervals, model.Interval{Start: ongoingTrFrom, End: ongoingTrTo})

	return mergedIntervals
}

func getOverlappingInterval(interval1, interval2 model.Interval) (bool, model.Interval) {
	if interval2.Start > interval1.Start {
		interval1.Start = interval2.Start
	}

	if interval2.End < interval1.End {
		interval1.End = interval2.End
	}

	return interval1.Start < interval1.End, interval1
}

func intervalsOverlap(interval1, interval2 model.Interval) bool {
	if interval1.Start > interval2.End || interval2.Start > interval1.End {
		return false
	}

	return true
}
