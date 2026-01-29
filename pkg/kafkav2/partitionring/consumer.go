package partitionring

import (
	"context"
	"errors"
	"strconv"
	"sync/atomic"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/twmb/franz-go/pkg/kgo"
)

// A SinglePartitionConsumer consumes records from a single partition.
type SinglePartitionConsumer struct {
	client             *kgo.Client
	topic              string
	partition          int32
	initialOffset      int64
	lastConsumedOffset atomic.Int64
	dst                chan<- *kgo.Record

	logger         log.Logger
	recordsPerPoll prometheus.Histogram
	pollFailures   prometheus.Counter
	polls          prometheus.Counter
}

// NewSinglePartitionConsumer returns a new SinglePartitionConsumer. It
// consumes records from the specified offset. It accepts the two special
// offsets of -2 to consume from the start and -1 to consume from the end.
func NewSinglePartitionConsumer(
	client *kgo.Client,
	topic string,
	partition int32,
	initialOffset int64,
	dst chan<- *kgo.Record,
	logger log.Logger,
	r prometheus.Registerer,
) *SinglePartitionConsumer {
	// Wrap the registerer with labels for the topic and partition, this
	// allows all metrics to inherit them automatically.
	r = prometheus.WrapRegistererWith(prometheus.Labels{"topic": topic, "partition": strconv.Itoa(int(partition))}, r)
	return &SinglePartitionConsumer{
		client:        client,
		topic:         topic,
		partition:     partition,
		initialOffset: initialOffset,
		dst:           dst,
		logger:        log.With(logger, "topic", topic, "partition", partition),
		recordsPerPoll: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:    "single_partition_consumer_records_per_poll",
			Help:    "The number of records received per poll.",
			Buckets: prometheus.ExponentialBuckets(1, 2, 15),
		}),
		pollFailures: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "single_partition_consumer_poll_failures_total",
			Help: "The number of polls that failed.",
		}),
		polls: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "single_partition_consumer_polls_total",
			Help: "Total number of polls.",
		}),
	}
}

// Run polls records from the partition until the context is canceled or
// a fatal error occurs.
func (c *SinglePartitionConsumer) Run(ctx context.Context) error {
	// Consume the topic and partition from the specified offset.
	c.client.AddConsumePartitions(map[string]map[int32]kgo.Offset{
		c.topic: map[int32]kgo.Offset{
			c.partition: kgo.NewOffset().At(c.initialOffset),
		},
	})
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			c.polls.Inc()
			fetches := c.client.PollRecords(ctx, -1)
			// If the client is closed, or the context was canceled, return
			// the error as no fetches were polled. We use this instead of
			// [kgo.IsClientClosed] so we can also check if the context was
			// canceled.
			if err := fetches.Err0(); err != nil {
				if errors.Is(err, kgo.ErrClientClosed) || errors.Is(err, context.Canceled) {
					c.pollFailures.Inc()
					return err
				}
			}
			fetches.EachRecord(func(record *kgo.Record) {
				c.lastConsumedOffset.Store(record.Offset)
				c.dst <- record
			})
		}
	}
}

// LastConsumedOffset returns the last consumed offset.
func (c *SinglePartitionConsumer) LastConsumedOffset() int64 {
	return c.lastConsumedOffset.Load()
}
