package metastore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
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

// DefaultTocBuilderConfig is the builder config for ToC objects. It is smaller
// than the config for logs objects, because a ToC holds only index pointers.
var DefaultTocBuilderConfig = logsobj.BuilderBaseConfig{
	TargetObjectSize:  32 * 1024 * 1024,
	TargetPageSize:    4 * 1024 * 1024,
	BufferSize:        32 * 1024 * 1024, // 8x page size
	TargetSectionSize: 4 * 1024 * 1024,  // object size / 8

	SectionStripeMergeLimit: 2,
}

// DefaultTocWriterBackoffConfig is the retry policy of a ToC change. It gives
// up after 10 retries.
var DefaultTocWriterBackoffConfig = backoff.Config{
	MinBackoff: 50 * time.Millisecond,
	MaxBackoff: 10 * time.Second,
	MaxRetries: 10,
}

// errUnrecoverable marks a ToC error that retrying can't fix, for example a
// ToC that holds a section of another tenant.
var errUnrecoverable = errors.New("unrecoverable ToC error")

// The GetAndReplace callback returns these errors to cancel the write of a
// ToC that needs no change. applyChange does not return them.
var (
	// errChangePresent means the ToC already holds the change.
	errChangePresent = errors.New("ToC already holds the change")
	// errRaceLost means the change requires a removal, and the ToC holds none
	// of the paths to remove.
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

	// buffers holds *bytes.Buffer values that hold an existing ToC during a
	// change. A ToC can grow to tens of MiB, so the writer reuses them.
	buffers sync.Pool
}

// NewTableOfContentsWriter creates a new Writer for adding entries to the
// metastore's Table of Contents files. It retries a failed write with
// backoffCfg, and builds ToC objects with builderCfg.
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
		// The ToC builders share these metrics, and nothing registers them:
		// the index builder registers collectors with the same names.
		builderMetrics: indexobj.NewBuilderMetrics(nil),
	}
}

// WriteEntry adds entry to the tenant's ToC of the window that holds entry.
//
// WriteEntry leaves the ToC unchanged if it already holds a pointer with
// entry.Path. It compares the path only: the path of an index object is a
// hash of its content, so the same path means the same pointer.
//
// WriteEntry retries a failed write with the backoff config of the writer.
// It returns an error without retrying if entry fails validation, entry spans
// more than one window, or the ToC holds a section of another tenant.
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

// tocChange is one change to a tenant's ToC. It removes the pointers whose
// path is in remove, then adds the entries of add that the ToC does not hold.
type tocChange struct {
	remove map[string]struct{}
	add    []TableOfContentsEntry

	// requireRemove makes the change apply only if the ToC holds a path in
	// remove. A missing ToC holds no path, so the change does not create it.
	requireRemove bool
}

// changeResult is the result of applyChange.
type changeResult int

const (
	// changeFailed means applyChange returned an error.
	changeFailed changeResult = iota
	// changeWritten means applyChange wrote the change to the ToC.
	changeWritten
	// changePresent means the ToC already held the change, so applyChange
	// wrote nothing.
	changePresent
	// changeRaceLost means the change requires a removal and the ToC held
	// none of the paths to remove, so applyChange wrote nothing.
	changeRaceLost
)

