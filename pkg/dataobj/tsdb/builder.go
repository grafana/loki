package index

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
	"github.com/grafana/loki/v3/pkg/scratch"
)

var ErrPartitionRevoked = errors.New("partition revoked")

const (
	defaultIndexConsumerGroup = "index-builder"
)

// An interface for the methods needed from a calculator. Useful for testing.
type tsdbBuilder interface {
	BuildAndStore(ctx context.Context, obj *dataobj.Object, objectPath string) error
}

// An interface for the methods needed from a kafka client. Useful for testing.
type kafkaClient interface {
	PollRecords(context.Context, int) kgo.Fetches
	CommitRecords(context.Context, ...*kgo.Record) error
	Close()
}

type Config struct{}

type Builder struct {
	services.Service

	cfg Config

	// Kafka client and topic/partition info
	client kafkaClient
	topic  string

	// Partition management only
	ownedPartitions map[int32]struct{}

	// Only kafka commit functionality
	// metrics *builderMetrics

	// Control and coordination
	wg                 sync.WaitGroup
	logger             log.Logger
	partitionsMutex    sync.Mutex
	activeCalculations map[int32]context.CancelCauseFunc
}

func NewTSDBBuilder(
	cfg Config,
	kafkaCfg kafka.Config,
	logger log.Logger,
	instanceID string,
	bucket objstore.Bucket,
	scratchStore scratch.Store,
	reg prometheus.Registerer,
) (*Builder, error) {
	builderReg := prometheus.WrapRegistererWith(prometheus.Labels{
		"topic":     kafkaCfg.Topic,
		"component": "index_builder",
	}, reg)

	builderMetrics := newBuilderMetrics()
	if err := builderMetrics.register(builderReg); err != nil {
		return nil, fmt.Errorf("failed to register metrics for index builder: %w", err)
	}

	indexStorageBucket := objstore.NewPrefixedBucket(bucket, mCfg.IndexStoragePrefix)

	if err := builder.RegisterMetrics(builderReg); err != nil {
		return nil, fmt.Errorf("failed to register metrics for index builder: %w", err)
	}

	s := &Builder{
		cfg:    cfg,
		logger: logger,
		// metrics:            builderMetrics,
		ownedPartitions:    make(map[int32]struct{}),
		activeCalculations: make(map[int32]context.CancelCauseFunc),
	}

	consumerGroup := defaultIndexConsumerGroup
	if kafkaCfg.ConsumerGroup != "" {
		consumerGroup = kafkaCfg.ConsumerGroup
	}

	kafkaCfg.AutoCreateTopicEnabled = true
	eventConsumerClient, err := client.NewReaderClient(
		"index_builder",
		kafkaCfg,
		logger,
		reg,
		kgo.ConsumeTopics(kafkaCfg.Topic),
		kgo.InstanceID(instanceID),
		kgo.SessionTimeout(3*time.Minute),
		kgo.ConsumerGroup(consumerGroup),
		kgo.Balancers(kgo.RoundRobinBalancer()),
		kgo.RebalanceTimeout(5*time.Minute),
		kgo.DisableAutoCommit(),
		kgo.OnPartitionsAssigned(s.handlePartitionsAssigned),
		kgo.OnPartitionsRevoked(s.handlePartitionsRevoked),
		kgo.OnPartitionsLost(s.handlePartitionsLost),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create kafka consumer client: %w", err)
	}
	s.client = eventConsumerClient

	s.Service = services.NewBasicService(s.starting, s.running, s.stopping)

	return s, nil
}

func (p *Builder) handlePartitionsAssigned(_ context.Context, _ *kgo.Client, topics map[string][]int32) {
	p.partitionsMutex.Lock()
	defer p.partitionsMutex.Unlock()

	for _, partitions := range topics {
		for _, partition := range partitions {
			p.ownedPartitions[partition] = struct{}{}
		}
	}
}

