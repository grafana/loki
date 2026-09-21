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
	"github.com/grafana/dskit/services"
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
	*services.BasicService
	client           kafkaConsumer
	partitionManager *partitionManager
	usage            *usageStore
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
	clock       quartz.Clock
	postFetchCh chan<- struct{}
}

// newConsumer returns a new Consumer.
func newConsumer(
	client kafkaConsumer,
	partitionManager *partitionManager,
	usage *usageStore,
	readinessCheck partitionReadinessCheck,
	zone string,
	logger log.Logger,
	reg prometheus.Registerer,
) *consumer {
	c := &consumer{
		client:           client,
		partitionManager: partitionManager,
		usage:            usage,
		readinessCheck:   readinessCheck,
		zone:             zone,
		logger:           logger,
		clock:            quartz.NewReal(),
		lag: promauto.With(reg).NewHistogram(
			prometheus.HistogramOpts{
				Name: "loki_ingest_limits_lag_seconds",
				Help: "The estimated consumption lag in seconds, measured as the difference between the current time and the timestamp of the record.",

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
	c.BasicService = services.NewBasicService(c.starting, c.running, c.stopping)
	return c
}

// starting implements [services.StartingFn].
func (c *consumer) starting(_ context.Context) error {
	return nil
}

// running implements [services.RunningFn].
func (c *consumer) running(ctx context.Context) error {
	return c.run(ctx)
}

// running implements [services.StoppingFn].
func (c *consumer) stopping(_ error) error {
	return nil
}

func (c *consumer) run(ctx context.Context) error {
	b := backoff.New(ctx, backoff.Config{
		MinBackoff: 100 * time.Millisecond,
		MaxBackoff: 10 * time.Second,
		MaxRetries: 0,
	})
	for b.Ongoing() {
		fetches := c.client.PollFetches(ctx)
		// We use Err0 instead of [kgo.IsClientClosed] so we can also check
		// if the context was canceled.
		if err := fetches.Err0(); errors.Is(err, kgo.ErrClientClosed) || errors.Is(err, context.Canceled) {
			// We don't return an error here as it manifests as a service
			// failure when stopping the service.
			return nil
		}
		// The client can fetch from multiple brokers in a single poll.
		// This means we must handle both records and errors at the same
		// time, as some brokers might be polled successfully while others
		// return errors.
		var numRecords int
		fetches.EachPartition(c.processFetchTopicPartition(ctx, &numRecords))
		if numRecords == 0 {
			// If no records were fetched, backoff before the next poll.
			b.Wait()
		} else {
			// If records were fetched, reset the backoff before the next poll.
			b.Reset()
		}
		if c.postFetchCh != nil {
			// Most of the time this is used in tests that need to synchronize with the
			// fetch loop. For example, a test might want to wait for a fetch to complete
			// and then assert an expectation.
			c.postFetchCh <- struct{}{}
		}
	}
	return nil
}

func (c *consumer) processFetchTopicPartition(ctx context.Context, numRecords *int) func(kgo.FetchTopicPartition) {
	return func(p kgo.FetchTopicPartition) {
		if err := p.Err; err != nil {
			level.Error(c.logger).Log("msg", "failed to poll fetches", "topic", p.Topic, "partition", p.Partition, "err", err)
			return
		}
		// When used with [kgo.EachPartition], this function is called once
		// for each partition in a fetch, including partitions that have not
		// produced records since the last fetch. If there are no records
		// we can just return as there is nothing to do here.
		if len(p.Records) == 0 {
			c.lag.Observe(float64(0))
			return
		}
		logger := log.With(c.logger, "partition", p.Partition)
		*numRecords += len(p.Records)
		c.recordsFetched.Add(float64(len(p.Records)))
		// We need the state of the partition so we can discard any records
		// that we produced (unless replaying) and mark a replaying partition
		// as ready once it has finished replaying.
		state, ok := c.partitionManager.GetState(p.Partition)
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
				c.partitionManager.SetReady(p.Partition)
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
	if c.shouldDiscardRecord(state, &s) {
		c.recordsDiscarded.Inc()
		return nil
	}
	if err := c.usage.Update(s.Tenant, s.Metadata, r.Timestamp); err != nil {
		if errors.Is(err, errOutsideActiveWindow) {
			c.recordsDiscarded.Inc()
		} else {
			return err
		}
	}
	return nil
}

func (c *consumer) shouldDiscardRecord(state partitionState, s *proto.StreamMetadataRecord) bool {
	// Discard our own records so we don't count the same streams twice.
	return state == partitionReady && c.zone == s.Zone
}

type partitionReadinessCheck func(partition int32, r *kgo.Record) (bool, error)

// newOffsetReadinessCheck marks a partition as ready if the target offset
// has been reached.
func newOffsetReadinessCheck(m *partitionManager) partitionReadinessCheck {
	return func(partition int32, r *kgo.Record) (bool, error) {
		return m.TargetOffsetReached(partition, r.Offset), nil
	}
}
