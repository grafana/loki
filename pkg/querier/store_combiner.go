package querier

import (
	"context"
	"sort"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/seriesvolume"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/stats"
)

var _ Store = &StoreCombiner{}

// StoreConfig represents a store and its time range configuration
type StoreConfig struct {
	Store Store
	From  model.Time // queries >= From will use this store
}

// StoreCombiner combines multiple stores and routes queries to the appropriate store based on time range
type StoreCombiner struct {
	stores []StoreConfig
}

// NewStoreCombiner creates a new StoreCombiner with the given store configurations.
// The stores should be provided in order from newest to oldest time ranges.
func NewStoreCombiner(stores []StoreConfig) *StoreCombiner {
	// Sort stores by From time in ascending order to ensure proper time range matching
	sort.Slice(stores, func(i, j int) bool {
		return stores[i].From < stores[j].From
	})
	return &StoreCombiner{stores: stores}
}

// findStoresForTimeRange returns the stores that should handle the given time range
func (sc *StoreCombiner) findStoresForTimeRange(from, through model.Time) []storeWithRange {
	if len(sc.stores) == 0 {
		return nil
	}

	// first, find the schema with the highest start _before or at_ from
	i := sort.Search(len(sc.stores), func(i int) bool {
		return sc.stores[i].From > from
	})
	if i > 0 {
		i--
	} else {
		// This could happen if we get passed a sample from before 1970.
		i = 0
		from = sc.stores[0].From
	}

	// next, find the schema with the lowest start _after_ through
	j := sort.Search(len(sc.stores), func(j int) bool {
		return sc.stores[j].From > through
	})

	var stores []storeWithRange
	start := from
	for ; i < j; i++ {
		nextSchemaStarts := model.Latest
		if i+1 < len(sc.stores) {
			nextSchemaStarts = sc.stores[i+1].From
		}

		end := min(through, nextSchemaStarts-1)
		stores = append(stores, storeWithRange{
			store:   sc.stores[i].Store,
			from:    start,
			through: end,
		})

		start = nextSchemaStarts
	}

	return stores
}

type storeWithRange struct {
	store         Store
	from, through model.Time
}

// SelectSamples implements Store
func (sc *StoreCombiner) SelectSamples(ctx context.Context, req logql.SelectSampleParams) (iter.SampleIterator, error) {
	stores := sc.findStoresForTimeRange(model.TimeFromUnixNano(req.Start.UnixNano()), model.TimeFromUnixNano(req.End.UnixNano()))

	if len(stores) == 0 {
		return iter.NoopSampleIterator, nil
	}

	if len(stores) == 1 {
		return stores[0].store.SelectSamples(ctx, req)
	}

	iters := make([]iter.SampleIterator, 0, len(stores))
	for _, s := range stores {
		reqCopy := req
		reqCopy.Start = s.from.Time()
		reqCopy.End = s.through.Time()

		iter, err := s.store.SelectSamples(ctx, reqCopy)
		if err != nil {
			return nil, err
		}
		iters = append(iters, iter)
	}

	return iter.NewMergeSampleIterator(ctx, iters), nil
}

// SelectLogs implements Store
func (sc *StoreCombiner) SelectLogs(ctx context.Context, req logql.SelectLogParams) (iter.EntryIterator, error) {
	stores := sc.findStoresForTimeRange(model.TimeFromUnixNano(req.Start.UnixNano()), model.TimeFromUnixNano(req.End.UnixNano()))

	if len(stores) == 0 {
		return iter.NoopEntryIterator, nil
	}

	if len(stores) == 1 {
		return stores[0].store.SelectLogs(ctx, req)
	}

	iters := make([]iter.EntryIterator, 0, len(stores))
	for _, s := range stores {
		reqCopy := req
		reqCopy.Start = s.from.Time()
		reqCopy.End = s.through.Time()

		iter, err := s.store.SelectLogs(ctx, reqCopy)
		if err != nil {
			return nil, err
		}
		iters = append(iters, iter)
	}

	return iter.NewMergeEntryIterator(ctx, iters, req.Direction), nil
}

// SelectSeries implements Store
func (sc *StoreCombiner) SelectSeries(ctx context.Context, req logql.SelectLogParams) ([]logproto.SeriesIdentifier, error) {
	stores := sc.findStoresForTimeRange(model.TimeFromUnixNano(req.Start.UnixNano()), model.TimeFromUnixNano(req.End.UnixNano()))

	if len(stores) == 0 {
		return nil, nil
	}

	if len(stores) == 1 {
		return stores[0].store.SelectSeries(ctx, req)
	}

	// Use a map to deduplicate series across stores
	uniqueSeries := make(map[uint64]struct{})
	var result []logproto.SeriesIdentifier

	// The buffers are used by `series.Hash`.
	b := make([]byte, 0, 1024)
	var key uint64

	for _, s := range stores {
		reqCopy := req
		reqCopy.Start = s.from.Time()
		reqCopy.End = s.through.Time()

		series, err := s.store.SelectSeries(ctx, reqCopy)
		if err != nil {
			return nil, err
		}

		for _, s := range series {
			key = s.Hash(b)
			if _, ok := uniqueSeries[key]; !ok {
				result = append(result, s)
				uniqueSeries[key] = struct{}{}
			}
		}
	}

	return result, nil
}

