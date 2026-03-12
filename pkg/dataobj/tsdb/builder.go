package tsdb

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
)

var ErrPartitionRevoked = errors.New("partition revoked")

const (
	defaultTsdbConsumerGroup = "tsdb-builder"
)

const flushInterval = 15 * time.Minute

// An interface for the methods needed from a kafka client. Useful for testing.
type kafkaClient interface {
	PollRecords(context.Context, int) kgo.Fetches
	CommitRecords(context.Context, ...*kgo.Record) error
	Close()
}

type Builder struct {
	services.Service

	cfg         Config
	readBucket  objstore.Bucket
	tsdbBuilder tsdbBuilder

	// Kafka client and topic/partition info
	client kafkaClient
	topic  string

	// Partition management only
	ownedPartitions map[int32]bool

	// Only kafka commit functionality
	metrics *builderMetrics

	// Control and coordination
	wg                 sync.WaitGroup
	logger             log.Logger
	partitionsMutex    sync.Mutex
	activeCalculations map[int32]context.CancelCauseFunc
	lastFlush          time.Time
	processedRecords   []*kgo.Record
}

func NewTSDBBuilder(
	cfg Config,
	kafkaCfg kafka.Config,
	logger log.Logger,
	instanceID string,
	dataobjBucket objstore.Bucket,
	reg prometheus.Registerer,
) (*Builder, error) {
	builderReg := prometheus.WrapRegistererWith(prometheus.Labels{
		"topic":     kafkaCfg.Topic,
		"component": "index_builder",
	}, reg)

	builderMetrics := newBuilderMetrics()
	if err := builderMetrics.register(builderReg); err != nil {
		return nil, fmt.Errorf("failed to register metrics for tsdb builder: %w", err)
	}

	tsdbStorageBucket := objstore.NewPrefixedBucket(dataobjBucket, cfg.TSDBStoragePrefix)
	tsdbBuilder := newTSDBBuilder(instanceID, tsdbStorageBucket)

	s := &Builder{
		cfg:                cfg,
		logger:             logger,
		readBucket:         dataobjBucket,
		tsdbBuilder:        tsdbBuilder,
		metrics:            builderMetrics,
		ownedPartitions:    make(map[int32]bool),
		activeCalculations: make(map[int32]context.CancelCauseFunc),
		lastFlush:          time.Now(),
	}

	consumerGroup := defaultTsdbConsumerGroup
	if kafkaCfg.ConsumerGroup != "" {
		consumerGroup = kafkaCfg.ConsumerGroup
	}

	kafkaCfg.AutoCreateTopicEnabled = true
	eventConsumerClient, err := client.NewReaderClient(
		"tsdb_builder",
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
			p.ownedPartitions[partition] = true
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
		if time.Since(p.lastFlush) > flushInterval {
			if err := p.tsdbBuilder.Store(ctx); err != nil {
				level.Error(p.logger).Log("msg", "failed to store tsdb builder", "err", err)
			}
			p.client.CommitRecords(ctx, p.processedRecords...)
			p.lastFlush = time.Now()
			p.processedRecords = p.processedRecords[:0]
			p.metrics.commitsTotal.Inc()
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

	calcCtx, cancel := context.WithCancelCause(ctx)
	p.activeCalculations[record.Partition] = cancel

	metastoreEvent := &metastore.ObjectWrittenEvent{}
	if err := metastoreEvent.Unmarshal(record.Value); err != nil {
		level.Error(p.logger).Log("msg", "failed to unmarshal metastore event", "err", err)
		return
	}

	obj, err := dataobj.FromBucket(calcCtx, p.readBucket, metastoreEvent.ObjectPath, 4*1024*1024)
	if err != nil {
		level.Error(p.logger).Log("msg", "failed to create object", "err", err)
		return
	}

	if err := p.tsdbBuilder.Append(calcCtx, obj, metastoreEvent.ObjectPath); err != nil {
		level.Error(p.logger).Log("msg", "failed to append object to tsdb builder", "err", err)
		return
	}

	p.processedRecords = append(p.processedRecords, record)

	level.Info(p.logger).Log("msg", "finished processing event", "object_path", metastoreEvent.ObjectPath)
}

func (p *Builder) cleanupPartition(partition int32) {
	p.partitionsMutex.Lock()
	defer p.partitionsMutex.Unlock()

	// Cancel active calculation for this partition
	if cancel, exists := p.activeCalculations[partition]; exists {
		cancel(nil)
		delete(p.activeCalculations, partition)
	}
}