// applyChange applies change to the tenant's ToC of window with a
// conditional write. It retries a failed write with the backoff config of the
// writer, and returns an error without retrying if the ToC holds a section of
// another tenant. op labels the metrics and logs.
//
// An attempt can fail after its conditional write landed, for example when
// the response is lost. So if an earlier attempt failed and the ToC now holds
// the change, applyChange returns changeWritten.
func (m *TableOfContentsWriter) applyChange(ctx context.Context, op, tenant string, window time.Time, change tocChange) (changeResult, error) {
	outcome := statusFailure
	start := time.Now()
	defer func() {
		m.metrics.changeTotalSeconds.WithLabelValues(op, string(outcome)).Observe(time.Since(start).Seconds())
	}()

	tocPath := TableOfContentsPath(tenant, window)
	builder, err := indexobj.NewBuilder(tenant, m.builderCfg, nil, m.builderMetrics)
	if err != nil {
		return changeFailed, err
	}

	var (
		result       = changeFailed
		failedBefore bool
	)
	b := backoff.New(ctx, m.backoffCfg)
	for b.Ongoing() {
		attemptStart := time.Now()

		err = m.bucket.GetAndReplace(ctx, tocPath, func(existing io.ReadCloser) (io.ReadCloser, error) {
			if existing != nil {
				defer existing.Close()
			}
			return m.rebuildToC(ctx, builder, existing, change)
		})

		switch {
		case err == nil:
			result, outcome = changeWritten, statusSuccess
			level.Info(m.logger).Log("msg", "toc updated", "op", op, "tocPath", tocPath)
		case errors.Is(err, errChangePresent):
			result, outcome = changePresent, statusSkipped
			level.Info(m.logger).Log("msg", "toc update skipped: toc already holds the change", "op", op, "tocPath", tocPath)
		case errors.Is(err, errRaceLost):
			result, outcome = changeRaceLost, statusSkipped
			level.Info(m.logger).Log("msg", "toc update skipped: toc holds none of the paths to remove", "op", op, "tocPath", tocPath)
		default:
			result, outcome = changeFailed, statusFailure
			level.Error(m.logger).Log("msg", "toc update failed", "op", op, "err", err, "tocPath", tocPath)
		}
		m.metrics.changeAttemptSeconds.WithLabelValues(op, string(outcome)).Observe(time.Since(attemptStart).Seconds())

		if outcome != statusFailure || errors.Is(err, errUnrecoverable) {
			break
		}
		failedBefore = true
		b.Wait()
	}

	// The loop ends with a failure on an unrecoverable error, after the last
	// retry, or once ctx is done. ctx can be done before the first attempt,
	// when err is still nil.
	if outcome == statusFailure {
		return changeFailed, errors.Join(b.Err(), err)
	}
	if result == changePresent && failedBefore {
		result, outcome = changeWritten, statusSuccess
	}
	return result, nil
}

// rebuildToC returns the ToC that results from applying change to the
// existing ToC. existing is nil or empty if the ToC does not exist yet. The
// caller owns the returned reader and must close it.
//
// rebuildToC keeps one pointer for each path, so the ToC it returns holds no
// repeated paths, even if the existing ToC does.
//
// It returns errChangePresent if the existing ToC already holds the change,
// and errRaceLost if the change requires a removal that the existing ToC does
// not allow. It returns an error that wraps errUnrecoverable if the existing
// ToC holds a section of another tenant.
func (m *TableOfContentsWriter) rebuildToC(ctx context.Context, builder *indexobj.Builder, existing io.Reader, change tocChange) (io.ReadCloser, error) {
	builder.Reset()

	buf := m.getBuffer()
	defer m.buffers.Put(buf)

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

	var added int
	for _, entry := range change.add {
		if _, ok := kept[entry.Path]; ok {
			continue
		}
		kept[entry.Path] = struct{}{}
		added++
		if change.requireRemove && removed == 0 {
			// The ToC is not written, so do not append. Keep counting to tell
			// a race lost from a change that is already present.
			continue
		}
		if err := builder.AppendIndexPointer(indexpointers.IndexPointer{
			Path:    entry.Path,
			StartTs: entry.StartTime,
			EndTs:   entry.EndTime,
		}); err != nil {
			return nil, fmt.Errorf("appending index pointer: %w", err)
		}
	}

	switch {
	case change.requireRemove && removed == 0 && added > 0:
		return nil, errRaceLost
	case removed == 0 && added == 0:
		return nil, errChangePresent
	}
	return flushToC(ctx, builder)
}

// getBuffer returns an empty buffer from the pool, or a new one.
func (m *TableOfContentsWriter) getBuffer() *bytes.Buffer {
	if buf, ok := m.buffers.Get().(*bytes.Buffer); ok {
		buf.Reset()
		return buf
	}
	return new(bytes.Buffer)
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

// flushToC flushes builder. On success the caller owns the returned reader
// and must close it. If an error is returned the reader is nil.
func flushToC(ctx context.Context, builder *indexobj.Builder) (io.ReadCloser, error) {
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

// wrappedReadCloser wraps an io.ReadCloser and calls OnClose when Close is
// called. wrappedReadCloser will not close rc on Close is OnClose is defined.
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
