package metastore

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
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
	metastoreWindowSizeFlag = flag.Duration("metastore.window-size", 12*time.Hour, "The size of the window to use for metastore updates.")
)

type MetastoreManager struct {
	metastoreBuilder *dataobj.Builder
	tenantID         string
	metrics          *metastoreMetrics
	bucket           objstore.Bucket
	logger           log.Logger
	backoff          *backoff.Backoff
}

func NewMetastoreManager(builderCfg dataobj.BuilderConfig, bucket objstore.Bucket, tenantID string, logger log.Logger, reg prometheus.Registerer) (*MetastoreManager, error) {
	metastoreBuilder, err := dataobj.NewBuilder(builderCfg, bucket, tenantID)
	if err != nil {
		return nil, err
	}
	metrics := newMetastoreMetrics()
	if err := metrics.register(reg); err != nil {
		return nil, err
	}

	return &MetastoreManager{
		metastoreBuilder: metastoreBuilder,
		bucket:           bucket,
		metrics:          metrics,
		logger:           logger,
		tenantID:         tenantID,
		backoff: backoff.New(context.TODO(), backoff.Config{
			MinBackoff: 10 * time.Millisecond,
			MaxBackoff: 100 * time.Millisecond,
		}),
	}, nil
}

func (m *MetastoreManager) UpdateMetastore(ctx context.Context, flushResult dataobj.FlushResult) error {
	start := time.Now()
	defer m.metrics.observeMetastoreProcessing(start)
	minTimestamp, maxTimestamp := flushResult.MinTimestamp, flushResult.MaxTimestamp

	// Work our way through the metastore objects window by window, updating & creating them as needed.
	// Each one handles its own retries in order to keep making progress in the event of a failure.
	minMetastoreWindow := minTimestamp.Truncate(metastoreWindowSize)
	maxMetastoreWindow := maxTimestamp.Truncate(metastoreWindowSize)
	for metastoreWindow := minMetastoreWindow; metastoreWindow.Compare(maxMetastoreWindow) <= 0; metastoreWindow = metastoreWindow.Add(metastoreWindowSize) {
		metastorePath := fmt.Sprintf("tenant-%s/metastore/%s.store", m.tenantID, metastoreWindow.Format(time.RFC3339))
		m.backoff.Reset()
		for m.backoff.Ongoing() {
			err := m.bucket.GetAndReplace(ctx, metastorePath, func(existing io.ReadCloser) (io.Reader, error) {
				buf, err := io.ReadAll(existing)
				if err != nil {
					return nil, err
				}

				m.metastoreBuilder.Reset()

				if len(buf) > 0 {
					replayStart := time.Now()
					err = m.metastoreBuilder.FromExisting(bytes.NewReader(buf))
					if err != nil {
						return nil, err
					}
					m.metrics.observeMetastoreReplay(replayStart)
				}

				encodingStart := time.Now()
				for _, stream := range flushResult.Streams {
					if stream.MinTimestamp.After(metastoreWindow.Add(metastoreWindowSize)) || stream.MaxTimestamp.Before(metastoreWindow) {
						continue
					}
					m.metastoreBuilder.AppendSummary(stream, []logproto.Entry{{
						Line: flushResult.Path,
					}})
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
	return nil
}
