package limits

import (
	"context"
	"errors"
	"fmt"
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

// kafkaConsumer allows mocking of certain [kgo.Client] methods in tests.
type kafkaConsumer interface {
	PollFetches(context.Context) kgo.Fetches
}

// consumer processes records from the metadata topic. It is responsible for
// replaying newly assigned partitions and merging records from other zones.
type consumer struct {
	client           kafkaConsumer
	partitionManager *partitionManager
	usage            *UsageStore
	// readinessCheck checks if a waiting or replaying partition can be
	// switched to ready.
	readinessCheck partitionReadinessCheck
	// zone is used to discard our own records.
	zone   string
	logger log.Logger

	// Metrics.
	lag              prometheus.Histogram
	recordsFetched   prometheus.Counter
	recordsDiscarded prometheus.Counter
	recordsInvalid   prometheus.Counter

	// Used for tests.
	clock quartz.Clock
}

// newConsumer returns a new Consumer.
func newConsumer(
	client kafkaConsumer,
	partitionManager *partitionManager,
	usage *UsageStore,
	readinessCheck partitionReadinessCheck,
	zone string,
	logger log.Logger,
	reg prometheus.Registerer,
) *consumer {
	return &consumer{
		client:           client,
		partitionManager: partitionManager,
		usage:            usage,
		readinessCheck:   readinessCheck,
		zone:             zone,
		logger:           logger,
		clock:            quartz.NewReal(),
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
		recordsFetched: promauto.With(reg).NewCounter(
			prometheus.CounterOpts{
				Name: "loki_ingest_limits_records_fetched_total",
				Help: "The total number of records fetched.",
			},
		),
		recordsDiscarded: promauto.With(reg).NewCounter(
			prometheus.CounterOpts{
				Name: "loki_ingest_limits_records_discarded_total",
				Help: "The total number of records discarded.",
			},
		),
		recordsInvalid: promauto.With(reg).NewCounter(
			prometheus.CounterOpts{
				Name: "loki_ingest_limits_records_invalid_total",
				Help: "The total number of invalid records.",
			},
		),
	}
}

func (c *consumer) run(ctx context.Context) {
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
			if err := c.pollFetches(ctx); err != nil {
				if errors.Is(err, kgo.ErrClientClosed) {
					return
				}
				level.Error(c.logger).Log("msg", "failed to poll fetches", "err", err.Error())
				b.Wait()
			}
		}
	}
}

func (c *consumer) pollFetches(ctx context.Context) error {
	fetches := c.client.PollFetches(ctx)
	if err := fetches.Err(); err != nil {
		return err
	}
	fetches.EachPartition(c.processFetchTopicPartition(ctx))
	return nil
}

func (c *consumer) processFetchTopicPartition(ctx context.Context) func(kgo.FetchTopicPartition) {
	return func(p kgo.FetchTopicPartition) {
		// When used with [kgo.EachPartition], this function is called once
		// for each partition in a fetch, including partitions that have not
		// produced records since the last fetch. If there are no records
		// we can just return as there is nothing to do here.
		if len(p.Records) == 0 {
			c.lag.Observe(float64(0))
			return
		}
		logger := log.With(c.logger, "partition", p.Partition)
		c.recordsFetched.Add(float64(len(p.Records)))
		// We need the state of the partition so we can discard any records
		// that we produced (unless replaying) and mark a replaying partition
		// as ready once it has finished replaying.
		state, ok := c.partitionManager.getState(p.Partition)
		if !ok {
			c.recordsDiscarded.Add(float64(len(p.Records)))
			level.Warn(logger).Log("msg", "discarding records for partition as the partition is not assigned to this client")
			return
		}
		for _, r := range p.Records {
			if err := c.processRecord(ctx, state, r); err != nil {
				level.Error(logger).Log("msg", "failed to process record", "err", err.Error())
			}
		}
		// Get the last record (has the latest offset and timestamp).
		lastRecord := p.Records[len(p.Records)-1]
		c.lag.Observe(c.clock.Since(lastRecord.Timestamp).Seconds())
		if state == partitionReplaying {
			passed, err := c.readinessCheck(p.Partition, lastRecord)
			if err != nil {
				level.Error(logger).Log("msg", "failed to run readiness check", "err", err.Error())
			} else if passed {
				level.Debug(logger).Log("msg", "passed readiness check, partition is ready")
				c.partitionManager.setReady(p.Partition)
			}
		}
	}
}

func (c *consumer) processRecord(_ context.Context, state partitionState, r *kgo.Record) error {
	s := proto.StreamMetadataRecord{}
	if err := s.Unmarshal(r.Value); err != nil {
		c.recordsInvalid.Inc()
		return fmt.Errorf("corrupted record: %w", err)
	}
	if state == partitionReady && c.zone == s.Zone {
		// Discard our own records so we don't count the same streams twice.
		c.recordsDiscarded.Inc()
		return nil
	}
	c.usage.Update(s.Tenant, []*proto.StreamMetadata{s.Metadata}, r.Timestamp, nil)
	return nil
}

type partitionReadinessCheck func(partition int32, r *kgo.Record) (bool, error)

// newOffsetReadinessCheck marks a partition as ready if the target offset
// has been reached.
func newOffsetReadinessCheck(partitionManager *partitionManager) partitionReadinessCheck {
	return func(partition int32, r *kgo.Record) (bool, error) {
		return partitionManager.targetOffsetReached(partition, r.Offset), nil
	}
}
