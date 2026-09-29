package hintprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"golang.org/x/sync/singleflight"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
)

const (
	hintCacheKeyPrefix  = "logline:"
	hintCacheGeneration = 1
	hintCacheDayLayout  = "2006-01-02"
)

const (
	hintCacheResultHit  = "hit"
	hintCacheResultMiss = "miss"
	hintCacheResultSkip = "skip"
)

type skipCacheKey struct{}

// WithSkipCache marks a request context so CachingHintProvider bypasses cache.
func WithSkipCache(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, skipCacheKey{}, true)
}

// SkipCache reports whether cache lookups should be bypassed for this context.
func SkipCache(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(skipCacheKey{}).(bool)
	return v
}

type dayWindow struct {
	day          string
	start        time.Time
	endExclusive time.Time
	hashedKey    string
}

type cachedHints struct {
	TimeRanges []cachedTimeRange `json:"r,omitempty"`
}

type cachedTimeRange struct {
	StartMs int64 `json:"s"`
	EndMs   int64 `json:"e"`
}

type provideHintsResult struct {
	hints *Hints
	stats *QueryStats
	err   error
}

type minDateProvider interface {
	MinDate() time.Time
}

// CachingHintProvider wraps another QueryHintProvider with day-aligned caching.
type CachingHintProvider struct {
	delegate QueryHintProvider
	cache    cache.Cache

	flight singleflight.Group

	requestsTotal          *prometheus.CounterVec
	singleflightDedupedTot prometheus.Counter
}

// NewCachingHintProvider constructs a caching decorator for QueryHintProvider.
func NewCachingHintProvider(delegate QueryHintProvider, c cache.Cache, reg prometheus.Registerer) *CachingHintProvider {
	return &CachingHintProvider{
		delegate: delegate,
		cache:    c,
		requestsTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "logline_hint_cache_requests_total",
			Help: "Total hint cache lookup requests by result.",
		}, []string{"result"}),
		singleflightDedupedTot: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "logline_hint_cache_singleflight_deduped_total",
			Help: "Total deduplicated hint-cache lookups via singleflight.",
		}),
	}
}

func (p *CachingHintProvider) ProvideHints(
	ctx context.Context,
	tenant string,
	expr syntax.Expr,
	from, through model.Time,
) (*Hints, *QueryStats, error) {
	if p.delegate == nil {
		return nil, nil, fmt.Errorf("caching hint provider delegate cannot be nil")
	}

	if SkipCache(ctx) {
		p.requestsTotal.WithLabelValues(hintCacheResultSkip).Inc()
		hints, stats, err := p.delegate.ProvideHints(ctx, tenant, expr, from, through)
		if stats != nil {
			stats.ObserveHintCache(hintCacheResultSkip, 0, 0)
		}
		return filterHintsByWindow(hints, from, through), stats, err
	}
	if p.cache == nil {
		hints, stats, err := p.delegate.ProvideHints(ctx, tenant, expr, from, through)
		return filterHintsByWindow(hints, from, through), stats, err
	}

	queryString := expr.String()
	minDate := cacheKeyMinDate(p.delegate)
	dayWindows := buildDayWindows(tenant, queryString, minDate, from.Time(), through.Time())
	cacheKeys := make([]string, 0, len(dayWindows))
	for _, day := range dayWindows {
		cacheKeys = append(cacheKeys, day.hashedKey)
	}

	cachedRanges := []HintTimeRange(nil)
	missingDays := dayWindows
	daysHit := 0
	found, bufs, missing, err := p.cache.Fetch(ctx, cacheKeys)
	if err == nil {
		cachedRanges, missingDays = decodeCachedAndMissingDays(dayWindows, found, bufs, missing)
		daysHit = len(dayWindows) - len(missingDays)
		if len(missingDays) == 0 {
			p.requestsTotal.WithLabelValues(hintCacheResultHit).Inc()
			stats := NewQueryStats()
			stats.ObserveHintCache(hintCacheResultHit, len(cacheKeys), len(found))
			return filterHintsByWindow(&Hints{TimeRanges: cachedRanges}, from, through), stats, nil
		}
	}

	p.requestsTotal.WithLabelValues(hintCacheResultMiss).Inc()
	combinedRanges := append([]HintTimeRange(nil), cachedRanges...)
	combinedStats := NewQueryStats()
	for _, day := range missingDays {
		dayFrom := model.TimeFromUnixNano(day.start.UnixNano())
		dayThrough := model.TimeFromUnixNano(day.endExclusive.Add(-time.Nanosecond).UnixNano())
		sfKey := singleflightKey(tenant, queryString, day.day)
		value, _, shared := p.flight.Do(sfKey, func() (any, error) {
			hints, stats, provideErr := p.delegate.ProvideHints(ctx, tenant, expr, dayFrom, dayThrough)
			result := &provideHintsResult{
				hints: hints,
				stats: stats,
				err:   provideErr,
			}
			if provideErr != nil {
				return result, nil
			}
			if result.hints == nil {
				result.hints = &Hints{}
			}

			p.storeDays(ctx, []dayWindow{day}, result.hints.TimeRanges)
			return result, nil
		})
		if shared {
			p.singleflightDedupedTot.Inc()
		}

		result, ok := value.(*provideHintsResult)
		if !ok {
			return nil, nil, fmt.Errorf("unexpected singleflight result type %T", value)
		}
		if result.err != nil {
			return nil, result.stats, result.err
		}
		if result.hints == nil {
			result.hints = &Hints{}
		}
		if result.stats != nil {
			combinedStats.Merge(result.stats)
		}
		combinedRanges = append(combinedRanges, result.hints.TimeRanges...)
	}

	combinedStats.ObserveHintCache(hintCacheResultMiss, len(cacheKeys), daysHit)
	return filterHintsByWindow(&Hints{TimeRanges: combinedRanges}, from, through), combinedStats, nil
}

