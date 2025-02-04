package metastore

import (
	"bytes"
	"context"
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
	"github.com/grafana/loki/v3/pkg/logproto"
)

const (
	metastoreWindowSize = 12 * time.Hour
)

var (
	// Define our own builder config because metastore objects are significantly smaller.
	metastoreBuilderCfg = dataobj.BuilderConfig{
		TargetObjectSize:  32 * 1024 * 1024,
		TargetPageSize:    4 * 1024 * 1024,
		BufferSize:        32 * 1024 * 1024, // 8x page size
		TargetSectionSize: 4 * 1024 * 1024,  // object size / 8
	}
)

type Manager struct {
	metastoreBuilder *dataobj.Builder
	tenantID         string
	metrics          *metastoreMetrics
	bucket           objstore.Bucket
	logger           log.Logger
	backoff          *backoff.Backoff
	buf              *bytes.Buffer

	builderOnce sync.Once
}

func NewManager(bucket objstore.Bucket, tenantID string, logger log.Logger) *Manager {
	metrics := newMetastoreMetrics()

	return &Manager{
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

func (m *Manager) RegisterMetrics(reg prometheus.Registerer) error {
	return m.metrics.register(reg)
}

func (m *Manager) UnregisterMetrics(reg prometheus.Registerer) {
	m.metrics.unregister(reg)
}

func (m *Manager) initBuilder() error {
	var initErr error
	m.builderOnce.Do(func() {
		metastoreBuilder, err := dataobj.NewBuilder(metastoreBuilderCfg)
		if err != nil {
			initErr = err
			return
		}
		m.buf = bytes.NewBuffer(make([]byte, 0, metastoreBuilderCfg.TargetObjectSize))
		m.metastoreBuilder = metastoreBuilder
	})
	return initErr
}

// UpdateMetastore adds provided dataobj path to the metastore. Flush stats are used to determine the stored metadata about this dataobj.
func (m *Manager) UpdateMetastore(ctx context.Context, dataobjPath string, flushStats dataobj.FlushStats) error {
	var err error
	processingTime := prometheus.NewTimer(m.metrics.metastoreProcessingTime)
	defer processingTime.ObserveDuration()

	// Initialize builder if this is the first call for this partition
	if err := m.initBuilder(); err != nil {
		return err
	}

	minTimestamp, maxTimestamp := flushStats.MinTimestamp, flushStats.MaxTimestamp

	// Work our way through the metastore objects window by window, updating & creating them as needed.
	// Each one handles its own retries in order to keep making progress in the event of a failure.
	minMetastoreWindow := minTimestamp.Truncate(metastoreWindowSize)
	maxMetastoreWindow := maxTimestamp.Truncate(metastoreWindowSize)
	for metastoreWindow := minMetastoreWindow; !metastoreWindow.After(maxMetastoreWindow); metastoreWindow = metastoreWindow.Add(metastoreWindowSize) {
		metastorePath := fmt.Sprintf("tenant-%s/metastore/%s.store", m.tenantID, metastoreWindow.Format(time.RFC3339))
		m.backoff.Reset()
		for m.backoff.Ongoing() {
			err = m.bucket.GetAndReplace(ctx, metastorePath, func(existing io.Reader) (io.Reader, error) {
				m.buf.Reset()
				_, err := io.Copy(m.buf, existing)
				if err != nil {
					return nil, err
				}

				m.metastoreBuilder.Reset()

				if m.buf.Len() > 0 {
					replayDuration := prometheus.NewTimer(m.metrics.metastoreReplayTime)
					object := dataobj.FromReaderAt(bytes.NewReader(m.buf.Bytes()), int64(m.buf.Len()))
					if err := m.readFromExisting(ctx, object); err != nil {
						return nil, err
					}
					replayDuration.ObserveDuration()
				}

				encodingDuration := prometheus.NewTimer(m.metrics.metastoreEncodingTime)

				ls := fmt.Sprintf("{__start__=\"%d\", __end__=\"%d\", __path__=\"%s\"}", minTimestamp.UnixNano(), maxTimestamp.UnixNano(), dataobjPath)
				err = m.metastoreBuilder.Append(logproto.Stream{
					Labels:  ls,
					Entries: []logproto.Entry{{Line: ""}},
				})
				if err != nil {
					return nil, err
				}

				m.buf.Reset()
				_, err = m.metastoreBuilder.Flush(m.buf)
				if err != nil {
					return nil, err
				}
				encodingDuration.ObserveDuration()
				return m.buf, nil
			})
			if err == nil {
				level.Info(m.logger).Log("msg", "successfully merged & updated metastore", "metastore", metastorePath)
				break
			}
			level.Error(m.logger).Log("msg", "failed to get and replace metastore object", "err", err, "metastore", metastorePath)
			m.metrics.incMetastoreWriteFailures()
			m.backoff.Wait()
		}
		// Reset at the end too so we don't leave our memory hanging around between calls.
		m.metastoreBuilder.Reset()
	}
	return err
}

// readFromExisting reads the provided metastore object and appends the streams to the builder so it can be later modified.
func (m *Manager) readFromExisting(ctx context.Context, object *dataobj.Object) error {
	// Fetch sections
	si, err := object.Metadata(ctx)
	if err != nil {
		return err
	}

	// Read streams from existing metastore object and write them to the builder for the new object
	streams := make([]dataobj.Stream, 100)
	for i := 0; i < si.StreamsSections; i++ {
		streamsReader := dataobj.NewStreamsReader(object, i)
		for n, err := streamsReader.Read(ctx, streams); n > 0; n, err = streamsReader.Read(ctx, streams) {
			if err != nil && err != io.EOF {
				return err
			}
			for _, stream := range streams[:n] {
				err = m.metastoreBuilder.Append(logproto.Stream{
					Labels:  stream.Labels.String(),
					Entries: []logproto.Entry{{Line: ""}},
				})
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}
