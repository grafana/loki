package metastore

import (
	"bytes"
	"context"
	stderrors "errors"
	"io"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
)

// Define our own builder config because metastore objects are significantly smaller.
var tocBuilderCfg = logsobj.BuilderConfig{
	TargetObjectSize:  32 * 1024 * 1024,
	TargetPageSize:    4 * 1024 * 1024,
	BufferSize:        32 * 1024 * 1024, // 8x page size
	TargetSectionSize: 4 * 1024 * 1024,  // object size / 8

	SectionStripeMergeLimit: 2,
}

// The TableOfContents updater writes the entrypoint file to the metastore, the Table of Contents, which is a list of other data objects in storage.
// The Table of contents is used to look up another set of objects based on a time range, either index files or the log objects themselves. All entries are expected to have an applicable time window.
type TableOfContentsWriter struct {
	cfg        Config
	tocBuilder *indexobj.Builder // New index pointer based builder.
	tenantID   string
	metrics    *tocMetrics
	bucket     objstore.Bucket
	logger     log.Logger
	backoff    *backoff.Backoff
	buf        *bytes.Buffer

	builderOnce sync.Once
}

func NewTableOfContentsWriter(cfg Config, bucket objstore.Bucket, tenantID string, logger log.Logger) *TableOfContentsWriter {
	metrics := newTableOfContentsMetrics()

	return &TableOfContentsWriter{
		cfg:      cfg,
		bucket:   bucket,
		metrics:  metrics,
		logger:   logger,
		tenantID: tenantID,
		backoff: backoff.New(context.TODO(), backoff.Config{
			MinBackoff: 50 * time.Millisecond,
			MaxBackoff: 10 * time.Second,
		}),
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
		indexBuilder, err := indexobj.NewBuilder(indexobj.BuilderConfig{
			TargetObjectSize:        tocBuilderCfg.TargetObjectSize,
			TargetPageSize:          tocBuilderCfg.TargetPageSize,
			BufferSize:              tocBuilderCfg.BufferSize,
			TargetSectionSize:       tocBuilderCfg.TargetSectionSize,
			SectionStripeMergeLimit: tocBuilderCfg.SectionStripeMergeLimit,
		})
		if err != nil {
			initErr = err
			return
		}
		m.tocBuilder = indexBuilder
	})
	return initErr
}

// WriteEntry adds the provided path to the Table of Contents file. The min/max timestamps are stored as metastore for the new entry can be accessed by time.
func (m *TableOfContentsWriter) WriteEntry(ctx context.Context, dataobjPath string, minTimestamp, maxTimestamp time.Time) error {
	var err error
	processingTime := prometheus.NewTimer(m.metrics.tocProcessingTime)
	defer processingTime.ObserveDuration()

	// Initialize builder if this is the first call for this partition
	if err := m.initBuilder(); err != nil {
		return err
	}

	// Work our way through the metastore objects window by window, updating & creating them as needed.
	// Each one handles its own retries in order to keep making progress in the event of a failure.
	prefix := storagePrefixFor(m.cfg.Storage, m.tenantID)
	for metastorePath := range iterTableOfContentsPaths(m.tenantID, minTimestamp, maxTimestamp, prefix) {
		m.backoff.Reset()
		for m.backoff.Ongoing() {
			err = m.bucket.GetAndReplace(ctx, metastorePath, func(existing io.ReadCloser) (io.ReadCloser, error) {
				if existing != nil {
					defer existing.Close()
				}

				m.buf.Reset()
				m.tocBuilder.Reset()

				if existing != nil {
					_, err := io.Copy(m.buf, existing)
					if err != nil {
						return nil, errors.Wrap(err, "copying to local buffer")
					}
				}

				if m.buf.Len() > 0 {
					replayDuration := prometheus.NewTimer(m.metrics.tocReplayTime)
					object, err := dataobj.FromReaderAt(bytes.NewReader(m.buf.Bytes()), int64(m.buf.Len()))
					if err != nil {
						return nil, errors.Wrap(err, "creating object from buffer")
					}
					err = m.copyFromExistingToc(ctx, object)
					if err != nil {
						return nil, errors.Wrap(err, "reading existing metastore version")
					}
					replayDuration.ObserveDuration()
				}

				encodingDuration := prometheus.NewTimer(m.metrics.tocEncodingTime)
				err := m.tocBuilder.AppendIndexPointer(dataobjPath, minTimestamp, maxTimestamp)
				if err != nil {
					return nil, errors.Wrap(err, "appending index pointer")
				}

				var (
					obj    *dataobj.Object
					closer io.Closer
				)

				obj, closer, err = m.tocBuilder.Flush()
				if err != nil {
					return nil, errors.Wrap(err, "flushing metastore builder")
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
						return stderrors.Join(errs...)
					},
				}, nil
			})
			if err == nil {
				level.Info(m.logger).Log("msg", "successfully merged & updated metastore", "metastore", metastorePath)
				m.metrics.incMetastoreWrites(statusSuccess)
				break
			}
			level.Error(m.logger).Log("msg", "failed to get and replace metastore object", "err", err, "metastore", metastorePath)
			m.metrics.incMetastoreWrites(statusFailure)
			m.backoff.Wait()
		}

		// Reset at the end too so we don't leave our memory hanging around between calls.
		m.tocBuilder.Reset()
	}
	return err
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

// copyFromExistingToc reads the provided table of contents object and appends the index pointers to the builder. The resulting builder will contain exactly the same entries as the input object.
func (m *TableOfContentsWriter) copyFromExistingToc(ctx context.Context, tocObject *dataobj.Object) error {
	var indexPointersReader indexpointers.RowReader
	defer indexPointersReader.Close()

	// Read index pointers from existing metastore object and write them to the builder for the new object
	pbuf := make([]indexpointers.IndexPointer, 256)

	for _, section := range tocObject.Sections().Filter(indexpointers.CheckSection) {
		sec, err := indexpointers.Open(ctx, section)
		if err != nil {
			return errors.Wrap(err, "opening section")
		}
		indexPointersReader.Reset(sec)
		for n, err := indexPointersReader.Read(ctx, pbuf); n > 0; n, err = indexPointersReader.Read(ctx, pbuf) {
			if err != nil && err != io.EOF {
				return errors.Wrap(err, "reading index pointers")
			}
			for _, indexPointer := range pbuf[:n] {
				err = m.tocBuilder.AppendIndexPointer(indexPointer.Path, indexPointer.StartTs, indexPointer.EndTs)
				if err != nil {
					return errors.Wrap(err, "appending index pointers")
				}
			}
		}
	}

	return nil
}
