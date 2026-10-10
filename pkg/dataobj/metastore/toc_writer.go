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
// that needs no write. applyChange turns them into a changeResult and returns
// no error.
var (
	// errChangePresent means the ToC already holds the change.
	errChangePresent = errors.New("ToC already holds the change")
	// errRaceLost means the change requires a removal, the ToC holds none of
	// the paths to remove, and the ToC lacks an entry to add.
	errRaceLost = errors.New("ToC holds none of the paths to remove")
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

// validate returns an error if e has no valid time range for a ToC.
func (e TableOfContentsEntry) validate() error {
	// The ToC writer fails to read back a row with a timestamp of 0, so a row
	// that starts at the Unix epoch would block every later write to its ToC.
	if !e.StartTime.After(time.Unix(0, 0)) {
		return fmt.Errorf("ToC entry %s starts at %s, not after the Unix epoch", e.Path, e.StartTime)
	}
	// The window checks of WriteEntry and ReplaceIndexPointers accept an
	// entry that ends before it starts if both ends fall in one window, so
	// without this check the ToC would hold the inverted range.
	if e.EndTime.Before(e.StartTime) {
		return fmt.Errorf("ToC entry %s ends at %s, before its start at %s", e.Path, e.EndTime, e.StartTime)
	}
	return nil
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
) *TableOfContentsWriter {
	return &TableOfContentsWriter{
		bucket:     bucket,
		backoffCfg: backoffCfg,
		builderCfg: builderCfg,
		logger:     logger,
		metrics:    metrics,
		// The writer does not register the ToC builder metrics, because the
		// index builder registers collectors with the same names.
		builderMetrics: indexobj.NewBuilderMetrics(nil),
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
	if err := entry.validate(); err != nil {
		return err
	}

	window := entry.StartTime.UTC().Truncate(MetastoreWindowSize)
	if endWindow := entry.EndTime.UTC().Truncate(MetastoreWindowSize); !endWindow.Equal(window) {
		return fmt.Errorf("entry %s spans more than one ToC window: %s to %s", entry.Path, window.Format(time.RFC3339), endWindow.Format(time.RFC3339))
	}

	_, err := m.applyChange(ctx, opWriteEntry, tenant, window, tocChange{
		add: []TableOfContentsEntry{entry},
	})
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
// an entry in newEntries has no valid time range or does not overlap the
// window, after the last attempt, or once ctx is done. If the ToC holds a
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
		remove: remove,
		add:    newEntries,
	})
	return result == changeWritten, err
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
	// changePresent means the attempt found the change already in the ToC and
	// wrote nothing. An earlier attempt of the same call may have written it.
	changePresent changeResult = "already_present"
	// changeRaceLost means the change requires a removal, the ToC held none of
	// the paths to remove, and the ToC lacked an entry to add. The attempt
	// wrote nothing.
	changeRaceLost changeResult = "race_lost"
)

// applyChange applies change to the tenant's ToC of window with a
// conditional write. It retries a failed write with the backoff config of the
// writer, and returns an error without retrying if the ToC holds a section of
// another tenant. op labels the metrics and logs.
//
// An attempt can fail after its conditional write landed, for example when
// the response is lost. The retry then finds the change in the ToC and
// returns changePresent.
func (m *TableOfContentsWriter) applyChange(ctx context.Context, op tocOp, tenant string, window time.Time, change tocChange) (changeResult, error) {
	result := changeFailed
	start := time.Now()
	defer func() {
		m.metrics.changeDurationSeconds.WithLabelValues(string(op), string(result)).Observe(time.Since(start).Seconds())
	}()

	tocPath := TableOfContentsPath(tenant, window)
	builder, err := indexobj.NewBuilder(tenant, m.builderCfg, nil, m.builderMetrics)
	if err != nil {
		return changeFailed, err
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
		case errors.Is(err, errChangePresent):
			result = changePresent
			level.Info(m.logger).Log("msg", "toc update skipped: toc already holds the change", "op", op, "tocPath", tocPath)
		case errors.Is(err, errRaceLost):
			result = changeRaceLost
			level.Info(m.logger).Log("msg", "toc update skipped: toc holds none of the paths to remove", "op", op, "tocPath", tocPath)
		default:
			// A failed attempt is often a conditional write that lost to
			// another writer, which the next attempt resolves.
			result = changeFailed
			level.Warn(m.logger).Log("msg", "toc update attempt failed", "op", op, "tocPath", tocPath, "attempt", attempts, "err", err)
		}
		m.metrics.changeAttemptSeconds.WithLabelValues(string(op), string(result)).Observe(time.Since(attemptStart).Seconds())

		if result != changeFailed || errors.Is(err, errUnrecoverable) {
			break
		}
		b.Wait()
	}

	// The loop ends with a failure on an unrecoverable error, after the last
	// attempt, or once ctx is done. ctx can be done before the first attempt,
	// when err is still nil.
	if result == changeFailed {
		err = errors.Join(b.Err(), err)
		level.Error(m.logger).Log("msg", "toc update failed", "op", op, "tocPath", tocPath, "attempts", attempts, "err", err)
		return changeFailed, err
	}
	return result, nil
}

// rebuildToc returns the ToC that results from applying change to the
// existing ToC. existing is nil or empty if the ToC does not exist yet. The
// caller owns the returned reader and must close it.
//
// rebuildToc keeps one pointer for each path, so the ToC it returns holds no
// repeated paths, even if the existing ToC does. It does not rewrite a ToC
// only to drop repeated paths, so they stay until the next change that writes.
//
// It returns errChangePresent if the existing ToC already holds the change. It
// returns errRaceLost if the change requires a removal, the existing ToC holds
// none of the paths to remove, and the existing ToC lacks an entry to add. It
// returns an error that wraps errUnrecoverable if the existing ToC holds a
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
		// another writer won the race.
		for _, entry := range change.add {
			if _, ok := kept[entry.Path]; !ok {
				return nil, errRaceLost
			}
		}
		return nil, errChangePresent
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
		return nil, errChangePresent
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
