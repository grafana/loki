package metastore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/storage/bucket"
)

// DefaultTocBuilderConfig is the builder config for ToC objects. Its sizes are
// smaller than those for logs objects, because a ToC holds only index pointers.
var DefaultTocBuilderConfig = logsobj.BuilderBaseConfig{
	TargetObjectSize:  32 * 1024 * 1024,
	TargetPageSize:    4 * 1024 * 1024,
	BufferSize:        32 * 1024 * 1024, // 8x page size
	TargetSectionSize: 4 * 1024 * 1024,  // object size / 8

	SectionStripeMergeLimit: 2,
}

// DefaultTocWriterBackoffConfig is the retry policy of a ToC change. It makes
// at most 10 attempts.
var DefaultTocWriterBackoffConfig = backoff.Config{
	MinBackoff: 50 * time.Millisecond,
	MaxBackoff: 10 * time.Second,
	MaxRetries: 10,
}

// errUnrecoverable marks a ToC error that retrying can't fix, for example a
// ToC that holds a section of another tenant.
var errUnrecoverable = errors.New("unrecoverable ToC error")

// rebuildToc returns these errors to cancel the conditional write of a ToC
// that needs no write from this call. applyChange returns them to its caller,
// which treats both as a change that wrote nothing.
var (
	// errChangeAlreadyApplied means the ToC already holds the change.
	errChangeAlreadyApplied = errors.New("ToC already holds the change")
	// errChangeSuperseded means another writer replaced the paths to remove
	// first: the ToC holds none of them, and it lacks an entry to add.
	errChangeSuperseded = errors.New("ToC holds none of the paths to remove")
)

// checkTenant returns an error that wraps errUnrecoverable unless tenant is
// the only tenant of tocObject.
func checkTenant(tocObject *dataobj.Object, tenant string) error {
	tocObjTenant, err := tocObject.Tenant()
	if err != nil {
		return fmt.Errorf("%w: %w", errUnrecoverable, err)
	}
	if tenant != tocObjTenant {
		return fmt.Errorf("%w: ToC holds tenant %q, want %q", errUnrecoverable, tocObjTenant, tenant)
	}

	return nil
}

// TableOfContentsEntry describes an index-pointer row to add to a tenant's ToC.
type TableOfContentsEntry struct {
	// Path is the object-storage path of the index object.
	Path string
	// StartTime and EndTime bound the time range covered by the index. Both
	// bounds are inclusive. StartTime must be after the Unix epoch.
	StartTime time.Time
	EndTime   time.Time
}

// TimeWindow returns the time of the time window this entry belongs to.
func (e TableOfContentsEntry) TimeWindow() (time.Time, error) {
	// The ToC writer fails to read back a row with a timestamp of 0, so a row
	// that starts at the Unix epoch would block every later write to its ToC.
	if !e.StartTime.After(time.Unix(0, 0)) {
		return time.Time{}, fmt.Errorf("ToC entry %s starts at %s, not after the Unix epoch", e.Path, e.StartTime)
	}

	if e.EndTime.Before(e.StartTime) {
		return time.Time{}, fmt.Errorf("ToC entry %s ends at %s, before its start at %s", e.Path, e.EndTime, e.StartTime)
	}

	window := e.StartTime.UTC().Truncate(MetastoreWindowSize)
	if endWindow := e.EndTime.UTC().Truncate(MetastoreWindowSize); !endWindow.Equal(window) {
		return time.Time{}, fmt.Errorf("entry %s spans more than one ToC window: %s to %s", e.Path, window.Format(time.RFC3339), endWindow.Format(time.RFC3339))
	}

	return window, nil
}

// TableOfContentsWriter (ToC writer) manages the metastore's Table of Contents files, which are a list of other
// index data objects in storage for a particular tenant and time range.
//
// TableOfContentsWriter is safe for concurrent use.
type TableOfContentsWriter struct {
	bucket         objstore.Bucket
	backoffCfg     backoff.Config
	builderCfg     logsobj.BuilderBaseConfig
	logger         log.Logger
	metrics        *TocWriterMetrics
	builderMetrics *indexobj.BuilderMetrics
}

// NewTableOfContentsWriter returns a ToC writer for bucket. It retries a failed
// change with backoffCfg and builds ToC objects with builderCfg. metrics must
// not be nil.
func NewTableOfContentsWriter(
	bucket objstore.Bucket,
	backoffCfg backoff.Config,
	builderCfg logsobj.BuilderBaseConfig,
	logger log.Logger,
	metrics *TocWriterMetrics,
	builderMetrics *indexobj.BuilderMetrics,
) *TableOfContentsWriter {
	return &TableOfContentsWriter{
		bucket:         bucket,
		backoffCfg:     backoffCfg,
		builderCfg:     builderCfg,
		logger:         logger,
		metrics:        metrics,
		builderMetrics: builderMetrics,
	}
}

