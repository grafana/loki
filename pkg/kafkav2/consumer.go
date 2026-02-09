package kafkav2

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
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

	logger      log.Logger
	fetchErrors prometheus.Counter
	polls       prometheus.Counter
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
	// Consume the topic and partition from the specified offset.
	client.AddConsumePartitions(map[string]map[int32]kgo.Offset{
		topic: {
			partition: kgo.NewOffset().At(initialOffset),
		},
	})
	c := SinglePartitionConsumer{
		client:        client,
		topic:         topic,
		partition:     partition,
		initialOffset: initialOffset,
		records:       records,
		logger:        log.With(logger, "topic", topic, "partition", partition),
		fetchErrors: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "single_partition_consumer_fetch_erros_total",
			Help: "The number of fetch errors.",
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
	b := backoff.New(ctx, backoff.Config{
		MinBackoff: time.Millisecond * 100,
		MaxBackoff: time.Second * 10,
		MaxRetries: 0, // Infinite retries.
	})
	for b.Ongoing() {
		c.polls.Inc()
		fetches := c.client.PollRecords(ctx, -1)
		// If the client is closed, or the context was canceled, exit the service.
		// We use Err0 instead of [kgo.IsClientClosed] so we can also check if the
		// context was canceled.
		if err := fetches.Err0(); errors.Is(err, kgo.ErrClientClosed) || errors.Is(err, context.Canceled) {
			// We don't return ctx.Err() here as it manifests as a service failure
			// when stopping the service.
			return nil
		}
		// The client can fetch from multiple brokers in a single poll. This means
		// we must handle both records and errors at the same time, as some brokers
		// might be polled successfully while others return errors.
		for record := range fetches.RecordsAll() {
			// We must check for cancelation to avoid a deadlock. This can happen
			// if the receiver stopped without draining the chan.
			select {
			case <-ctx.Done():
				// We don't return ctx.Err() here as it manifests as a service failure
				// when stopping the service.
				return nil
			case c.records <- record:
			}
		}
		var numErrs int
		fetches.EachError(func(_ string, _ int32, err error) {
			level.Error(c.logger).Log("msg", "failed to poll fetches", "err", err)
			c.fetchErrors.Inc()
			numErrs++
		})
		if numErrs == 0 {
			b.Reset()
		} else {
			b.Wait()
		}
	}
	return nil
}
