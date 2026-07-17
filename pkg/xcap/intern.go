package xcap

import (
	"fmt"
	"sync"
)

// internStatisticLimit bounds the number of distinct statistics retained in the
// intern cache. Beyond the limit, statistics are still returned correctly but
// are freshly allocated instead of cached. In practice the set of
// distinct statistics is small and stable, so the limit is never reached.
const internStatisticLimit = 512

// statisticInterner caches immutable Statistic instances keyed by their
// definition, so repeated captures referencing the same statistic reuse a single
// shared instance instead of each allocating a fresh one.
type statisticInterner struct {
	mu    sync.RWMutex
	cache map[StatisticKey]Statistic
	limit int
}

func newStatisticInterner(limit int) *statisticInterner {
	return &statisticInterner{
		cache: make(map[StatisticKey]Statistic),
		limit: limit,
	}
}

// globalStatisticInterner is the shared interner used by the unmarshal path.
var globalStatisticInterner = newStatisticInterner(internStatisticLimit)

// internStatistic returns a shared, immutable Statistic for the given
// definition from the global interner. See [statisticInterner.intern].
func internStatistic(name string, dataType DataType, aggType AggregationType) (Statistic, error) {
	return globalStatisticInterner.intern(name, dataType, aggType)
}

// intern returns a shared, immutable Statistic for the given definition. Because
// a Statistic is fully identified by its [StatisticKey] and never mutated after
// construction, a single instance can be reused across every capture that
// references the same statistic. This avoids allocating a fresh Statistic for
// each statistic in each unmarshalled capture.
func (i *statisticInterner) intern(name string, dataType DataType, aggType AggregationType) (Statistic, error) {
	key := StatisticKey{Name: name, DataType: dataType, Aggregation: aggType}

	i.mu.RLock()
	s, ok := i.cache[key]
	i.mu.RUnlock()
	if ok {
		return s, nil
	}

	built, err := buildStatistic(name, dataType, aggType)
	if err != nil {
		return nil, err
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	// Another goroutine may have cached this key while we were building.
	if existing, ok := i.cache[key]; ok {
		return existing, nil
	}
	// Bound the cache: past the limit, return the freshly built statistic
	// without caching it so memory stays bounded.
	if len(i.cache) < i.limit {
		i.cache[key] = built
	}
	return built, nil
}

// buildStatistic constructs a concrete Statistic from its definition.
func buildStatistic(name string, dataType DataType, aggType AggregationType) (Statistic, error) {
	switch dataType {
	case DataTypeInt64:
		return NewStatisticInt64(name, aggType), nil
	case DataTypeFloat64:
		return NewStatisticFloat64(name, aggType), nil
	case DataTypeBool:
		return NewStatisticFlag(name), nil
	default:
		return nil, fmt.Errorf("unsupported data type: %v", dataType)
	}
}
