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

// DefaultTocBuilderConfig is the builder config for ToC objects. It is smaller
// than the config for logs objects, because a ToC holds only index pointers.
var DefaultTocBuilderConfig = logsobj.BuilderBaseConfig{
	TargetObjectSize:  32 * 1024 * 1024,
	TargetPageSize:    4 * 1024 * 1024,
	BufferSize:        32 * 1024 * 1024, // 8x page size
	TargetSectionSize: 4 * 1024 * 1024,  // object size / 8

	SectionStripeMergeLimit: 2,
}

// DefaultTocWriterBackoffConfig is the retry policy of WriteEntry. It gives up
// after 10 retries.
var DefaultTocWriterBackoffConfig = backoff.Config{
	MinBackoff: 50 * time.Millisecond,
	MaxBackoff: 10 * time.Second,
	MaxRetries: 10,
}

// errUnrecoverable marks a ToC error that retrying can't fix, for example a
// ToC that holds a section of another tenant.
var errUnrecoverable = errors.New("unrecoverable ToC error")

// errEntryPresent cancels the write of a ToC that already holds the entry's
// path. The GetAndReplace callback returns it, and WriteEntry counts it as a
// skipped write instead of an error.
var errEntryPresent = errors.New("ToC already holds the entry")

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
type TableOfContentsWriter struct {
	bucket         objstore.Bucket
	backoffCfg     backoff.Config
	builderCfg     logsobj.BuilderBaseConfig
	logger         log.Logger
	metrics        *TocWriterMetrics
	builderMetrics *indexobj.BuilderMetrics

	// buf holds the existing ToC during a write. resetBuffer allocates it.
	buf *bytes.Buffer
}

// NewTableOfContentsWriter creates a new Writer for adding entries to the
// metastore's Table of Contents files. WriteEntry retries a failed write with
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
//
// WriteEntry is not safe for concurrent use, because all calls share one
// buffer.
func (m *TableOfContentsWriter) WriteEntry(ctx context.Context, tenant string, entry TableOfContentsEntry) error {
	outcome := statusFailure
	start := time.Now()
	defer func() {
		m.metrics.writeEntryTotalSeconds.WithLabelValues(string(outcome)).Observe(time.Since(start).Seconds())
	}()

	if err := entry.validate(); err != nil {
		return err
	}

	tocPath, err := tableOfContentsPathOf(tenant, entry)
	if err != nil {
		return err
	}

	tocBuilder, err := indexobj.NewBuilder(tenant, m.builderCfg, nil, m.builderMetrics)
	if err != nil {
		return err
	}

	b := backoff.New(ctx, m.backoffCfg)
	for b.Ongoing() {
		attemptStart := time.Now()

		err = m.bucket.GetAndReplace(ctx, tocPath, func(existing io.ReadCloser) (io.ReadCloser, error) {
			if existing != nil {
				defer existing.Close()
			}

			tocBuilder.Reset()

			if err := m.copyFromExistingToc(ctx, tocBuilder, existing, entry); err != nil {
				return nil, fmt.Errorf("copying existing ToC: %w", err)
			}
			return appendAndFlush(ctx, tocBuilder, entry)
		})

		switch {
		case err == nil:
			outcome = statusSuccess
			level.Info(m.logger).Log("msg", "toc updated", "tocPath", tocPath)
		case errors.Is(err, errEntryPresent):
			outcome = statusSkipped
			level.Info(m.logger).Log("msg", "toc update skipped: duplicate index pointer", "tocPath", tocPath, "index", entry.Path)
		default:
			outcome = statusFailure
			level.Error(m.logger).Log("msg", "toc update failed", "err", err, "tocPath", tocPath)
		}
		m.metrics.writeEntryAttemptSeconds.WithLabelValues(string(outcome)).Observe(time.Since(attemptStart).Seconds())

		if outcome != statusFailure || errors.Is(err, errUnrecoverable) {
			break
		}
		b.Wait()
	}

	// The loop ends with a failure on an unrecoverable error, after the last
	// retry, or once ctx is done. ctx can be done before the first attempt,
	// when err is still nil.
	if outcome == statusFailure {
		return errors.Join(b.Err(), err)
	}
	return nil
}

