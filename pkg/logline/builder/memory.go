package builder

import "runtime/debug"

const (
	flushOnMemoryFraction = 0.70
	// Go reports math.MaxInt64 when GOMEMLIMIT is unset; treat absurd limits as disabled.
	maxEffectiveGOMEMLIMIT = 1 << 40 // 1 TiB
)

// memoryFlushThresholdBytes returns the resident-memory byte threshold for a
// full flush: 70% of GOMEMLIMIT. Returns 0 when GOMEMLIMIT is unset.
func memoryFlushThresholdBytes() uint64 {
	limit := debug.SetMemoryLimit(-1)
	if limit <= 0 || limit > maxEffectiveGOMEMLIMIT {
		return 0
	}
	return uint64(float64(limit) * flushOnMemoryFraction)
}
