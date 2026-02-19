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
}

func (s *PointersScan) MaxTimeRange() TimeRange {
	return TimeRange{s.Start, s.End}
}

func (s *PointersScan) ID() ulid.ULID { return s.NodeID }

func (s *PointersScan) Clone() Node {
	var selector Expression
	if s.Selector != nil {
		selector = s.Selector.Clone()
	}
	return &PointersScan{
		NodeID:     ulid.Make(),
		Location:   s.Location,
		Selector:   selector,
		Predicates: cloneExpressions(s.Predicates),
		Start:      s.Start,
		End:        s.End,
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
