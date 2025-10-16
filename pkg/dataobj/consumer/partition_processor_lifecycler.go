package consumer

import (
	"context"
	"sync"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kgo"
)

type processor interface {
	Append(records []*kgo.Record) bool
	start()
	stop()
}

// processorFactory allows mocking of partition processor factory in tests.
type processorFactory interface {
	New(ctx context.Context, client *kgo.Client, topic string, partition int32) processor
}

// partitionProcessorListener is an interface that listens to registering
// and deregistering of partition processors.
type partitionProcessorListener interface {
	OnRegister(topic string, partition int32, processor processor)
	OnDeregister(topic string, partition int32)
}

// partitionProcessorLifecycler manages the lifecycle of partition processors.
type partitionProcessorLifecycler struct {
	factory    processorFactory
	processors map[string]map[int32]processor
	listeners  []partitionProcessorListener
	logger     log.Logger
	reg        prometheus.Registerer
	mtx        sync.Mutex
}

// newPartitionProcessorLifecycler returns a new partitionProcessorLifecycler.
func newPartitionProcessorLifecycler(
	factory processorFactory,
	logger log.Logger,
	reg prometheus.Registerer,
) *partitionProcessorLifecycler {
	return &partitionProcessorLifecycler{
		factory:    factory,
		processors: make(map[string]map[int32]processor),
		listeners:  make([]partitionProcessorListener, 0),
		logger:     logger,
		reg:        reg,
	}
}

// AddListener adds a listener to the partitionProcessorLifecycler.
func (l *partitionProcessorLifecycler) AddListener(listener partitionProcessorListener) {
	l.mtx.Lock()
	defer l.mtx.Unlock()
	l.listeners = append(l.listeners, listener)
}

// RemoveListener removes a listener from the partitionProcessorLifecycler.
func (l *partitionProcessorLifecycler) RemoveListener(listener partitionProcessorListener) {
	l.mtx.Lock()
	defer l.mtx.Unlock()
	for i, next := range l.listeners {
		if next == listener {
			l.listeners = append(l.listeners[:i], l.listeners[i+1:]...)
			return
		}
	}
}

// Register creates and starts a processor for the partition.
func (l *partitionProcessorLifecycler) Register(
	ctx context.Context,
	client *kgo.Client,
	topic string,
	partition int32,
) {
	level.Debug(l.logger).Log("msg", "registering processor", "topic", topic, "partition", partition)
	l.mtx.Lock()
	defer l.mtx.Unlock()
	processorsByTopic, ok := l.processors[topic]
	if !ok {
		processorsByTopic = make(map[int32]processor)
		l.processors[topic] = processorsByTopic
	}
	_, ok = processorsByTopic[partition]
	if !ok {
		processor := l.factory.New(ctx, client, topic, partition)
		processorsByTopic[partition] = processor
		processor.start()
		for _, listener := range l.listeners {
			listener.OnRegister(topic, partition, processor)
		}
	}
}

// Deregister stops and removes processor for the partition.
func (l *partitionProcessorLifecycler) Deregister(_ context.Context, topic string, partition int32) {
	level.Debug(l.logger).Log("msg", "deregistering processor", "topic", topic, "partition", partition)
	l.mtx.Lock()
	defer l.mtx.Unlock()
	processorsByTopic, ok := l.processors[topic]
	if !ok {
		return
	}
	processor, ok := processorsByTopic[partition]
	if !ok {
		return
	}
	processor.stop()
	for _, listener := range l.listeners {
		listener.OnDeregister(topic, partition)
	}
	delete(processorsByTopic, partition)
	if len(processorsByTopic) == 0 {
		delete(l.processors, topic)
	}
}

// Stop stops and removes all processors.
func (l *partitionProcessorLifecycler) Stop(_ context.Context) {
	l.mtx.Lock()
	defer l.mtx.Unlock()
	level.Info(l.logger).Log("msg", "stopping")
	for topic, processors := range l.processors {
		for partition, processor := range processors {
			level.Debug(l.logger).Log("msg", "deregistering processor", "topic", topic, "partition", partition)
			processor.stop()
			for _, listener := range l.listeners {
				listener.OnDeregister(topic, partition)
			}
		}
		delete(l.processors, topic)
	}
	level.Info(l.logger).Log("msg", "stopped")
}