// This is not thread-safe
func (p *Builder) handlePartitionsRevoked(_ context.Context, _ *kgo.Client, topics map[string][]int32) {
	p.partitionsMutex.Lock()
	defer p.partitionsMutex.Unlock()

	for _, partitions := range topics {
		for _, partition := range partitions {
			// Delete partition metrics to prevent cardinality growth
			p.metrics.deletePartitionMetrics(partition)

			delete(p.ownedPartitions, partition)

			// Cancel any active calculations
			if cancel, exists := p.activeCalculations[partition]; exists {
				cancel(ErrPartitionRevoked)
				delete(p.activeCalculations, partition)
			}
		}
	}
}

func (p *Builder) handlePartitionsLost(ctx context.Context, client *kgo.Client, topics map[string][]int32) {
	p.handlePartitionsRevoked(ctx, client, topics)
}

func (p *Builder) starting(ctx context.Context) error {
	level.Info(p.logger).Log("msg", "started tsdb builder service")
	return nil
}

func (p *Builder) running(ctx context.Context) error {
	// Main Kafka processing loop
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		fetches := p.client.PollRecords(ctx, -1)
		if err := fetches.Err0(); err != nil {
			if errors.Is(err, kgo.ErrClientClosed) || errors.Is(err, context.Canceled) {
				return err
			}
			// Some other error occurred. We will check it in
			// [processFetchTopicPartition] instead.
		}
		if fetches.Empty() {
			continue
		}
		fetches.EachPartition(func(fetch kgo.FetchTopicPartition) {
			if err := fetch.Err; err != nil {
				level.Error(p.logger).Log("msg", "failed to fetch records for topic partition", "topic", fetch.Topic, "partition", fetch.Partition, "err", err.Error())
				return
			}
			for _, record := range fetch.Records {
				p.processRecord(ctx, record)
			}
		})
	}
}

func (p *Builder) stopping(failureCase error) error {
	p.wg.Wait()
	p.client.Close()
	return failureCase
}

// processRecord processes a single record. It is not safe for concurrent use.
func (p *Builder) processRecord(ctx context.Context, record *kgo.Record) {
	defer p.cleanupPartition(record.Partition)

	// Commit the records
	if err := p.commitRecords(calculationCtx, records); err != nil {
		if errors.Is(context.Cause(calculationCtx), ErrPartitionRevoked) {
			level.Debug(p.logger).Log("msg", "partition revoked, aborting index commit", "partition", record.Partition)
			return
		}
		level.Error(p.logger).Log("msg", "failed to commit records", "err", err, "partition", record.Partition)
		return
	}
	p.markEventsCompleted(record.Partition, len(records))
}

// Appends a record and returns a slice of buffered events to index. The slice will be empty if no indexing is required.
func (p *Builder) appendRecord(ctx context.Context, record *kgo.Record) (context.Context, []bufferedEvent) {
	event := &metastore.ObjectWrittenEvent{}
	if err := event.Unmarshal(record.Value); err != nil {
		level.Error(p.logger).Log("msg", "failed to unmarshal metastore event", "err", err)
		return nil, nil
	}

	bufferedEvt := &bufferedEvent{
		event:  *event,
		record: record,
	}

	return p.bufferAndTryProcess(ctx, record.Partition, bufferedEvt, triggerTypeAppend)
}

func (p *Builder) cleanupPartition(partition int32) {
	p.partitionsMutex.Lock()
	defer p.partitionsMutex.Unlock()

	// Cancel active calculation for this partition
	if cancel, exists := p.activeCalculations[partition]; exists {
		cancel(nil)
		delete(p.activeCalculations, partition)
	}

	if state, ok := p.ownedPartitions[partition]; ok {
		// Clear processed events and reset processing flag
		state.isProcessing = false
		state.lastActivity = time.Now()
	}
}

func (p *Builder) markEventsCompleted(partition int32, eventsProcessed int) {
	p.partitionsMutex.Lock()
	defer p.partitionsMutex.Unlock()

	state, ok := p.ownedPartitions[partition]
	if !ok {
		return
	}
	state.events = state.events[eventsProcessed:]
}

func (p *Builder) checkAndFlushStalePartitions(ctx context.Context) {
	p.partitionsMutex.Lock()
	partitionsToFlush := make([]int32, 0)

	for partition, state := range p.ownedPartitions {
		if !state.isProcessing &&
			time.Since(state.lastActivity) >= p.cfg.MaxIdleTime {
			partitionsToFlush = append(partitionsToFlush, partition)
		}
	}
	p.partitionsMutex.Unlock()

	for _, partition := range partitionsToFlush {
		p.flushPartition(ctx, partition)
	}
}