// WriteEntry adds entry to the tenant's ToC of the window that holds entry.
//
// WriteEntry leaves the ToC unchanged if it already holds a pointer with
// entry.Path. It compares the path only: the path of an index object is a
// hash of its content, so the same path means the same pointer.
//
// WriteEntry retries a failed write with the backoff config of the writer, and
// returns an error after the last attempt or once ctx is done. It returns an
// error without retrying if entry fails validation, entry spans more than one
// window, or the ToC holds a section of another tenant.
func (m *TableOfContentsWriter) WriteEntry(ctx context.Context, tenant string, entry TableOfContentsEntry) error {
	window, err := entry.TimeWindow()
	if err != nil {
		return err
	}

	err = m.applyChange(ctx, opWriteEntry, tenant, window, tocChange{
		add: []TableOfContentsEntry{entry},
	})
	if errors.Is(err, errChangeAlreadyApplied) {
		return nil
	}
	return err
}

// ReplaceIndexPointers swaps index pointers in the tenant's ToC of window in
// one conditional write. It removes every pointer whose path is in oldPaths
// and adds each entry of newEntries that the ToC does not hold.
//
// The swap applies if the ToC holds at least one path in oldPaths. It then
// removes the paths it finds and ignores the others. If the ToC holds no path
// in oldPaths, the call writes nothing.
//
// ReplaceIndexPointers returns true if it wrote the swap. It returns false
// and no error if it wrote nothing:
//   - oldPaths and newEntries are both empty.
//   - The ToC does not exist.
//   - The ToC holds no path in oldPaths. This includes a ToC that already
//     holds the swap, for example because an earlier attempt of this call
//     wrote it and lost the response.
//
// It returns an error if exactly one of oldPaths and newEntries is empty, if
// an entry in newEntries has no valid time range or lies in another window,
// after the last attempt, or once ctx is done. If the ToC holds a
// section of another tenant, it returns an error without retrying.
//
// Concurrent calls for the same ToC are safe on a bucket whose GetAndReplace
// is a conditional write: a call that loses the race retries on the new ToC.
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
		entryWindow, err := e.TimeWindow()
		if err != nil {
			return false, err
		}
		// An entry outside the window would put a pointer in a ToC whose window
		// does not hold the entry's data.
		if !entryWindow.Equal(window) {
			return false, fmt.Errorf(
				"want window: %s, got ToC entry %s at %s",
				window.Format(time.RFC3339),
				e.Path,
				entryWindow.Format(time.RFC3339),
			)
		}
	}

	remove := make(map[string]struct{}, len(oldPaths))
	for _, p := range oldPaths {
		remove[p] = struct{}{}
	}

	err := m.applyChange(ctx, opReplace, tenant, window, tocChange{
		remove: remove,
		add:    newEntries,
	})
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, errChangeAlreadyApplied), errors.Is(err, errChangeSuperseded):
		return false, nil
	default:
		return false, err
	}
}

// tocChange is one change to a tenant's ToC. It removes the pointers whose
// path is in remove, then adds the entries of add that the ToC does not hold.
//
// If remove is not empty, the change applies only if the ToC holds a path in
// remove. A missing ToC holds no path, so such a change does not create it.
type tocChange struct {
	remove map[string]struct{}
	add    []TableOfContentsEntry
}

// changeResult is the result of one attempt of applyChange, and of the whole
// call. Its values are the result label values of the ToC writer metrics, so
// renaming one changes the metric series.
type changeResult string

const (
	// changeFailed means the attempt or the call failed.
	changeFailed changeResult = "failed"
	// changeWritten means the attempt wrote the change to the ToC.
	changeWritten changeResult = "written"
	// changeAlreadyApplied means the ToC already held the change, so the
	// attempt wrote nothing. An earlier attempt of the same call may have
	// written it.
	changeAlreadyApplied changeResult = "already_applied"
	// changeSuperseded means another writer replaced the paths to remove
	// first: the ToC held none of them, and it lacked an entry to add. The
	// attempt wrote nothing, so the new index objects are not referenced.
	changeSuperseded changeResult = "superseded"
	// changeConflict means the attempt lost its conditional write to another
	// writer and wrote nothing. Only an attempt has this result.
	changeConflict changeResult = "conflict"
	// changeCancelled means ctx was canceled before the call finished. Only a
	// call has this result.
	changeCancelled changeResult = "cancelled"
)

