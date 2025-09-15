package consumer

import (
	"context"
	"sync"

	"github.com/go-kit/log"
	"github.com/grafana/loki/v3/pkg/kafka/partition"
)

// processor allows mocking of [partitionProcessor] in tests.
type processor interface {
	Append(records []partition.Record) bool
}

// consumer polls records from the Kafka topic and passes each record to
// its indended processor.
type consumer struct {
	logger    log.Logger
	processor processor
	mtx       sync.RWMutex
}

// newConsumer returns a new consumer.
func newConsumer(processor processor, logger log.Logger) *consumer {
	return &consumer{
		logger:    logger,
		processor: processor,
	}
}

func (c *consumer) Start(_ context.Context, recordsChan <-chan []partition.Record) func() {
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for records := range recordsChan {
			c.processor.Append(records)
		}
	}()
	return wg.Wait
}

// newConsumerFactory returns a consumer factory.
func newConsumerFactory(partitionProcessorFactory *partitionProcessorFactory) partition.ConsumerFactory {
	return func(committer partition.Committer, logger log.Logger) (partition.Consumer, error) {
		return newConsumer(partitionProcessorFactory.New(committer, logger), logger), nil
	}
}