// LabelValuesForMetricName implements Store
func (sc *StoreCombiner) LabelValuesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string, labelName string, matchers ...*labels.Matcher) ([]string, error) {
	stores := sc.findStoresForTimeRange(from, through)

	if len(stores) == 0 {
		return nil, nil
	}

	if len(stores) == 1 {
		return stores[0].store.LabelValuesForMetricName(ctx, userID, from, through, metricName, labelName, matchers...)
	}

	// Use a map to deduplicate values across stores
	valueSet := make(map[string]struct{})

	for _, s := range stores {
		values, err := s.store.LabelValuesForMetricName(ctx, userID, s.from, s.through, metricName, labelName, matchers...)
		if err != nil {
			return nil, err
		}

		for _, v := range values {
			valueSet[v] = struct{}{}
		}
	}

	result := make([]string, 0, len(valueSet))
	for v := range valueSet {
		result = append(result, v)
	}
	sort.Strings(result)
	return result, nil
}

// LabelNamesForMetricName implements Store
func (sc *StoreCombiner) LabelNamesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string, matchers ...*labels.Matcher) ([]string, error) {
	stores := sc.findStoresForTimeRange(from, through)

	if len(stores) == 0 {
		return nil, nil
	}

	if len(stores) == 1 {
		return stores[0].store.LabelNamesForMetricName(ctx, userID, from, through, metricName, matchers...)
	}

	// Use a map to deduplicate names across stores
	nameSet := make(map[string]struct{})

	for _, s := range stores {
		names, err := s.store.LabelNamesForMetricName(ctx, userID, s.from, s.through, metricName, matchers...)
		if err != nil {
			return nil, err
		}

		for _, n := range names {
			nameSet[n] = struct{}{}
		}
	}

	result := make([]string, 0, len(nameSet))
	for n := range nameSet {
		result = append(result, n)
	}
	sort.Strings(result)
	return result, nil
}

// Stats implements Store
func (sc *StoreCombiner) Stats(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) (*stats.Stats, error) {
	stores := sc.findStoresForTimeRange(from, through)

	if len(stores) == 0 {
		return &stats.Stats{}, nil
	}

	if len(stores) == 1 {
		return stores[0].store.Stats(ctx, userID, from, through, matchers...)
	}

	// Collect stats from all stores
	statsSlice := make([]*stats.Stats, 0, len(stores))
	for _, s := range stores {
		stats, err := s.store.Stats(ctx, userID, s.from, s.through, matchers...)
		if err != nil {
			return nil, err
		}
		statsSlice = append(statsSlice, stats)
	}

	// Merge all stats using the MergeStats function
	mergedStats := stats.MergeStats(statsSlice...)
	return &mergedStats, nil
}

// Volume implements Store
func (sc *StoreCombiner) Volume(ctx context.Context, userID string, from, through model.Time, limit int32, targetLabels []string, aggregateBy string, matchers ...*labels.Matcher) (*logproto.VolumeResponse, error) {
	stores := sc.findStoresForTimeRange(from, through)

	if len(stores) == 0 {
		return &logproto.VolumeResponse{}, nil
	}

	if len(stores) == 1 {
		return stores[0].store.Volume(ctx, userID, from, through, limit, targetLabels, aggregateBy, matchers...)
	}

	// Combine volumes from all stores
	volumes := make([]*logproto.VolumeResponse, 0, len(stores))

	for _, s := range stores {
		vol, err := s.store.Volume(ctx, userID, s.from, s.through, limit, targetLabels, aggregateBy, matchers...)
		if err != nil {
			return nil, err
		}
		volumes = append(volumes, vol)
	}

	// Use the seriesvolume package's Merge function to properly merge volume responses
	return seriesvolume.Merge(volumes, limit), nil
}

// GetShards implements Store
func (sc *StoreCombiner) GetShards(ctx context.Context, userID string, from, through model.Time, targetBytesPerShard uint64, predicate chunk.Predicate) (*logproto.ShardsResponse, error) {
	stores := sc.findStoresForTimeRange(from, through)

	if len(stores) == 0 {
		return &logproto.ShardsResponse{}, nil
	}

	if len(stores) == 1 {
		return stores[0].store.GetShards(ctx, userID, from, through, targetBytesPerShard, predicate)
	}

	// Combine shards from all stores
	groups := make([]*logproto.ShardsResponse, 0, len(stores))

	for _, s := range stores {
		shards, err := s.store.GetShards(ctx, userID, s.from, s.through, targetBytesPerShard, predicate)
		if err != nil {
			return nil, err
		}
		groups = append(groups, shards)
	}

	switch {
	case len(groups) == 1:
		return groups[0], nil
	case len(groups) == 0:
		return nil, nil
	default:
		sort.Slice(groups, func(i, j int) bool {
			return len(groups[i].Shards) > len(groups[j].Shards)
		})
		return groups[0], nil
	}
}
