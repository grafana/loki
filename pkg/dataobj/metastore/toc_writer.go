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
	"github.com/grafana/loki/v3/pkg/dataobj/sections/watermarks"
	"github.com/grafana/loki/v3/pkg/storage/bucket"
)

// DefaultTocBuilderConfig is the builder config for ToC objects. Its sizes are
// smaller than those for logs objects, because a ToC holds index pointers and a few watermark rows.
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
// that gets no write from this call. applyChange returns them to its caller.
var (
	// errChangeAlreadyApplied means the ToC already holds the change.
	errChangeAlreadyApplied = errors.New("ToC already holds the change")
	// errChangeSuperseded means another writer removed a path to remove first.
	errChangeSuperseded = errors.New("ToC is missing paths to remove")
)

// ErrWatermarkMismatch means the watermark of a ToC differs from the value the
// caller expected, so another process wrote the Kafka partition. Use
// [errors.Is] to detect it and [errors.As] with [*WatermarkMismatchError] to
// read the values.
var ErrWatermarkMismatch = errors.New("ToC watermark differs from the expected value")

// WatermarkMismatchError is the error that [TableOfContentsWriter.WriteEntry]
// returns when the watermark check fails. It wraps [ErrWatermarkMismatch].
type WatermarkMismatchError struct {
	Topic     string
	Partition int32
	// Expected is the value the caller passed. It is nil if the caller had
	// none.
	Expected *int64
	// Actual is the value in the ToC. It is nil if the ToC has no row for the
	// topic and partition.
	Actual *int64
}

func (e *WatermarkMismatchError) Error() string {
	return fmt.Sprintf("%s: topic %q partition %d: expected %s, ToC holds %s",
		ErrWatermarkMismatch, e.Topic, e.Partition, formatWatermark(e.Expected), formatWatermark(e.Actual))
}

// Unwrap returns [ErrWatermarkMismatch].
func (e *WatermarkMismatchError) Unwrap() error { return ErrWatermarkMismatch }

func formatWatermark(w *int64) string {
	if w == nil {
		return "no value"
	}
	return fmt.Sprintf("%d", *w)
}

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
	// Watermark, if set, raises a Kafka watermark in the same write that adds
	// the pointer.
	Watermark *WatermarkUpdate
}

// WatermarkUpdate raises the watermark of one Kafka topic and partition in a
// ToC, if the ToC holds the value the caller expects.
type WatermarkUpdate struct {
	Topic     string
	Partition int32
	// EndOffset is the highest offset of the entry's records, not the next
	// offset to read. It must not be lower than Expected.
	EndOffset int64
	// Expected is the watermark the caller expects the ToC to hold. Nil means
	// the caller has no value in memory, which matches a ToC with no row for
	// the topic and partition.
	Expected *int64
}

func (u WatermarkUpdate) validate() error {
	switch {
	case u.Topic == "":
		return errors.New("watermark update has no topic")
	case u.Partition < 0:
		return fmt.Errorf("watermark update has negative partition %d", u.Partition)
	case u.EndOffset < 0:
		return fmt.Errorf("watermark update has negative end offset %d", u.EndOffset)
	case u.Expected != nil && *u.Expected < 0:
		return fmt.Errorf("watermark update has negative expected value %d", *u.Expected)
	case u.Expected != nil && *u.Expected > u.EndOffset:
		return fmt.Errorf("watermark update has end offset %d below expected value %d", u.EndOffset, *u.Expected)
	}
	return nil
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
// If entry.Watermark is set, WriteEntry checks and raises the watermark in the
// same conditional write that adds the pointer. The check passes if the
// watermark of the topic and partition in the ToC equals Expected. A missing
// row equals a nil Expected. Rows of other topics and partitions do not
// matter. If the check fails, WriteEntry writes nothing and returns an error
// that wraps [ErrWatermarkMismatch], without retrying. A retry after a lost
// conditional write runs the check again on the new ToC.
//
// If the ToC already holds the pointer and its watermark is at or above
// EndOffset, an earlier attempt wrote the entry, so WriteEntry writes nothing
// and returns nil. If the ToC holds the pointer and a lower watermark, the
// check runs as usual, and a passing check raises the watermark without a
// second pointer. The path is a hash of the content, so the pointer already
// covers the records.
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

	change := tocChange{add: []TableOfContentsEntry{entry}}
	if entry.Watermark != nil {
		if err := entry.Watermark.validate(); err != nil {
			return fmt.Errorf("ToC entry %s: %w", entry.Path, err)
		}
		change.watermark = entry.Watermark
	}

	err = m.applyChange(ctx, opWriteEntry, tenant, window, change)
	if errors.Is(err, errChangeAlreadyApplied) {
		return nil
	}
	return err
}