func filterHintsByWindow(hints *Hints, from, through model.Time) *Hints {
	if hints == nil {
		return nil
	}

	filtered := *hints
	filtered.TimeRanges = filterRangesByWindow(hints.TimeRanges, from.Time().UTC(), through.Time().UTC())
	return &filtered
}

func (p *CachingHintProvider) storeDays(ctx context.Context, days []dayWindow, ranges []HintTimeRange) {
	if p.cache == nil || len(days) == 0 {
		return
	}

	keys := make([]string, 0, len(days))
	values := make([][]byte, 0, len(days))
	for _, day := range days {
		clipped := clipRangesToDay(ranges, day.start, day.endExclusive)
		encoded, err := marshalCachedHints(clipped)
		if err != nil {
			continue
		}
		keys = append(keys, day.hashedKey)
		values = append(values, encoded)
	}
	if len(keys) == 0 {
		return
	}
	_ = p.cache.Store(ctx, keys, values)
}

func decodeCachedAndMissingDays(days []dayWindow, found []string, bufs [][]byte, missing []string) ([]HintTimeRange, []dayWindow) {
	if len(days) == 0 {
		return nil, nil
	}

	missingByKey := make(map[string]struct{}, len(missing))
	for _, key := range missing {
		missingByKey[key] = struct{}{}
	}

	decodedRanges := make([]HintTimeRange, 0, len(days))
	missingDays := make([]dayWindow, 0, len(days))
	if len(found) != len(bufs) {
		return nil, append(missingDays, days...)
	}

	foundByKey := make(map[string][]byte, len(found))
	for i, key := range found {
		foundByKey[key] = bufs[i]
	}

	for _, day := range days {
		if _, markedMissing := missingByKey[day.hashedKey]; markedMissing {
			missingDays = append(missingDays, day)
			continue
		}

		raw, ok := foundByKey[day.hashedKey]
		if !ok {
			missingDays = append(missingDays, day)
			continue
		}
		decoded, err := unmarshalCachedHints(raw)
		if err != nil {
			missingDays = append(missingDays, day)
			continue
		}
		decodedRanges = append(decodedRanges, decoded...)
	}

	return normalizeRanges(decodedRanges), missingDays
}

func cacheKeyMinDate(delegate QueryHintProvider) string {
	mdp, ok := delegate.(minDateProvider)
	if !ok {
		return ""
	}
	minDate := mdp.MinDate().UTC()
	if minDate.IsZero() {
		return ""
	}
	return minDate.Format(hintCacheDayLayout)
}

