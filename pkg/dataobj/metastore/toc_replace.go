package metastore

import (
	"bytes"
	"context"
	stderrors "errors"
	"io"
	"time"

	"github.com/grafana/dskit/backoff"
	"github.com/pkg/errors"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
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

// replaceBackoffConfig bounds ReplaceIndexPointers retries on conditional-write
// rejection (412 PreconditionFailed) or transient bucket errors.
var replaceBackoffConfig = backoff.Config{
	MinBackoff: 50 * time.Millisecond,
	MaxBackoff: 10 * time.Second,
	MaxRetries: 10,
}

// errReplaceNoOp is a sentinel returned from the GetAndReplace callback to
// signal "do not write". It is consumed by replaceIndexPointers and converted
// to (false, nil); it never escapes the package.
var errReplaceNoOp = stderrors.New("replace-index-pointers: no-op")

// ReplaceIndexPointers atomically swaps a set of index pointers in the
// ToC for the given window. For the target tenant, every row in oldPaths
// is removed and every entry in newEntries is added; all other tenants'
// entries are preserved unchanged.
//
// Returns (true, nil) if the swap was applied, (false, nil) if no oldPaths
// were still present in the target tenant's current section (race-loss /
// already-converged case, or the ToC is missing entirely), or (false, err)
// on any error including retry exhaustion.
//
// Race-loss is detected on an ANY-match basis: if ANY oldPath is still
// present in the target tenant's current section, the swap proceeds and
// drops the matched subset (leaving non-matched oldPaths' replacements, if
// they were already swapped in by a concurrent coordinator, untouched).
// Only when ZERO oldPaths match is the call treated as a no-op. Callers
// orchestrating per-cycle plans against a single ToC snapshot will see
// all-or-nothing matches in practice; partial overlaps can only occur in
// rare cross-cycle races and produce bounded duplicate index entries that
// the next cycle's plan will re-merge.
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
) (bool, error) {
	// Deterministic fast-path: with no oldPaths to remove, the call would always
	// converge to a sentinel-aborted no-op inside the callback. Returning here
	// makes the no-op behavior independent of bucket availability so callers can
	// rely on (false, nil) without error-handling for transient storage issues.
	if len(oldPaths) == 0 {
		return false, nil
	}
	return m.replaceIndexPointers(ctx, window, tenant, oldPaths, newEntries, replaceBackoffConfig)
}

// replaceIndexPointers is the internal entrypoint that accepts an explicit
// backoff config, exposed only to same-package tests for retry-exhaustion
// coverage. Production callers go through ReplaceIndexPointers.
func (m *TableOfContentsWriter) replaceIndexPointers(
	ctx context.Context,
	window time.Time,
	tenant string,
	oldPaths []string,
	newEntries []TableOfContentsEntry,
	backoffCfg backoff.Config,
) (bool, error) {
	tocPath := TableOfContentsPath(window.Truncate(MetastoreWindowSize).UTC())

	oldSet := make(map[string]struct{}, len(oldPaths))
	for _, p := range oldPaths {
		oldSet[p] = struct{}{}
	}

	var (
		swapped bool
		lastErr error
	)
	b := backoff.New(ctx, backoffCfg)
	for b.Ongoing() {
		// Reset across retries — each attempt is independent.
		swapped = false

		err := m.bucket.GetAndReplace(ctx, tocPath, func(existing io.ReadCloser) (io.ReadCloser, error) {
			// Drain `existing` fully up-front so we never return a drained reader.
			var existingBytes []byte
			if existing != nil {
				defer existing.Close()
				var rerr error
				existingBytes, rerr = io.ReadAll(existing)
				if rerr != nil {
					return nil, errors.Wrap(rerr, "reading existing ToC")
				}
			}

			// Missing or empty ToC: nothing to remove → idempotent no-op.
			// Abort the conditional PUT entirely so we don't materialize an empty
			// object at tocPath. The outer loop converts this sentinel to (false, nil).
			if len(existingBytes) == 0 {
				return nil, errReplaceNoOp
			}

			obj, oerr := dataobj.FromReaderAt(bytes.NewReader(existingBytes), int64(len(existingBytes)))
			if oerr != nil {
				return nil, errors.Wrap(oerr, "parsing existing ToC")
			}

			// Pass 1: detect whether any oldPaths are still present in the target tenant.
			anyMatched, scanErr := scanForMatches(ctx, obj, tenant, oldSet)
			if scanErr != nil {
				return nil, scanErr
			}
			if !anyMatched {
				// Race-loss / already-converged: do not modify the ToC.
				// Returning the sentinel aborts the conditional PUT without
				// writing a no-op blob. See the missing-ToC branch above.
				return nil, errReplaceNoOp
			}

			// Pass 2: rebuild ToC, dropping target tenant's oldPaths and appending newEntries.
			builder, berr := indexobj.NewBuilder(tocBuilderCfg, nil)
			if berr != nil {
				return nil, errors.Wrap(berr, "creating ToC builder")
			}

			if err := replayFiltered(ctx, obj, builder, tenant, oldSet); err != nil {
				return nil, err
			}

			for _, e := range newEntries {
				if err := builder.AppendIndexPointer(tenant, e.Path, e.StartTime, e.EndTime); err != nil {
					return nil, errors.Wrap(err, "appending new ToC entry")
				}
			}

			newObj, closer, ferr := builder.Flush()
			if ferr != nil {
				// Full drain: every row in every section was filtered out and
				// no newEntries were appended, so the rebuilt ToC would be
				// empty. Treat this as a successful swap and abort the
				// conditional PUT — leaving the existing ToC in place is
				// acceptable here because the original entries are gone in
				// the caller's view and a future cycle will replace the ToC
				// when fresh content arrives. A full-drain ToC delete would
				// be a separate primitive.
				if stderrors.Is(ferr, indexobj.ErrBuilderEmpty) {
					swapped = true
					return nil, errReplaceNoOp
				}
				return nil, errors.Wrap(ferr, "flushing rebuilt ToC")
			}
			reader, rerr := newObj.Reader(ctx)
			if rerr != nil {
				_ = closer.Close()
				return nil, errors.Wrap(rerr, "opening rebuilt ToC reader")
			}
			newBytes, rerr := io.ReadAll(reader)
			closeErr := stderrors.Join(reader.Close(), closer.Close())
			if rerr != nil {
				return nil, errors.Wrap(rerr, "reading rebuilt ToC")
			}
			if closeErr != nil {
				return nil, errors.Wrap(closeErr, "closing rebuilt ToC")
			}
			swapped = true
			return objstore.NopCloserWithSize(bytes.NewReader(newBytes)), nil
		})

		if err == nil {
			return swapped, nil
		}
		if stderrors.Is(err, errReplaceNoOp) {
			// swapped is true only in the full-drain path; all other sentinel
			// callers (missing ToC, zero-match race-loss) leave it false.
			return swapped, nil
		}
		lastErr = err
		b.Wait()
	}
	if lastErr == nil {
		lastErr = b.Err()
	}
	return false, lastErr
}