// applyChange applies change to the tenant's ToC of window with a
// conditional write. It retries a failed write with the backoff config of the
// writer, and returns an error without retrying if the ToC holds a section of
// another tenant. op labels the metrics and logs.
//
// It returns nil if it wrote the change. It returns errChangeAlreadyApplied
// or errChangeSuperseded if the ToC needed no write from this call.
func (m *TableOfContentsWriter) applyChange(ctx context.Context, op tocOp, tenant string, window time.Time, change tocChange) error {
	result := changeFailed
	start := time.Now()
	defer func() {
		m.metrics.writeSeconds.WithLabelValues(string(op), string(result)).Observe(time.Since(start).Seconds())
	}()

	tocPath := TableOfContentsPath(tenant, window)
	builder, err := indexobj.NewBuilder(tenant, m.builderCfg, nil, m.builderMetrics)
	if err != nil {
		return err
	}

	var attempts int
	b := backoff.New(ctx, m.backoffCfg)
	for b.Ongoing() {
		attempts++
		attemptStart := time.Now()

		err = m.bucket.GetAndReplace(ctx, tocPath, func(existing io.ReadCloser) (io.ReadCloser, error) {
			if existing != nil {
				defer existing.Close()
			}
			return m.rebuildToc(ctx, builder, existing, change)
		})

		switch {
		case err == nil:
			result = changeWritten
			level.Info(m.logger).Log("msg", "toc updated", "op", op, "tocPath", tocPath)
			m.reportAttempt(op, result, attemptStart)
			return nil

		case errors.Is(err, errChangeAlreadyApplied):
			result = changeAlreadyApplied
			level.Info(m.logger).Log("msg", "toc update skipped: toc already holds the change", "op", op, "tocPath", tocPath)
			m.reportAttempt(op, result, attemptStart)
			return err

		case errors.Is(err, errChangeSuperseded):
			result = changeSuperseded
			level.Info(m.logger).Log("msg", "toc update skipped: toc holds none of the paths to remove", "op", op, "tocPath", tocPath)
			m.reportAttempt(op, result, attemptStart)
			return err

		case errors.Is(err, errUnrecoverable):
			result = changeFailed
			level.Error(m.logger).Log("msg", "toc update attempt failed unrecoverably", "op", op, "tocPath", tocPath, "attempt", attempts, "err", err)
			m.reportAttempt(op, result, attemptStart)
			return err

		case bucket.IsConditionalWriteConflict(err):
			result = changeConflict
			level.Debug(m.logger).Log("msg", "toc update attempt lost a conditional write", "op", op, "tocPath", tocPath, "attempt", attempts)
			m.reportAttempt(op, result, attemptStart)
			b.Wait()
			continue

		default:
			result = changeFailed
			level.Warn(m.logger).Log("msg", "toc update attempt failed, retrying", "op", op, "tocPath", tocPath, "attempt", attempts, "err", err)
			m.reportAttempt(op, result, attemptStart)
			b.Wait()
			continue
		}
	}

	if errors.Is(b.Err(), context.Canceled) {
		result = changeCancelled
		level.Info(m.logger).Log("msg", "toc update cancelled", "op", op, "tocPath", tocPath, "attempt", attempts, "err", b.Err())
		return b.Err()
	}

	result = changeFailed
	finalErr := fmt.Errorf("gave up after all retries: %w", errors.Join(err, b.Err()))
	level.Error(m.logger).Log("msg", "toc update failed, gave up", "op", op, "tocPath", tocPath, "attempts", attempts, "err", finalErr)
	return finalErr
}

func (m *TableOfContentsWriter) reportAttempt(op tocOp, result changeResult, since time.Time) {
	m.metrics.writeAttemptSeconds.WithLabelValues(string(op), string(result)).Observe(time.Since(since).Seconds())
}

