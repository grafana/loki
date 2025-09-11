package consumer

import (
	"context"
	"errors"
	"fmt"
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

// consumer polls records from the Kafka topic and passes each record to
// its indended processor.
type consumer struct {
	client     kafkaConsumer
	logger     log.Logger
	processors map[string]map[int32]processor
	mtx        sync.RWMutex
}

// newConsumer returns a new consumer.
func newConsumer(client kafkaConsumer, logger log.Logger) *consumer {
	return &consumer{
		client:     client,
		logger:     logger,
		processors: make(map[string]map[int32]processor),
	}
}

// OnRegister implements the [partitionProcessorListener] interface.
func (c *consumer) OnRegister(topic string, partition int32, p processor) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	processorsByTopic, ok := c.processors[topic]
	if !ok {
		processorsByTopic = make(map[int32]processor)
		c.processors[topic] = processorsByTopic
	}
	processorsByTopic[partition] = p
}

// OnDeregister implements the [partitionProcessorListener] interface.
func (c *consumer) OnDeregister(topic string, partition int32) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	processorsByTopic, ok := c.processors[topic]
	if !ok {
		return
	}
	delete(processorsByTopic, partition)
	if len(processorsByTopic) == 0 {
		delete(c.processors, topic)
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
		// Some other error occurred. We will check it in
		// [processFetchTopicPartition] instead.
	}
	fetches.EachPartition(c.processFetchTopicPartition(ctx))
	return nil
}

func (c *consumer) processFetchTopicPartition(_ context.Context) func(kgo.FetchTopicPartition) {
	return func(fetch kgo.FetchTopicPartition) {
		if err := fetch.Err; err != nil {
			level.Error(c.logger).Log("msg", "failed to fetch records for topic partition", "topic", fetch.Topic, "partition", fetch.Partition, "err", err.Error())
			return
		}
		// If there are no records for this partition then skip it.
		if len(fetch.Records) == 0 {
			return
		}
		processor, err := c.processorForTopicPartition(fetch.Topic, fetch.Partition)
		if err != nil {
			// It should never happen that we fetch records for a newly
			// assigned partition before the lifecycler has registered a
			// processor for it. This is because [kgo.OnPartitionsAssigned]
			// guarantees to return before the client starts fetching records
			// for new partitions.
			//
			// However, it can happen the client has fetched records for a
			// partition that has just been reassigned to another consumer.
			// If this happens, we will attempt to process those records, but
			// may not have a processor for them as the processor would have
			// been deregistered via [kgo.OnPartitionsRevoked], and the
			// following log line will be emitted.
			level.Error(c.logger).Log("msg", "failed to get processor", "error", err.Error())
			return
		}
		_ = processor.Append(fetch.Records)
	}
}

// processorForTopicPartition returns the processor for the topic and partition.
// It returns an error if one does not exist.
func (c *consumer) processorForTopicPartition(topic string, partition int32) (processor, error) {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	processorsByTopic, ok := c.processors[topic]
	if !ok {
		return nil, fmt.Errorf("unknown topic %s", topic)
	}
	p, ok := processorsByTopic[partition]
	if !ok {
		return nil, fmt.Errorf("unknown partition %d for topic %s", partition, topic)
	}
	return p, nil
}
