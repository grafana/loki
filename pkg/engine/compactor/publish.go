package compactor

import (
	"context"
	"time"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
)

// tocReplacer is the subset of *metastore.TableOfContentsWriter the
// tocPublisher needs.
type tocReplacer interface {
	ReplaceIndexPointers(
		ctx context.Context,
		window time.Time,
		tenant string,
		oldPaths []string,
		newEntries []metastore.TableOfContentsEntry,
	) (bool, error)
}

// tocPublisher swaps compaction outputs into a window's ToC.
type tocPublisher struct {
	writer  tocReplacer
	timeout time.Duration
	dryRun  bool
}

// Replace atomically replaces oldPaths with newEntries in the tenant's ToC
// for window. It returns true only when this call applied the swap.
//
// The swap applies only if the ToC holds every path in oldPaths. Otherwise it
// writes nothing.
//
// Replace returns false without an error in these cases:
//   - Replace runs in dry-run mode.
//   - The ToC holds none of oldPaths. A concurrent writer replaced oldPaths
//     first, or the ToC already holds the swap, for example because an
//     earlier attempt wrote it and lost the response.
//   - The ToC holds some but not all of oldPaths. A concurrent writer removed
//     the others first.
//
// Callers treat every false as no progress.
//
// The timeout applies to the swap only. A DeadlineExceeded error from it does
// not mean the parent ctx is done.
func (p *tocPublisher) Replace(
	ctx context.Context,
	tenant string,
	window time.Time,
	oldPaths []string,
	newEntries []metastore.TableOfContentsEntry,
) (bool, error) {
	if p.dryRun {
		return false, nil
	}

	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	return p.writer.ReplaceIndexPointers(ctx, window, tenant, oldPaths, newEntries)
}
