package kafkav2

import (
	"context"
	"errors"
	"strconv"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/twmb/franz-go/pkg/kgo"
)

// A SinglePartitionConsumer consumes records from a single partition.
type SinglePartitionConsumer struct {
	*services.BasicService

	client        *kgo.Client
	topic         string
	partition     int32
	initialOffset int64
	records       chan<- *kgo.Record

	logger       log.Logger
	pollFailures prometheus.Counter
	polls        prometheus.Counter
}

// NewSinglePartitionConsumer returns a new SinglePartitionConsumer. It
// consumes records from the specified offset. It accepts the two special
// offsets of -2 to consume from the start and -1 to consume from the end.
func NewSinglePartitionConsumer(
	client *kgo.Client,
	topic string,
	partition int32,
	initialOffset int64,
	records chan<- *kgo.Record,
	logger log.Logger,
	r prometheus.Registerer,
) *SinglePartitionConsumer {
	// Wrap the registerer with labels for the topic and partition so we don't
	// need to add it to each metric.
	r = prometheus.WrapRegistererWith(prometheus.Labels{
		"topic":     topic,
		"partition": strconv.Itoa(int(partition)),
	}, r)
	c := SinglePartitionConsumer{
		client:        client,
		topic:         topic,
		partition:     partition,
		initialOffset: initialOffset,
		records:       records,
		logger:        log.With(logger, "topic", topic, "partition", partition),
		pollFailures: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "single_partition_consumer_poll_failures_total",
			Help: "The number of polls that failed.",
		}),
		polls: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "single_partition_consumer_polls_total",
			Help: "Total number of polls.",
		}),
	}
	c.BasicService = services.NewBasicService(c.starting, c.running, c.stopping)
	return &c
}

// starting implements [services.StartingFn].
func (c *SinglePartitionConsumer) starting(_ context.Context) error {
	return nil
}

// running implements [services.RunningFn].
func (c *SinglePartitionConsumer) running(ctx context.Context) error {
	return c.Run(ctx)
}

// running implements [services.StoppingFn].
func (c *SinglePartitionConsumer) stopping(_ error) error {
	return nil
}

func (c *SinglePartitionConsumer) Run(ctx context.Context) error {
	// Consume the topic and partition from the specified offset.
	c.client.AddConsumePartitions(map[string]map[int32]kgo.Offset{
		c.topic: {
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
				c.records <- record
			})
		}
	}
}
