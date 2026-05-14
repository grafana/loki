package metastore

import (
	"context"
	"time"
)

// TableOfContentsEntry describes an index-pointer row to add to a ToC for a
// given tenant. Used by ReplaceIndexPointers as the "to add" set.
type TableOfContentsEntry struct {
	// Path is the object-storage path of the index object.
	Path string
	// StartTime / EndTime bound the time range covered by the index.
	StartTime time.Time
	EndTime   time.Time
}

// ReplaceIndexPointers atomically swaps a set of index pointers in the
// ToC for the given window. For the target tenant, every row in oldPaths
// is removed and every entry in newEntries is added; all other tenants'
// sections are preserved byte-equivalent.
//
// Returns (true, nil) if the swap was applied, (false, nil) if no oldPaths
// were still present in the target tenant's current section (race-loss /
// already-converged case), or (false, err) on any error including retry
// exhaustion.
//
// The primitive is idempotent: re-invoking it with already-applied
// oldPaths/newEntries is a no-op.
//
// Callers must serialize overlapping ReplaceIndexPointers calls for the
// same window within a process; the method allocates per-call state but
// does not coordinate across goroutines. Concurrent processes racing on
// the same window are safe because each call goes through a fresh
// GetAndReplace with conditional-PUT semantics.
func (m *TableOfContentsWriter) ReplaceIndexPointers(
	ctx context.Context,
	window time.Time,
	tenant string,
	oldPaths []string,
	newEntries []TableOfContentsEntry,
) (swapped bool, err error) {
	// TODO: implement.
	return false, nil
}
