package compactor

// Limits is the per-tenant configuration the compaction coordinator consults.
// *github.com/grafana/loki/v3/pkg/validation.Overrides satisfies it.
type Limits interface {
	// CompactionPhases reports which compaction phases run for the tenant.
	// runIndex is true when either index or log compaction is enabled; runLog
	// is true only when log compaction is enabled. The invalid combination
	// (log without index) is rejected at config validation.
	CompactionPhases(userID string) (runIndex, runLog bool)

	// DeletionSweepEnabled reports whether the deletion sweep runs for the
	// tenant.
	DeletionSweepEnabled(userID string) bool

	// MaxLogsRunSizeUncompressedBytes returns the exclusive upper bound of the
	// uncompressed size of a run that log compaction writes for the tenant,
	// in bytes. Log compaction merges a group of runs only if their summed
	// size is below it. 0 means no maximum.
	MaxLogsRunSizeUncompressedBytes(userID string) uint64
}
