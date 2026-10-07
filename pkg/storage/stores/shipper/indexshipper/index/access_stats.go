package index

import (
	"context"

	"go.uber.org/atomic"
)

// Tiers reported by AccessStats.
const (
	// AccessTierMemory means every file the request read was held in memory.
	AccessTierMemory = "memory"
	// AccessTierDisk means the request read at least one file from disk.
	AccessTierDisk = "disk"
	// AccessTierOnDemand means the request downloaded index files, or waited
	// for a download that was in flight.
	AccessTierOnDemand = "on_demand"
	// AccessTierNone means the request read no index files.
	AccessTierNone = "none"
)

type accessStatsKey struct{}

// AccessStats records, for one request, which tier each index file it read
// was served from and whether it had to wait for index files to be
// downloaded. It is safe for concurrent use.
type AccessStats struct {
	memory   atomic.Int64
	disk     atomic.Int64
	onDemand atomic.Bool
}

// NewContextWithAccessStats returns a context carrying a new AccessStats,
// which RecordFileAccess and RecordOnDemand record into.
func NewContextWithAccessStats(ctx context.Context) (context.Context, *AccessStats) {
	s := &AccessStats{}
	return context.WithValue(ctx, accessStatsKey{}, s), s
}

func accessStatsFromContext(ctx context.Context) *AccessStats {
	s, _ := ctx.Value(accessStatsKey{}).(*AccessStats)
	return s
}

// RecordFileAccess records that the request read one index file from tier.
// Tiers other than AccessTierMemory count as AccessTierDisk. It does nothing
// if ctx carries no AccessStats.
func RecordFileAccess(ctx context.Context, tier string) {
	s := accessStatsFromContext(ctx)
	if s == nil {
		return
	}
	if tier == AccessTierMemory {
		s.memory.Inc()
	} else {
		s.disk.Inc()
	}
}

// RecordOnDemand records that the request downloaded index files, or waited
// for a download in flight. It does nothing if ctx carries no AccessStats.
func RecordOnDemand(ctx context.Context) {
	if s := accessStatsFromContext(ctx); s != nil {
		s.onDemand.Store(true)
	}
}

// RequestTier returns the slowest tier the request touched: AccessTierOnDemand,
// then AccessTierDisk, then AccessTierMemory, or AccessTierNone if it read no
// files.
func (s *AccessStats) RequestTier() string {
	switch {
	case s.onDemand.Load():
		return AccessTierOnDemand
	case s.disk.Load() > 0:
		return AccessTierDisk
	case s.memory.Load() > 0:
		return AccessTierMemory
	default:
		return AccessTierNone
	}
}

// FileAccesses returns the number of file reads recorded per tier.
func (s *AccessStats) FileAccesses() (memory, disk int64) {
	return s.memory.Load(), s.disk.Load()
}