// rebuildToc returns the ToC that results from applying change to the
// existing ToC. existing is nil or empty if the ToC does not exist yet. The
// caller owns the returned reader and must close it.
//
// rebuildToc keeps one pointer for each path, so the ToC it returns holds no
// repeated paths, even if the existing ToC does. It does not rewrite a ToC
// only to drop repeated paths, so they stay until the next change that writes.
//
// It returns errChangeAlreadyApplied if the existing ToC already holds the
// change. It returns errChangeSuperseded if the change requires a removal, the
// existing ToC holds none of the paths to remove, and it lacks an entry to add.
// It returns an error that wraps errUnrecoverable if the existing ToC holds a
// section of another tenant.
func (m *TableOfContentsWriter) rebuildToc(ctx context.Context, builder *indexobj.Builder, existing io.Reader, change tocChange) (io.ReadCloser, error) {
	builder.Reset()

	var buf bytes.Buffer
	if existing != nil {
		if _, err := buf.ReadFrom(existing); err != nil {
			return nil, fmt.Errorf("reading existing ToC: %w", err)
		}
	}

	var (
		kept    = make(map[string]struct{})
		removed int
	)
	if buf.Len() > 0 {
		tocObject, err := dataobj.FromReaderAt(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
		if err != nil {
			return nil, fmt.Errorf("parsing existing ToC: %w", err)
		}
		if err := checkTenant(tocObject, builder.Tenant()); err != nil {
			return nil, err
		}

		err = forEachTocPointer(ctx, tocObject, func(pointer indexpointers.IndexPointer) error {
			if _, ok := change.remove[pointer.Path]; ok {
				removed++
				return nil
			}
			if _, ok := kept[pointer.Path]; ok {
				return nil
			}
			kept[pointer.Path] = struct{}{}
			if err := builder.AppendIndexPointer(pointer); err != nil {
				return fmt.Errorf("appending existing index pointer: %w", err)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	if len(change.remove) > 0 && removed == 0 {
		// Write nothing, because no path to remove is left. If the ToC holds
		// every entry to add, an earlier swap applied the change. Otherwise
		// another writer superseded it.
		for _, entry := range change.add {
			if _, ok := kept[entry.Path]; !ok {
				return nil, errChangeSuperseded
			}
		}
		return nil, errChangeAlreadyApplied
	}

	var added int
	for _, entry := range change.add {
		if _, ok := kept[entry.Path]; ok {
			continue
		}
		kept[entry.Path] = struct{}{}
		added++
		if err := builder.AppendIndexPointer(indexpointers.IndexPointer{
			Path:    entry.Path,
			StartTs: entry.StartTime,
			EndTs:   entry.EndTime,
		}); err != nil {
			return nil, fmt.Errorf("appending index pointer: %w", err)
		}
	}

	if removed == 0 && added == 0 {
		return nil, errChangeAlreadyApplied
	}
	return flushToc(ctx, builder)
}

// forEachTocPointer calls fn for every pointer of every index pointers
// section of tocObject. It stops at the first error.
func forEachTocPointer(ctx context.Context, tocObject *dataobj.Object, fn func(indexpointers.IndexPointer) error) error {
	var reader indexpointers.RowReader
	defer reader.Close()

	buf := make([]indexpointers.IndexPointer, 256)
	for _, section := range tocObject.Sections().Filter(indexpointers.CheckSection) {
		sec, err := indexpointers.Open(ctx, section)
		if err != nil {
			return fmt.Errorf("opening section: %w", err)
		}
		reader.Reset(sec)
		if err := reader.Open(ctx); err != nil {
			return fmt.Errorf("opening index pointers reader: %w", err)
		}
		for {
			n, err := reader.Read(ctx, buf)
			if err != nil && !errors.Is(err, io.EOF) {
				return fmt.Errorf("reading index pointers: %w", err)
			}
			for _, pointer := range buf[:n] {
				if err := fn(pointer); err != nil {
					return err
				}
			}
			if errors.Is(err, io.EOF) {
				break
			}
		}
	}
	return nil
}

// flushToc flushes builder. On success the caller owns the returned reader
// and must close it. If an error is returned the reader is nil.
func flushToc(ctx context.Context, builder *indexobj.Builder) (io.ReadCloser, error) {
	obj, closer, err := builder.Flush()
	if err != nil {
		return nil, fmt.Errorf("flushing metastore builder: %w", err)
	}

	reader, err := obj.Reader(ctx)
	if err != nil {
		return nil, errors.Join(err, closer.Close())
	}

	return &wrappedReadCloser{
		rc: reader,
		OnClose: func() error {
			// We must close our object reader before closing the object
			// itself.
			return errors.Join(reader.Close(), closer.Close())
		},
	}, nil
}

// wrappedReadCloser calls OnClose on Close if OnClose is set, and closes rc
// otherwise.
type wrappedReadCloser struct {
	rc      io.ReadCloser
	OnClose func() error
}

func (w *wrappedReadCloser) Read(p []byte) (int, error) {
	return w.rc.Read(p)
}

func (w *wrappedReadCloser) Close() error {
	if w.OnClose != nil {
		return w.OnClose()
	}
	return w.rc.Close()
}