// tableOfContentsPathOf returns the path of the tenant's ToC of the window
// that holds entry. It returns an error if entry spans more than one window.
func tableOfContentsPathOf(tenant string, entry TableOfContentsEntry) (string, error) {
	window := entry.StartTime.UTC().Truncate(MetastoreWindowSize)
	if endWindow := entry.EndTime.UTC().Truncate(MetastoreWindowSize); !endWindow.Equal(window) {
		return "", fmt.Errorf("entry %s spans more than one ToC window: %s to %s", entry.Path, window.Format(time.RFC3339), endWindow.Format(time.RFC3339))
	}
	return TableOfContentsPath(tenant, window), nil
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

// copyFromExistingToc appends the index pointers of the existing ToC to
// builder. A missing or empty ToC appends nothing.
//
// It returns an error that wraps errUnrecoverable if the ToC holds a section
// of a tenant other than the builder's tenant. It returns errEntryPresent if
// the ToC already holds a pointer with entry.Path.
func (m *TableOfContentsWriter) copyFromExistingToc(
	ctx context.Context,
	builder *indexobj.Builder,
	existing io.Reader,
	entry TableOfContentsEntry,
) error {
	if existing == nil {
		return nil
	}

	buf := m.resetBuffer()
	if _, err := io.Copy(buf, existing); err != nil {
		return fmt.Errorf("copying to local buffer: %w", err)
	}
	if buf.Len() == 0 {
		return nil
	}

	tocObject, err := dataobj.FromReaderAt(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		return fmt.Errorf("creating object from buffer: %w", err)
	}

	if err := checkTenant(tocObject, builder.Tenant()); err != nil {
		return err
	}

	var indexPointersReader indexpointers.RowReader
	defer indexPointersReader.Close()

	pbuf := make([]indexpointers.IndexPointer, 256)

	for _, section := range tocObject.Sections().Filter(indexpointers.CheckSection) {
		sec, err := indexpointers.Open(ctx, section)
		if err != nil {
			return fmt.Errorf("opening section: %w", err)
		}
		indexPointersReader.Reset(sec)
		if err := indexPointersReader.Open(ctx); err != nil {
			return fmt.Errorf("opening index pointers reader: %w", err)
		}
		for {
			n, err := indexPointersReader.Read(ctx, pbuf)
			if err != nil && !errors.Is(err, io.EOF) {
				return fmt.Errorf("reading index pointers: %w", err)
			}
			for _, indexPointer := range pbuf[:n] {
				if indexPointer.Path == entry.Path {
					return errEntryPresent
				}
				if err := builder.AppendIndexPointer(indexPointer); err != nil {
					return fmt.Errorf("appending index pointers: %w", err)
				}
			}
			if errors.Is(err, io.EOF) {
				break
			}
		}
	}

	return nil
}

// resetBuffer returns the writer's buffer, empty. The buffer is large, so the
// writer allocates it on first use only. A writer that only replaces index
// pointers never needs it.
func (m *TableOfContentsWriter) resetBuffer() *bytes.Buffer {
	if m.buf == nil {
		m.buf = bytes.NewBuffer(make([]byte, 0, m.builderCfg.TargetObjectSize))
	}
	m.buf.Reset()
	return m.buf
}

// appendAndFlush appends entry to builder and flushes it. On success the
// caller owns the returned reader and must close it. If an error is returned
// the reader is nil.
func appendAndFlush(ctx context.Context, builder *indexobj.Builder, entry TableOfContentsEntry) (io.ReadCloser, error) {
	err := builder.AppendIndexPointer(indexpointers.IndexPointer{
		Path:    entry.Path,
		StartTs: entry.StartTime,
		EndTs:   entry.EndTime,
	})
	if err != nil {
		return nil, fmt.Errorf("appending index pointer: %w", err)
	}

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
