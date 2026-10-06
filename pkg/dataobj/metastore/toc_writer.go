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
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
)

// Define our own builder config for the Table Of Contents object because they are smaller than logs objects.
var tocBuilderCfg = logsobj.BuilderBaseConfig{
	TargetObjectSize:  32 * 1024 * 1024,
	TargetPageSize:    4 * 1024 * 1024,
	BufferSize:        32 * 1024 * 1024, // 8x page size
	TargetSectionSize: 4 * 1024 * 1024,  // object size / 8

	// TODO(chaudum): Should we set the page limit by the number of rows, rather than by bytes size?
	// MaxPageRows: 20000,

	SectionStripeMergeLimit: 2,
}

// The TableOfContents (ToC) writer manages the metastore's Table of Contents files, which are a list of other data objects in storage for a particular tenant and time range.
// The Table of Contents files are used to look up other objects based on a time range, either index files or the log objects themselves. All entries are expected to have an applicable time window.
type TableOfContentsWriter struct {
	tocBuilder *indexobj.Builder // New index pointer based builder.
	metrics    *tocMetrics
	bucket     objstore.Bucket
	logger     log.Logger
	buf        *bytes.Buffer

	builderOnce sync.Once
}

// NewTableOfContentsWriter creates a new Writer for adding entries to the metastore's Table of Contents files.
func NewTableOfContentsWriter(bucket objstore.Bucket, logger log.Logger) *TableOfContentsWriter {
	metrics := newTableOfContentsMetrics()

	return &TableOfContentsWriter{
		bucket:      bucket,
		metrics:     metrics,
		logger:      logger,
		builderOnce: sync.Once{},
	}
}

func (m *TableOfContentsWriter) RegisterMetrics(reg prometheus.Registerer) error {
	return m.metrics.register(reg)
}

func (m *TableOfContentsWriter) UnregisterMetrics(reg prometheus.Registerer) {
	m.metrics.unregister(reg)
}

func (m *TableOfContentsWriter) initBuilder() error {
	var initErr error

	m.builderOnce.Do(func() {
		m.buf = bytes.NewBuffer(make([]byte, 0, tocBuilderCfg.TargetObjectSize))
		indexBuilder, err := indexobj.NewBuilder(tocBuilderCfg, nil, indexobj.NewBuilderMetrics(nil))
		if err != nil {
			initErr = err
			return
		}
		m.tocBuilder = indexBuilder
	})
	return initErr
}

// WriteEntry adds entry to the tenant's ToC of every window that entry
// overlaps. It writes one window at a time and retries each window until the
// write succeeds or ctx is done.
//
// WriteEntry returns an error without retrying if entry fails validation.
func (m *TableOfContentsWriter) WriteEntry(ctx context.Context, tenant string, entry TableOfContentsEntry) error {
	processingTime := prometheus.NewTimer(m.metrics.tocProcessingTime)
	defer processingTime.ObserveDuration()

	if err := entry.validate(); err != nil {
		return err
	}

	// Initialize builder if this is the first call for this partition
	if err := m.initBuilder(); err != nil {
		return err
	}

	// Work our way through the metastore objects window by window, updating & creating them as needed.
	// Each one handles its own retries in order to keep making progress in the event of a failure.
	for tocPath := range IterTableOfContentsPaths(tenant, entry.StartTime, entry.EndTime) {
		b := backoff.New(ctx, backoff.Config{
			MinBackoff: 50 * time.Millisecond,
			MaxBackoff: 10 * time.Second,
		})
		var (
			err     error
			written bool
		)
		for b.Ongoing() {
			err = m.bucket.GetAndReplace(ctx, tocPath, func(existing io.ReadCloser) (io.ReadCloser, error) {
				if existing != nil {
					defer existing.Close()
				}

				m.buf.Reset()
				m.tocBuilder.Reset()

				if existing != nil {
					_, err := io.Copy(m.buf, existing)
					if err != nil {
						return nil, fmt.Errorf("copying to local buffer: %w", err)
					}
				}

				if m.buf.Len() > 0 {
					replayDuration := prometheus.NewTimer(m.metrics.tocReplayTime)
					object, err := dataobj.FromReaderAt(bytes.NewReader(m.buf.Bytes()), int64(m.buf.Len()))
					if err != nil {
						return nil, fmt.Errorf("creating object from buffer: %w", err)
					}
					err = m.copyFromExistingToc(ctx, object)
					if err != nil {
						return nil, fmt.Errorf("reading existing metastore version: %w", err)
					}
					replayDuration.ObserveDuration()
				}

				encodingDuration := prometheus.NewTimer(m.metrics.tocEncodingTime)
				err := m.tocBuilder.AppendIndexPointer(tenant, indexpointers.IndexPointer{
					Path:    entry.Path,
					StartTs: entry.StartTime,
					EndTs:   entry.EndTime,
				})
				if err != nil {
					return nil, fmt.Errorf("appending index pointer: %w", err)
				}

				var (
					obj    *dataobj.Object
					closer io.Closer
				)

				obj, closer, err = m.tocBuilder.Flush()
				if err != nil {
					return nil, fmt.Errorf("flushing metastore builder: %w", err)
				}

				reader, err := obj.Reader(ctx)
				if err != nil {
					_ = closer.Close()
					return nil, err
				}

				encodingDuration.ObserveDuration()
				return &wrappedReadCloser{
					rc: reader,
					OnClose: func() error {
						// We must close our object reader before closing the object
						// itself.
						var errs []error
						errs = append(errs, reader.Close())
						errs = append(errs, closer.Close())
						return errors.Join(errs...)
					},
				}, nil
			})
			if err == nil {
				level.Info(m.logger).Log("msg", "successfully merged & updated metastore", "metastore", tocPath)
				m.metrics.incTableOfContentsWrites(statusSuccess)
				written = true
				break
			}
			level.Error(m.logger).Log("msg", "failed to get and replace metastore object", "err", err, "metastore", tocPath)
			m.metrics.incTableOfContentsWrites(statusFailure)
			b.Wait()
		}

		// Reset at the end too so we don't leave our memory hanging around between calls.
		m.tocBuilder.Reset()

		// The loop only stops without writing once the context is done, which
		// can happen before the first attempt, when err is still nil.
		if !written {
			return errors.Join(b.Err(), err)
		}
	}
	return nil
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

// copyFromExistingToc reads the provided table of contents (toc) object and appends the contained index pointers to the builder. The resulting builder will contain exactly the same entries as the input object.
func (m *TableOfContentsWriter) copyFromExistingToc(ctx context.Context, tocObject *dataobj.Object) error {
	var indexPointersReader indexpointers.RowReader
	defer indexPointersReader.Close()

	// Read index pointers from existing metastore object and write them to the builder for the new object
	pbuf := make([]indexpointers.IndexPointer, 256)

	for _, section := range tocObject.Sections().Filter(indexpointers.CheckSection) {
		sec, err := indexpointers.Open(ctx, section)
		if err != nil {
			return fmt.Errorf("opening section: %w", err)
		}
		tenantID := section.Tenant
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
				if err := m.tocBuilder.AppendIndexPointer(tenantID, indexPointer); err != nil {
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