// ReplaceIndexPointers swaps index pointers in the tenant's ToC of window in
// one conditional write. It removes every pointer whose path is in oldPaths
// and adds each entry of newEntries that the ToC does not hold.
//
// The swap applies only if the ToC holds every path in oldPaths. Otherwise the
// call writes nothing: no removals and no additions. A swap without every old
// path would leave the ToC with both the remaining old paths and the new
// entries that replace them, so it would point to the same data twice.
//
// ReplaceIndexPointers returns true if it wrote the swap. It returns false
// and no error if it wrote nothing:
//   - oldPaths and newEntries are both empty.
//   - The ToC already holds the swap: it holds no path in oldPaths and every
//     entry in newEntries. For example, an earlier attempt of this call wrote
//     it and lost the response.
//
// It returns an error if exactly one of oldPaths and newEntries is empty, if
// an entry in newEntries has no valid time range or lies in another window,
// after the last attempt, or once ctx is done. It returns an error without
// retrying if the ToC holds a section of another tenant, if the ToC does not
// exist, or if another writer removed a path in oldPaths first.
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
	case errors.Is(err, errChangeAlreadyApplied):
		return false, nil
	default:
		return false, err
	}
}

// tocChange is one change to a tenant's ToC. It removes the pointers whose
// path is in remove, then adds the entries of add that the ToC does not hold.
//
// If remove is not empty, the change applies only if the ToC holds every path
// in remove. Otherwise it writes nothing. If a ToC doesn't exist yet and
// remove is not empty, the change does not create it.
//
// If watermark is set, the change also applies only if the watermark check
// passes, and it raises the watermark. See [TableOfContentsWriter.WriteEntry].
type tocChange struct {
	remove    map[string]struct{}
	add       []TableOfContentsEntry
	watermark *WatermarkUpdate
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
	// changeSuperseded means another writer removed a path to remove first,
	// so the attempt wrote nothing.
	changeSuperseded changeResult = "superseded"
	// changeWatermarkMismatch means the watermark of the ToC differed from the
	// expected value, so the attempt wrote nothing and the call does not retry.
	changeWatermarkMismatch changeResult = "watermark_mismatch"
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
// or errChangeSuperseded if the ToC needed no write from this call. It
// returns a [*WatermarkMismatchError] without retrying if the watermark check
// fails.
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
			level.Info(m.logger).Log("msg", "toc update skipped: toc is missing paths to remove", "op", op, "tocPath", tocPath, "err", err)
			m.reportAttempt(op, result, attemptStart)
			return err

		case errors.Is(err, ErrWatermarkMismatch):
			result = changeWatermarkMismatch
			level.Warn(m.logger).Log("msg", "toc update skipped: toc watermark differs from the expected value", "op", op, "tocPath", tocPath, "err", err)
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

// rebuildToc returns the ToC that results from applying change to existing,
// which is nil or empty if the ToC does not exist yet. The caller owns the
// returned reader and must close it.
//
// rebuildToc copies every watermark row of existing, so a change without a
// watermark update keeps them. It returns an error that wraps
// [ErrWatermarkMismatch] if the watermark check of change fails.
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
		removed = make(map[string]struct{}, len(change.remove))
		// current is the watermark in existing for the topic and partition of
		// change.watermark. It is nil if existing has no such row.
		current *int64
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
				removed[pointer.Path] = struct{}{}
				return nil
			}
			// Keep one pointer for each path. The ToC can hold repeated paths
			// from before, and they stay until the next change that writes.
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

		for res := range watermarks.Iter(ctx, tocObject) {
			wm, err := res.Value()
			if err != nil {
				return nil, fmt.Errorf("reading watermarks: %w", err)
			}
			if err := builder.AppendWatermark(wm.Topic, wm.Partition, wm.Watermark); err != nil {
				return nil, fmt.Errorf("appending existing watermark: %w", err)
			}
			if u := change.watermark; u != nil && wm.Topic == u.Topic && wm.Partition == u.Partition {
				current = &wm.Watermark
			}
		}
	}

	if len(removed) < len(change.remove) {
		// Write nothing. An earlier attempt may have written the change and
		// lost the response.
		alreadyApplied := len(removed) == 0
		for _, entry := range change.add {
			if _, ok := kept[entry.Path]; !ok {
				alreadyApplied = false
				break
			}
		}
		if alreadyApplied {
			return nil, errChangeAlreadyApplied
		}

		return nil, fmt.Errorf("%w: %d of %d", errChangeSuperseded, len(change.remove)-len(removed), len(change.remove))
	}

	pointersHeld := true
	for _, entry := range change.add {
		if _, ok := kept[entry.Path]; !ok {
			pointersHeld = false
			break
		}
	}

	raiseWatermark := false
	if u := change.watermark; u != nil {
		// Check this first: an earlier attempt that wrote the change and lost
		// the response left a watermark above the expected value.
		if pointersHeld && current != nil && *current >= u.EndOffset {
			return nil, errChangeAlreadyApplied
		}
		if !sameWatermark(current, u.Expected) {
			return nil, &WatermarkMismatchError{Topic: u.Topic, Partition: u.Partition, Expected: u.Expected, Actual: current}
		}
		raiseWatermark = current == nil || *current < u.EndOffset
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

	// The change has no paths to remove, and the ToC already holds every
	// entry to add.
	if len(removed) == 0 && added == 0 && !raiseWatermark {
		return nil, errChangeAlreadyApplied
	}

	if u := change.watermark; u != nil {
		if err := builder.AppendWatermark(u.Topic, u.Partition, u.EndOffset); err != nil {
			return nil, fmt.Errorf("appending watermark: %w", err)
		}
	}
	return flushToc(ctx, builder)
}

// sameWatermark returns true if a and b are both nil or hold the same value.
func sameWatermark(a, b *int64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
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
