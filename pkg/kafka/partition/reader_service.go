package partition

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/kafka"
)

const (
	phaseStarting = "starting"
	phaseRunning  = "running"
)

type ConsumerFactory func(committer Committer, logger log.Logger) (Consumer, error)

type Consumer interface {
	Start(ctx context.Context, recordsChan <-chan []Record) func()
}

type serviceMetrics struct {
	phase     *prometheus.GaugeVec
	partition *prometheus.GaugeVec
}

func newServiceMetrics(r prometheus.Registerer) *serviceMetrics {
	return &serviceMetrics{
		partition: promauto.With(r).NewGaugeVec(prometheus.GaugeOpts{
			Name: "loki_ingest_storage_reader_partition",
			Help: "The partition ID assigned to this reader.",
		}, []string{"id"}),
		phase: promauto.With(r).NewGaugeVec(prometheus.GaugeOpts{
			Name: "loki_ingest_storage_reader_phase",
			Help: "The current phase of the consumer.",
		}, []string{"phase"}),
	}
}

type ReaderService struct {
	services.Service

	cfg             ReaderConfig
	reader          Reader
	offsetManager   OffsetManager
	consumerFactory ConsumerFactory
	logger          log.Logger
	metrics         *serviceMetrics
	committer       *partitionCommitter
	partitionID     int32

	lastProcessedOffset int64
}

type ReaderConfig struct {
	MaxConsumerLagAtStartup       time.Duration
	ConsumerGroupOffsetCommitFreq time.Duration
}

// mimics `NewReader` constructor but builds a reader service using
// a reader.
func NewReaderService(
	kafkaCfg kafka.Config,
	partitionID int32,
	instanceID string,
	consumerFactory ConsumerFactory,
	logger log.Logger,
	reg prometheus.Registerer,
) (*ReaderService, error) {
	readerMetrics := NewReaderMetrics(reg)
	reader, err := NewKafkaReader(
		kafkaCfg,
		partitionID,
		logger,
		readerMetrics,
	)
	if err != nil {
		return nil, fmt.Errorf("creating kafka reader: %w", err)
	}

	offsetManager, err := NewKafkaOffsetManager(
		kafkaCfg,
		instanceID,
		logger,
		reg,
	)
	if err != nil {
		return nil, fmt.Errorf("creating kafka offset manager: %w", err)
	}

	return newReaderService(
		ReaderConfig{
			MaxConsumerLagAtStartup:       kafkaCfg.MaxConsumerLagAtStartup,
			ConsumerGroupOffsetCommitFreq: kafkaCfg.ConsumerGroupOffsetCommitInterval,
		},
		reader,
		offsetManager,
		partitionID,
		consumerFactory,
		logger,
		reg,
	), nil
}

func newReaderService(
	cfg ReaderConfig,
	reader Reader,
	offsetManager OffsetManager,
	partitionID int32,
	consumerFactory ConsumerFactory,
	logger log.Logger,
	reg prometheus.Registerer,
) *ReaderService {
	s := &ReaderService{
		cfg:                 cfg,
		reader:              reader,
		offsetManager:       offsetManager,
		partitionID:         partitionID,
		consumerFactory:     consumerFactory,
		logger:              log.With(logger, "partition", partitionID, "consumer_group", offsetManager.ConsumerGroup()),
		metrics:             newServiceMetrics(reg),
		lastProcessedOffset: int64(KafkaEndOffset),
	}

	// Create the committer
	s.committer = newCommitter(offsetManager, partitionID, cfg.ConsumerGroupOffsetCommitFreq, logger, reg)

	s.Service = services.NewBasicService(s.starting, s.running, nil)
	return s
}

