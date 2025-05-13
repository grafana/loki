package limits

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/coder/quartz"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/limits/proto"
)

// Consumer allows mocking of certain [kgo.Client] methods in tests.
type Consumer interface {
	PollFetches(context.Context) kgo.Fetches
}

// PlaybackManager processes records from the metadata topic. It is
// responsible for replaying newly assigned partitions and merging records
// from other zones.
type PlaybackManager struct {
	consumer         Consumer
	partitionManager *PartitionManager
	usage            *UsageStore
	// readinessCheck checks if a waiting or replaying partition can be
	// switched to ready.
	readinessCheck PartitionReadinessCheck
	logger         log.Logger

	// Metrics.
	recordsProcessed *prometheus.CounterVec
	lag              prometheus.Histogram

	// Used for tests.
	clock quartz.Clock
}

// NewPlaybackManager returns a new PlaybackManager.
func NewPlaybackManager(
	consumer Consumer,
	partitionManager *PartitionManager,
	usage *UsageStore,
	readinessCheck PartitionReadinessCheck,
	logger log.Logger,
	reg prometheus.Registerer,
) *PlaybackManager {
	return &PlaybackManager{
		consumer:         consumer,
		partitionManager: partitionManager,
		usage:            usage,
		readinessCheck:   readinessCheck,
		logger:           logger,
		clock:            quartz.NewReal(),
		recordsProcessed: promauto.With(reg).NewCounterVec(
			prometheus.CounterOpts{
				Name: "loki_ingest_limits_records_processed_total",
				Help: "The total number of records processed.",
			},
			[]string{"partition"},
		),
		lag: promauto.With(reg).NewHistogram(
			prometheus.HistogramOpts{
				Name:                            "loki_ingest_limits_lag_seconds",
				Help:                            "The estimated consumption lag in seconds, measured as the difference between the current time and the timestamp of the record.",
				NativeHistogramBucketFactor:     1.1,
				NativeHistogramMinResetDuration: 1 * time.Hour,
				NativeHistogramMaxBucketNumber:  100,
				Buckets:                         prometheus.ExponentialBuckets(0.125, 2, 18),
			},
		),
	}
}

func (m *PlaybackManager) Run(ctx context.Context) {
	b := backoff.New(ctx, backoff.Config{
		MinBackoff: 100 * time.Millisecond,
		MaxBackoff: time.Second,
		MaxRetries: 0,
	})
	for b.Ongoing() {
		select {
		case <-ctx.Done():
			return
		default:
			if err := m.pollFetches(ctx); err != nil {
				if errors.Is(err, kgo.ErrClientClosed) {
					return
				}
				level.Error(m.logger).Log("msg", "failed to poll fetches", "err", err.Error())
				b.Wait()
			}
		}
	}
}

func (m *PlaybackManager) pollFetches(ctx context.Context) error {
	fetches := m.consumer.PollFetches(ctx)
	if err := fetches.Err(); err != nil {
		return err
	}
	fetches.EachPartition(m.processFetchTopicPartition(ctx))
	return nil
}

func (m *PlaybackManager) processFetchTopicPartition(ctx context.Context) func(kgo.FetchTopicPartition) {
	return func(p kgo.FetchTopicPartition) {
		// When used with [kgo.EachPartition], this function is called once
		// for each partition in a fetch, including partitions that have not
		// produced records since the last fetch. If there are no records
		// we can just return as there is nothing to do here.
		if len(p.Records) == 0 {
			m.lag.Observe(float64(0))
			return
		}
		logger := log.With(m.logger, "partition", p.Partition)
		state, ok := m.partitionManager.GetState(p.Partition)
		if !ok {
			level.Warn(logger).Log("msg", "discarding records for partition as the partition is not assigned to this client")
			return
		}
		if state == PartitionReplaying {
			// TODO(grobinson): For now we just consume records when replaying
			// a partition. In a future commit this will be moved outside of
			// this check, and records will be consumed both when replaying
			// newly assigned partitions and merging records from other zones.
			for _, r := range p.Records {
				if err := m.processRecord(ctx, r); err != nil {
					level.Error(logger).Log("msg", "failed to process record", "err", err.Error())
				}
			}
			m.recordsProcessed.
				WithLabelValues(strconv.FormatInt(int64(p.Partition), 10)).
				Add(float64(len(p.Records)))
			m.lag.Observe(m.clock.Since(p.Records[len(p.Records)-1].Timestamp).Seconds())
			passed, err := m.readinessCheck(p.Partition, p.Records[len(p.Records)-1])
			if err != nil {
				level.Error(logger).Log("msg", "failed to run readiness check", "err", err.Error())
			} else if passed {
				level.Debug(logger).Log("msg", "passed readiness check, partition is ready")
				m.partitionManager.SetReady(p.Partition)
			}
		}
	}
}

func (m *PlaybackManager) processRecord(_ context.Context, r *kgo.Record) error {
	s := proto.StreamMetadataRecord{}
	if err := s.Unmarshal(r.Value); err != nil {
		return fmt.Errorf("corrupted record: %w", err)
	}
	m.usage.Update(s.Tenant, []*proto.StreamMetadata{s.Metadata}, r.Timestamp, nil)
	return nil
}

type PartitionReadinessCheck func(partition int32, r *kgo.Record) (bool, error)

// NewOffsetReadinessCheck marks a partition as ready if the target offset
// has been reached.
func NewOffsetReadinessCheck(partitionManager *PartitionManager) PartitionReadinessCheck {
	return func(partition int32, r *kgo.Record) (bool, error) {
		return partitionManager.TargetOffsetReached(partition, r.Offset), nil
	}
}
