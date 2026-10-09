package metastore

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// TableOfContentsEntry describes an index-pointer row to add to a tenant's ToC.
// Used by WriteEntry and as the "to add" set of ReplaceIndexPointers.
type TableOfContentsEntry struct {
	// Path is the object-storage path of the index object.
	Path string
	// StartTime / EndTime bound the time range covered by the index.
	StartTime time.Time
	EndTime   time.Time
}

// validate returns an error if e has no valid time range for a ToC.
func (e TableOfContentsEntry) validate() error {
	// The ToC writer fails to read back a row with a timestamp of 0, so a row
	// that starts at the Unix epoch would block every later write to its ToC.
	if !e.StartTime.After(time.Unix(0, 0)) {
		return fmt.Errorf("ToC entry %s starts at %s, not after the Unix epoch", e.Path, e.StartTime)
	}
	// An entry that ends before it starts overlaps no ToC window, so the
	// writer would write nothing for it.
	if e.EndTime.Before(e.StartTime) {
		return fmt.Errorf("ToC entry %s ends at %s, before its start at %s", e.Path, e.EndTime, e.StartTime)
	}
	return nil
}

// ReplaceIndexPointers atomically swaps a set of index pointers in the
// tenant's ToC for the given window. Every row in oldPaths is removed and
// every entry in newEntries that the ToC does not hold yet is added.
//
// Returns (true, nil) if the swap was applied.
// Returns (false, nil) if there's nothing to do, examples are:
// - both oldPaths and newEntries are empty
// - oldPaths do not exist in TOC (race-loss)
// - TOC doesn't exist
// Returns (false, error) if an error happened (including retry exhaustion),
// or if an entry in newEntries has no valid time range or does not overlap
// the window.
//
// Race-loss is detected on an ANY-match basis: if ANY oldPath is still
// present in the tenant's ToC, the swap proceeds and drops the matched subset.
// Only when ZERO oldPaths match is the call treated as a no-op. The ToC keeps
// one pointer for each path, so a new entry that a concurrent coordinator
// already added is not added again.
//
// The primitive is idempotent: re-invoking it with already-applied
// oldPaths/newEntries is a no-op. If an attempt fails after its write landed,
// for example because the response is lost, the retry finds the swap applied
// and returns (true, nil).
//
// A ToC holds one tenant. If it holds a section of another tenant,
// ReplaceIndexPointers returns an error without retrying.
//
// Callers must serialize overlapping ReplaceIndexPointers calls for the
// same tenant and window within a process. Concurrent processes racing on the
// same ToC are safe because each call goes through a fresh GetAndReplace with
// conditional-PUT semantics.
func (m *TableOfContentsWriter) ReplaceIndexPointers(
	ctx context.Context,
	window time.Time,
	tenant string,
	oldPaths []string,
	newEntries []TableOfContentsEntry,
) (bool, error) {
	switch {
	case len(oldPaths) == 0 && len(newEntries) == 0:
		return false, nil
	case len(oldPaths) == 0:
		return false, errors.New("replace-index-pointers: no old entries")
	case len(newEntries) == 0:
		return false, errors.New("replace-index-pointers: no new entries")
	}

	window = window.Truncate(MetastoreWindowSize).UTC()
	for _, e := range newEntries {
		if err := e.validate(); err != nil {
			return false, err
		}
		// An entry may extend past the window, because the data of older
		// objects can cross a window boundary. It must overlap the window.
		if e.EndTime.Before(window) || !e.StartTime.Before(window.Add(MetastoreWindowSize)) {
			return false, fmt.Errorf("ToC entry %s from %s to %s does not overlap the window at %s", e.Path, e.StartTime, e.EndTime, window.Format(time.RFC3339))
		}
	}

	remove := make(map[string]struct{}, len(oldPaths))
	for _, p := range oldPaths {
		remove[p] = struct{}{}
	}

	result, err := m.applyChange(ctx, opReplace, tenant, window, tocChange{
		remove:        remove,
		add:           newEntries,
		requireRemove: true,
	})
	return result == changeWritten, err
}
