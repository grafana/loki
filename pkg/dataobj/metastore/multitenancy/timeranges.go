package multitenancy

import (
	"iter"
	"time"
)

type TimeRange struct {
	MinTime time.Time
	MaxTime time.Time
}

type TimeRangesIterator interface {
	Iter() iter.Seq2[string, TimeRange]
}

type TimeRangeSet map[string]TimeRange

func (t TimeRangeSet) Iter() iter.Seq2[string, TimeRange] {
	return func(yield func(tenant string, timeRange TimeRange) bool) {
		for tenant, timeRange := range t {
			if !yield(tenant, timeRange) {
				return
			}
		}
	}
}