func (s *ReaderService) starting(ctx context.Context) error {
	level.Info(s.logger).Log("msg", "starting reader service")
	s.metrics.reportOwnerOfPartition(s.partitionID)
	s.metrics.reportStarting()

	logger := log.With(s.logger, "phase", phaseStarting)
	s.reader.SetPhase(phaseStarting)
	// Fetch the last committed offset to determine where to start reading
	lastCommittedOffset, err := s.offsetManager.FetchLastCommittedOffset(ctx, s.partitionID)
	if err != nil {
		return fmt.Errorf("fetching last committed offset: %w", err)
	}

	if lastCommittedOffset == int64(KafkaEndOffset) {
		level.Warn(logger).Log("msg", fmt.Sprintf("no committed offset found, starting from %d", KafkaStartOffset))
	} else {
		level.Debug(logger).Log("msg", "last committed offset", "offset", lastCommittedOffset)
	}

	consumeOffset := int64(KafkaStartOffset)
	if lastCommittedOffset >= 0 {
		// Read from the next offset.
		consumeOffset = lastCommittedOffset + 1
	}
	level.Debug(logger).Log("msg", "consuming from offset", "offset", consumeOffset)
	s.reader.SetOffsetForConsumption(consumeOffset)

	if err = s.processConsumerLagAtStartup(ctx, logger); err != nil {
		return fmt.Errorf("failed to process consumer lag at startup: %w", err)
	}

	return nil
}