func (p *Builder) flushPartition(ctx context.Context, partition int32) {
	calculationCtx, eventsToFlush := p.bufferAndTryProcess(ctx, partition, nil, triggerTypeMaxIdle)
	if len(eventsToFlush) == 0 {
		return
	}

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		defer p.cleanupPartition(partition)

		level.Info(p.logger).Log("msg", "flushing stale partition",
			"partition", partition, "events", len(eventsToFlush))

		// Submit to indexer service and wait for completion
		records, err := p.indexer.submitBuild(calculationCtx, eventsToFlush, partition, triggerTypeMaxIdle)
		if err != nil {
			if errors.Is(context.Cause(calculationCtx), ErrPartitionRevoked) {
				level.Debug(p.logger).Log("msg", "partition revoked during flush", "partition", partition)
				return
			}
			level.Error(p.logger).Log("msg", "failed to flush partition", "partition", partition, "err", err)
			return
		}

		// Commit the records
		if err := p.commitRecords(calculationCtx, records); err != nil {
			if errors.Is(context.Cause(calculationCtx), ErrPartitionRevoked) {
				level.Debug(p.logger).Log("msg", "partition revoked during flush commit", "partition", partition)
				return
			}
			level.Error(p.logger).Log("msg", "failed to commit flush records", "partition", partition, "err", err)
		}
	}()
}

// bufferAndTryProcess is the unified method that handles both buffering and processing decisions
func (p *Builder) bufferAndTryProcess(ctx context.Context, partition int32, newEvent *bufferedEvent, trigger triggerType) (context.Context, []bufferedEvent) {
	p.partitionsMutex.Lock()
	defer p.partitionsMutex.Unlock()

	state, exists := p.ownedPartitions[partition]
	if !exists {
		return nil, nil
	}

	// Add new event to buffer if provided (normal processing case)
	if newEvent != nil {
		state.events = append(state.events, *newEvent)
		state.lastActivity = time.Now()
		level.Debug(p.logger).Log("msg", "buffered new event for partition", "count", len(state.events), "partition", partition)
	}

	// Check if we can start processing
	if state.isProcessing || len(state.events) == 0 {
		return nil, nil
	}

	// Check trigger-specific requirements
	switch trigger {
	case triggerTypeAppend:
		if len(state.events) < p.cfg.EventsPerIndex {
			return nil, nil
		}
	case triggerTypeMaxIdle:
		if time.Since(state.lastActivity) < p.cfg.MaxIdleTime {
			return nil, nil
		}
	default:
		level.Error(p.logger).Log("msg", "unknown trigger type")
		return nil, nil
	}

	// Atomically mark as processing and extract events
	state.isProcessing = true
	eventsToProcess := make([]bufferedEvent, len(state.events))
	copy(eventsToProcess, state.events)

	// Set up cancellation context with proper coordination
	calculationCtx, cancel := context.WithCancelCause(ctx)
	p.activeCalculations[partition] = cancel

	level.Debug(p.logger).Log("msg", "started processing partition",
		"partition", partition, "events", len(eventsToProcess), "trigger", trigger)

	return calculationCtx, eventsToProcess
}

func (p *Builder) commitRecords(ctx context.Context, records []*kgo.Record) error {
	if len(records) == 0 {
		return nil
	}

	backoff := backoff.New(ctx, backoff.Config{
		MinBackoff: 100 * time.Millisecond,
		MaxBackoff: 10 * time.Second,
		MaxRetries: 20,
	})

	var lastErr error
	backoff.Reset()
	for backoff.Ongoing() {
		p.metrics.incCommitsTotal()
		err := p.client.CommitRecords(ctx, records...)
		if err == nil {
			return nil
		}
		level.Error(p.logger).Log("msg", "failed to commit records", "err", err, "count", len(records))
		p.metrics.incCommitFailures()
		lastErr = err
		backoff.Wait()
	}
	return lastErr
}
