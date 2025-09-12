package consumer

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/twmb/franz-go/pkg/kgo"
)

// kafkaConsumer allows mocking of certain [kgo.Client] methods in tests.
type kafkaConsumer interface {
	PollFetches(context.Context) kgo.Fetches
}

// processor allows mocking of [partitionProcessor] in tests.
type processor interface {
	Append(records []*kgo.Record) bool
}

// consumer polls records from the Kafka topic and passes each record to
// its indended processor.
type consumer struct {
	client    kafkaConsumer
	logger    log.Logger
	processor processor
	mtx       sync.RWMutex
}

// newConsumer returns a new consumer.
func newConsumer(client kafkaConsumer, processor processor, logger log.Logger) *consumer {
	return &consumer{
		client:    client,
		logger:    logger,
		processor: processor,
	}
}

// run starts the poll loop. It is stopped when either the context is canceled
// or the kafka client is closed.
func (c *consumer) Run(ctx context.Context) error {
	b := backoff.New(ctx, backoff.Config{
		MinBackoff: 100 * time.Millisecond,
		MaxBackoff: time.Second,
		MaxRetries: 0,
	})
	for b.Ongoing() {
		select {
		case <-ctx.Done():
			return nil
		default:
			if err := c.pollFetches(ctx); err != nil {
				if errors.Is(err, kgo.ErrClientClosed) {
					return nil
				}
				level.Error(c.logger).Log("msg", "failed to poll fetches", "err", err.Error())
				b.Wait()
			}
		}
	}
	return nil
}

func (c *consumer) pollFetches(ctx context.Context) error {
	fetches := c.client.PollFetches(ctx)
	// If the client is closed, or the context was canceled, return the error
	// as no fetches were polled. We use this instead of [kgo.IsClientClosed]
	// so we can also check if the context was canceled.
	if err := fetches.Err0(); err != nil {
		if errors.Is(err, kgo.ErrClientClosed) || errors.Is(err, context.Canceled) {
			return err
		}
	}
	c.processor.Append(fetches.Records())
	return nil
}