func buildDayWindows(tenant, query, minDate string, from, through time.Time) []dayWindow {
	start := from.UTC()
	end := through.UTC()
	if end.Before(start) {
		end = start
	}

	first := truncateToUTCDay(start)
	last := truncateToUTCDay(end)

	days := make([]dayWindow, 0, int(last.Sub(first)/(24*time.Hour))+1)
	for day := first; !day.After(last); day = day.Add(24 * time.Hour) {
		dayString := day.Format(hintCacheDayLayout)
		logical := buildHintCacheLogicalKey(tenant, query, minDate, dayString)
		days = append(days, dayWindow{
			day:          dayString,
			start:        day,
			endExclusive: day.Add(24 * time.Hour),
			hashedKey:    cache.HashKey(logical),
		})
	}
	return days
}

func buildHintCacheLogicalKey(tenant, query, minDate, day string) string {
	return fmt.Sprintf("%s%d:%s:%s:%s:%s", hintCacheKeyPrefix, hintCacheGeneration, tenant, query, minDate, day)
}

func singleflightKey(tenant, query, day string) string {
	return fmt.Sprintf("%s:%s:%s", tenant, query, day)
}

func filterRangesByWindow(ranges []HintTimeRange, from, through time.Time) []HintTimeRange {
	if len(ranges) == 0 {
		return nil
	}
	if through.Before(from) {
		through = from
	}

	filtered := make([]HintTimeRange, 0, len(ranges))
	for _, r := range ranges {
		if r.End.Before(from) || r.Start.After(through) {
			continue
		}
		filtered = append(filtered, r)
	}
	return normalizeRanges(filtered)
}

func truncateToUTCDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func clipRangesToDay(ranges []HintTimeRange, dayStart, dayEndExclusive time.Time) []HintTimeRange {
	if len(ranges) == 0 {
		return nil
	}

	out := make([]HintTimeRange, 0, len(ranges))
	for _, r := range ranges {
		if r.End.Before(dayStart) || !r.Start.Before(dayEndExclusive) {
			continue
		}

		// End is exclusive, so it clips to the day boundary itself. Backing off by a
		// nanosecond would round down to the previous millisecond once the payload is
		// serialized, leaving a gap between this day and the next: a range reaching
		// midnight would end 23:59:59.999 while the next day starts 00:00:00.000, and
		// normalizeRanges cannot bridge that.
		clipped := HintTimeRange{
			Start: maxTime(r.Start, dayStart).UTC(),
			End:   minTime(r.End, dayEndExclusive).UTC(),
		}
		if clipped.End.Before(clipped.Start) {
			continue
		}
		out = append(out, clipped)
	}

	return normalizeRanges(out)
}

func marshalCachedHints(ranges []HintTimeRange) ([]byte, error) {
	if len(ranges) == 0 {
		return json.Marshal(cachedHints{})
	}

	encoded := make([]cachedTimeRange, 0, len(ranges))
	for _, r := range ranges {
		encoded = append(encoded, cachedTimeRange{
			StartMs: r.Start.UTC().UnixMilli(),
			EndMs:   r.End.UTC().UnixMilli(),
		})
	}
	return json.Marshal(cachedHints{TimeRanges: encoded})
}

func unmarshalCachedHints(encoded []byte) ([]HintTimeRange, error) {
	if len(encoded) == 0 {
		return nil, nil
	}

	var payload cachedHints
	if err := json.Unmarshal(encoded, &payload); err != nil {
		return nil, err
	}
	if len(payload.TimeRanges) == 0 {
		return nil, nil
	}

	ranges := make([]HintTimeRange, 0, len(payload.TimeRanges))
	for _, r := range payload.TimeRanges {
		start := time.UnixMilli(r.StartMs).UTC()
		end := time.UnixMilli(r.EndMs).UTC()
		if end.Before(start) {
			end = start
		}
		ranges = append(ranges, HintTimeRange{
			Start: start,
			End:   end,
		})
	}
	return normalizeRanges(ranges), nil
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

var _ QueryHintProvider = (*CachingHintProvider)(nil)
