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
	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"
)

const (
	metastoreWindowSize = 12 * time.Hour
)

var (
	// Define our own builder config because metastore objects are significantly smaller.
	metastoreBuilderCfg = dataobj.BuilderConfig{
		SHAPrefixSize:     2,
		TargetObjectSize:  32 * 1024 * 1024,
		TargetPageSize:    4 * 1024 * 1024,
		BufferSize:        32 * 1024 * 1024, // 8x page size
		TargetSectionSize: 4 * 1024 * 1024,  // object size / 8
	}
)

type MetastoreManager struct {
	metastoreBuilder *dataobj.Builder
	tenantID         string
	metrics          *metastoreMetrics
	bucket           objstore.Bucket
	logger           log.Logger
	backoff          *backoff.Backoff

	builderOnce sync.Once
}

func NewMetastoreManager(bucket objstore.Bucket, tenantID string, logger log.Logger, reg prometheus.Registerer) (*MetastoreManager, error) {
	metrics := newMetastoreMetrics()
	if err := metrics.register(reg); err != nil {
		return nil, err
	}

	return &MetastoreManager{
		bucket:   bucket,
		metrics:  metrics,
		logger:   logger,
		tenantID: tenantID,
		backoff: backoff.New(context.TODO(), backoff.Config{
			MinBackoff: 50 * time.Millisecond,
			MaxBackoff: 10 * time.Second,
		}),
		builderOnce: sync.Once{},
	}, nil
}

func (m *MetastoreManager) initBuilder() error {
	var initErr error
	m.builderOnce.Do(func() {
		metastoreBuilder, err := dataobj.NewBuilder(metastoreBuilderCfg, m.bucket, m.tenantID)
		if err != nil {
			initErr = err
			return
		}
		m.metastoreBuilder = metastoreBuilder
	})
	return initErr
}

func (m *MetastoreManager) UpdateMetastore(ctx context.Context, flushResult dataobj.FlushResult) error {
	var err error
	start := time.Now()
	defer m.metrics.observeMetastoreProcessing(start)

	// Initialize builder if this is the first call for this partition
	if err := m.initBuilder(); err != nil {
		return err
	}

	minTimestamp, maxTimestamp := flushResult.MinTimestamp, flushResult.MaxTimestamp

	// Work our way through the metastore objects window by window, updating & creating them as needed.
	// Each one handles its own retries in order to keep making progress in the event of a failure.
	minMetastoreWindow := minTimestamp.Truncate(metastoreWindowSize)
	maxMetastoreWindow := maxTimestamp.Truncate(metastoreWindowSize)
	for metastoreWindow := minMetastoreWindow; metastoreWindow.Compare(maxMetastoreWindow) <= 0; metastoreWindow = metastoreWindow.Add(metastoreWindowSize) {
		metastorePath := fmt.Sprintf("tenant-%s/metastore/%s.store", m.tenantID, metastoreWindow.Format(time.RFC3339))
		m.backoff.Reset()
		for m.backoff.Ongoing() {
			err = m.bucket.GetAndReplace(ctx, metastorePath, func(existing io.Reader) (io.Reader, error) {
				buf, err := io.ReadAll(existing)
				if err != nil {
					return nil, err
				}

				m.metastoreBuilder.Reset()

				if len(buf) > 0 {
					replayStart := time.Now()
					object := dataobj.FromReaderAt(bytes.NewReader(buf), int64(len(buf)))
					if err := m.readFromExisting(ctx, object); err != nil {
						return nil, err
					}
					m.metrics.observeMetastoreReplay(replayStart)
				}

				encodingStart := time.Now()

				ls := fmt.Sprintf("{__start__=\"%d\", __end__=\"%d\", __path__=\"%s\"}", minTimestamp.UnixNano(), maxTimestamp.UnixNano(), flushResult.Path)
				err = m.metastoreBuilder.Append(logproto.Stream{
					Labels:  ls,
					Entries: []logproto.Entry{{Line: ""}},
				})
				if err != nil {
					return nil, err
				}

				newMetastore, err := m.metastoreBuilder.FlushToBuffer()
				if err != nil {
					return nil, err
				}
				m.metrics.observeMetastoreEncoding(encodingStart)
				return newMetastore, nil
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

func (m *MetastoreManager) readFromExisting(ctx context.Context, object *dataobj.Object) error {
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
