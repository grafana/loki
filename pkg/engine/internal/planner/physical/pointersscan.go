package physical

import (
	"time"

	"github.com/oklog/ulid/v2"
)

type PointersScan struct {
	NodeID ulid.ULID

	Location DataObjLocation

	Selector   Expression
	Predicates []Expression

	Start time.Time
	End   time.Time

	// maxTimeRange is the maximum time boundary of the index (from the index pointer).
	// Used for cache key and plan time-range calculation; execution uses Start/End (query range).
	maxTimeRange TimeRange
}

func (s *PointersScan) GetMaxTimeRange() TimeRange {
	return s.maxTimeRange
}

// MaxTimeRange returns the index time range when set, otherwise the query Start/End.
func (s *PointersScan) MaxTimeRange() TimeRange {
	return TimeRange{s.Start, s.End}
}

// SetMaxTimeRange sets the maximum time boundary of the index (e.g. from proto unmarshal).
func (s *PointersScan) SetMaxTimeRange(tr TimeRange) {
	s.maxTimeRange = tr
}

func (s *PointersScan) ID() ulid.ULID { return s.NodeID }

func (s *PointersScan) Clone() Node {
	var selector Expression
	if s.Selector != nil {
		selector = s.Selector.Clone()
	}
	return &PointersScan{
		NodeID:       ulid.Make(),
		Location:     s.Location,
		Selector:     selector,
		Predicates:   cloneExpressions(s.Predicates),
		Start:        s.Start,
		End:          s.End,
		maxTimeRange: s.maxTimeRange,
	}
}

func (s *PointersScan) Type() NodeType {
	return NodeTypePointersScan
}

// TaskCacheID returns a deterministic, readable cache key string for this scan task.
// The same Location, Selector, Predicates, and time range produce the same key across
// plan instances. Callers should hash this for actual cache storage (e.g. cache.HashKey).
func (s *PointersScan) TaskCacheID() string {
	return cacheKeyStringPointersScan(s)
}

// DataObjectCacheKey returns a deterministic key scoped to the scanned object location.
// PointersScan does not carry a section index; section=-1 is used as a sentinel.
func (s *PointersScan) DataObjectCacheKey() string {
	return cacheKeyStringDataObject(s.Location, -1)
}

func (s *PointersScan) CacheableKey() string { return s.TaskCacheID() }
