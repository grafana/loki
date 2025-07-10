package tsdb

import "math"

func (h *Head) MustIndex() IndexReader {
	return h.indexRange(math.MinInt64, math.MaxInt64)
}