// scanForMatches reports whether any row in any section of obj is owned by
// the target tenant AND has a path present in oldSet.
func scanForMatches(ctx context.Context, obj *dataobj.Object, tenant string, oldSet map[string]struct{}) (bool, error) {
	var reader indexpointers.RowReader
	defer reader.Close()
	buf := make([]indexpointers.IndexPointer, 256)
	for _, section := range obj.Sections().Filter(indexpointers.CheckSection) {
		if section.Tenant != tenant {
			continue
		}
		sec, err := indexpointers.Open(ctx, section)
		if err != nil {
			return false, errors.Wrap(err, "opening section")
		}
		reader.Reset(sec)
		if err := reader.Open(ctx); err != nil {
			return false, errors.Wrap(err, "opening row reader")
		}
		for {
			n, err := reader.Read(ctx, buf)
			for i := 0; i < n; i++ {
				if _, ok := oldSet[buf[i].Path]; ok {
					return true, nil
				}
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				return false, errors.Wrap(err, "reading rows")
			}
			if n == 0 {
				break
			}
		}
	}
	return false, nil
}

// replayFiltered replays every row from every section of obj into builder,
// EXCEPT rows belonging to the target tenant whose path is in oldSet.
func replayFiltered(ctx context.Context, obj *dataobj.Object, builder *indexobj.Builder, tenant string, oldSet map[string]struct{}) error {
	var reader indexpointers.RowReader
	defer reader.Close()
	buf := make([]indexpointers.IndexPointer, 256)
	for _, section := range obj.Sections().Filter(indexpointers.CheckSection) {
		sec, err := indexpointers.Open(ctx, section)
		if err != nil {
			return errors.Wrap(err, "opening section")
		}
		sectionTenant := section.Tenant
		reader.Reset(sec)
		if err := reader.Open(ctx); err != nil {
			return errors.Wrap(err, "opening row reader")
		}
		for {
			n, err := reader.Read(ctx, buf)
			for i := 0; i < n; i++ {
				if sectionTenant == tenant {
					if _, drop := oldSet[buf[i].Path]; drop {
						continue
					}
				}
				if aerr := builder.AppendIndexPointer(sectionTenant, buf[i].Path, buf[i].StartTs, buf[i].EndTs); aerr != nil {
					return errors.Wrap(aerr, "replaying index pointer")
				}
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				return errors.Wrap(err, "reading rows")
			}
			if n == 0 {
				break
			}
		}
	}
	return nil
}