func (s *ReaderService) running(ctx context.Context) error {
	level.Info(s.logger).Log("msg", "reader service running")
	s.metrics.reportRunning()
	s.reader.SetPhase(phaseRunning)

	consumer, err := s.consumerFactory(s.committer, log.With(s.logger, "phase", phaseRunning))
	if err != nil {
		return fmt.Errorf("creating consumer: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	recordsChan := s.startFetchLoop(ctx)
	wait := consumer.Start(ctx, recordsChan)
	wait()
	s.committer.Stop()
	return nil
}

func (s *ReaderService) processConsumerLagAtStartup(ctx context.Context, logger log.Logger) error {
	if s.cfg.MaxConsumerLagAtStartup <= 0 {
		level.Debug(logger).Log("msg", "processing consumer lag at startup is disabled")
		return nil
	}

	consumer, err := s.consumerFactory(s.committer, logger)
	if err != nil {
		return fmt.Errorf("failed to create consumer: %w", err)
	}

	cancelCtx, cancel := context.WithCancel(ctx)
	recordsCh := make(chan []Record)
	wait := consumer.Start(cancelCtx, recordsCh)
	defer func() {
		close(recordsCh)
		cancel()
		wait()
	}()

	level.Debug(logger).Log("msg", "processing consumer lag at startup")
	_, err = s.fetchUntilLagSatisfied(ctx, s.cfg.MaxConsumerLagAtStartup, logger, recordsCh, time.Since)
	if err != nil {
		level.Error(logger).Log("msg", "failed to catch up", "err", err)
		return err
	}
	level.Debug(logger).Log("msg", "processing consumer lag at startup finished")

	return nil
}

func (s *ReaderService) fetchUntilLagSatisfied(
	ctx context.Context,
	targetLag time.Duration,
	logger log.Logger,
	recordsCh chan<- []Record,
	timeSince func(time.Time) time.Duration,
) (time.Duration, error) {
	b := backoff.New(ctx, backoff.Config{
		MinBackoff: 100 * time.Millisecond,
		MaxBackoff: time.Second,
		// Retry forever (unless context is canceled / deadline exceeded).
		MaxRetries: 0,
	})
	currentLag := time.Duration(0)

	for b.Ongoing() {
		// Send a direct request to the Kafka backend to fetch the partition start offset.
		partitionStartOffset, err := s.offsetManager.FetchPartitionOffset(ctx, s.partitionID, KafkaStartOffset)
		if err != nil {
			level.Warn(logger).Log("msg", "partition reader failed to fetch partition start offset", "err", err)
			b.Wait()
			continue
		}

		consumerGroupLastCommittedOffset, err := s.offsetManager.FetchLastCommittedOffset(ctx, s.partitionID)
		if err != nil {
			level.Warn(logger).Log("msg", "partition reader failed to fetch last committed offset", "err", err)
			b.Wait()
			continue
		}

		// Send a direct request to the Kafka backend to fetch the last produced offset.
		// We intentionally don't use WaitNextFetchLastProducedOffset() to not introduce further
		// latency.
		lastProducedOffsetRequestedAt := time.Now()
		lastProducedOffset, err := s.offsetManager.FetchPartitionOffset(ctx, s.partitionID, KafkaEndOffset)
		if err != nil {
			level.Warn(logger).Log("msg", "partition reader failed to fetch last produced offset", "err", err)
			b.Wait()
			continue
		}

		// Kafka returns the next empty offset so we must subtract 1 to get the oldest written offset.
		lastProducedOffset = lastProducedOffset - 1

		level.Debug(logger).Log(
			"msg",
			"fetched latest offset information",
			"partition_start_offset",
			partitionStartOffset,
			"last_produced_offset",
			lastProducedOffset,
			"last_committed_offset",
			consumerGroupLastCommittedOffset)

		// Ensure there are some records to consume. For example, if the partition has been inactive for a long
		// time and all its records have been deleted, the partition start offset may be > 0 but there are no
		// records to actually consume.
		if partitionStartOffset > lastProducedOffset {
			level.Info(logger).Log("msg", "partition reader found no records to consume because partition is empty", "partition_start_offset", partitionStartOffset, "last_produced_offset", lastProducedOffset)
			return 0, nil
		}

		if consumerGroupLastCommittedOffset == lastProducedOffset {
			level.Info(logger).Log("msg", "partition reader found no records to consume because it is already up-to-date", "last_committed_offset", consumerGroupLastCommittedOffset, "last_produced_offset", lastProducedOffset)
			return 0, nil
		}

		// This message is NOT expected to be logged with a very high rate. In this log we display the last measured
		// lag. If we don't have it (lag is zero value), then it will not be logged.
		level.Info(loggerWithCurrentLagIfSet(logger, currentLag)).Log("msg", "partition reader is consuming records to honor target and max consumer lag", "partition_start_offset", partitionStartOffset, "last_produced_offset", lastProducedOffset, "last_processed_offset", s.lastProcessedOffset, "offset_lag", lastProducedOffset-s.lastProcessedOffset)

		for b.Ongoing() {
			// Continue reading until we reached the desired offset.
			if lastProducedOffset <= s.lastProcessedOffset {
				break
			}
			if time.Since(lastProducedOffsetRequestedAt) > time.Minute {
				level.Info(loggerWithCurrentLagIfSet(logger, currentLag)).Log("msg", "partition reader is still consuming records...", "last_processed_offset", s.lastProcessedOffset, "offset_lag", lastProducedOffset-s.lastProcessedOffset)
			}

			timedCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			records, err := s.reader.Poll(timedCtx, -1)
			cancel()

			if err != nil {
				level.Error(logger).Log("msg", "error polling records", "err", err)
				continue
			}
			if len(records) > 0 {
				recordsCh <- records
				s.lastProcessedOffset = records[len(records)-1].Offset
			}
		}

		if b.Err() != nil {
			return 0, b.ErrCause()
		}

		// If it took less than the max desired lag to replay the partition
		// then we can stop here, otherwise we'll have to redo it.
		if currentLag = timeSince(lastProducedOffsetRequestedAt); currentLag <= targetLag {
			return currentLag, nil
		}
	}

	return 0, b.ErrCause()
}

func (s *ReaderService) startFetchLoop(ctx context.Context) chan []Record {
	records := make(chan []Record)
	go func() {
		defer close(records)
		for {
			select {
			case <-ctx.Done():
				return
			default:
				res, err := s.reader.Poll(ctx, -1)
				if err != nil {
					level.Error(s.logger).Log("msg", "error polling records", "err", err)
					continue
				}
				if len(res) > 0 {
					records <- res
					s.lastProcessedOffset = res[len(res)-1].Offset
				}
			}
		}
	}()
	return records
}

func (s *serviceMetrics) reportOwnerOfPartition(id int32) {
	s.partition.WithLabelValues(strconv.Itoa(int(id))).Set(1)
}

func (s *serviceMetrics) reportStarting() {
	s.phase.WithLabelValues(phaseStarting).Set(1)
	s.phase.WithLabelValues(phaseRunning).Set(0)
}

func (s *serviceMetrics) reportRunning() {
	s.phase.WithLabelValues(phaseStarting).Set(0)
	s.phase.WithLabelValues(phaseRunning).Set(1)
}

func loggerWithCurrentLagIfSet(logger log.Logger, currentLag time.Duration) log.Logger {
	if currentLag <= 0 {
		return logger
	}

	return log.With(logger, "current_lag", currentLag)
}
