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
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/kafka"
)

var errWaitTargetLagDeadlineExceeded = errors.New("waiting for target lag deadline exceeded")

const (
	kafkaStartOffset = -2
	kafkaEndOffset   = -1

	phaseStarting = "starting"
	phaseRunning  = "running"
)

type ConsumerFactory func(committer Committer) (Consumer, error)

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
	consumerFactory ConsumerFactory
	logger          log.Logger
	metrics         *serviceMetrics
	committer       *partitionCommitter

	lastProcessedOffset int64
}

type ReaderConfig struct {
	TargetConsumerLagAtStartup    time.Duration
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
	reader, err := NewKafkaReader(
		kafkaCfg,
		partitionID,
		instanceID,
		logger,
		reg,
	)
	if err != nil {
		return nil, fmt.Errorf("creating kafka reader: %w", err)
	}
	return newReaderService(
		ReaderConfig{
			TargetConsumerLagAtStartup:    kafkaCfg.TargetConsumerLagAtStartup,
			MaxConsumerLagAtStartup:       kafkaCfg.MaxConsumerLagAtStartup,
			ConsumerGroupOffsetCommitFreq: kafkaCfg.ConsumerGroupOffsetCommitInterval,
		},
		reader,
		consumerFactory,
		logger,
		reg,
	), nil
}

func newReaderService(
	cfg ReaderConfig,
	reader Reader,
	consumerFactory ConsumerFactory,
	logger log.Logger,
	reg prometheus.Registerer,
) *ReaderService {
	s := &ReaderService{
		cfg:                 cfg,
		reader:              reader,
		consumerFactory:     consumerFactory,
		logger:              log.With(logger, "partition", reader.Partition(), "consumer_group", reader.ConsumerGroup()),
		metrics:             newServiceMetrics(reg),
		lastProcessedOffset: kafkaEndOffset,
	}

	// Create the committer
	s.committer = newCommitter(reader, cfg.ConsumerGroupOffsetCommitFreq, logger, reg)

	s.Service = services.NewBasicService(s.starting, s.running, nil)
	return s
}

func (s *ReaderService) starting(ctx context.Context) error {
	level.Info(s.logger).Log("msg", "starting reader service")
	s.metrics.reportOwnerOfPartition(s.reader.Partition())
	s.metrics.reportStarting()

	// Fetch the last committed offset to determine where to start reading
	lastCommittedOffset, err := s.reader.FetchLastCommittedOffset(ctx)
	if err != nil {
		return fmt.Errorf("fetching last committed offset: %w", err)
	}

	if lastCommittedOffset == int64(KafkaEndOffset) {
		level.Warn(s.logger).Log("msg", fmt.Sprintf("no committed offset found, starting from %d", kafkaStartOffset))
	} else {
		level.Debug(s.logger).Log("msg", "last committed offset", "offset", lastCommittedOffset)
	}

	consumeOffset := int64(kafkaStartOffset)
	if lastCommittedOffset >= 0 {
		// Read from the next offset.
		consumeOffset = lastCommittedOffset + 1
	}
	level.Debug(s.logger).Log("msg", "consuming from offset", "offset", consumeOffset)
	s.reader.SetOffsetForConsumption(consumeOffset)

	return s.processConsumerLag(ctx)
}

func (s *ReaderService) processConsumerLag(ctx context.Context) error {
	targetLag := s.cfg.TargetConsumerLagAtStartup
	maxLag := s.cfg.MaxConsumerLagAtStartup

	if targetLag > 0 && maxLag > 0 {
		consumer, err := s.consumerFactory(s.committer)
		if err != nil {
			return fmt.Errorf("failed to create consumer: %w", err)
		}

		cancelCtx, cancel := context.WithCancel(ctx)
		recordsChan := make(chan []Record)
		wait := consumer.Start(cancelCtx, recordsChan)
		defer func() {
			close(recordsChan)
			cancel()
			wait()
		}()

		err = s.processNextFetchesUntilTargetOrMaxLagHonored(ctx, maxLag, targetLag, recordsChan)
		if err != nil {
			level.Error(s.logger).Log("msg", "failed to catch up to max lag", "err", err)
			return err
		}
	}

	return nil
}

func (s *ReaderService) running(ctx context.Context) error {
	level.Info(s.logger).Log("msg", "reader service running")
	s.metrics.reportRunning()

	consumer, err := s.consumerFactory(s.committer)
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

// processNextFetchesUntilTargetOrMaxLagHonored process records from Kafka until at least the maxLag is honored.
// This function does a best-effort to get lag below targetLag, but it's not guaranteed that it will be
// reached once this function successfully returns (only maxLag is guaranteed).
func (s *ReaderService) processNextFetchesUntilTargetOrMaxLagHonored(ctx context.Context, targetLag, maxLag time.Duration, recordsChan chan<- []Record) error {
	logger := log.With(s.logger, "target_lag", targetLag, "max_lag", maxLag)
	level.Info(logger).Log("msg", "partition reader is starting to consume partition until target and max consumer lag is honored")

	attempts := []func() (time.Duration, error){
		// First process fetches until at least the max lag is honored.
		func() (time.Duration, error) {
			return s.processNextFetchesUntilLagHonored(ctx, maxLag, logger, recordsChan, time.Since)
		},

		// If the target lag hasn't been reached with the first attempt (which stops once at least the max lag
		// is honored) then we try to reach the (lower) target lag within a fixed time (best-effort).
		// The timeout is equal to the max lag. This is done because we expect at least a 2x replay speed
		// from Kafka (which means at most it takes 1s to ingest 2s of data): assuming new data is continuously
		// written to the partition, we give the reader maxLag time to replay the backlog + ingest the new data
		// written in the meanwhile.
		func() (time.Duration, error) {
			timedCtx, cancel := context.WithTimeoutCause(ctx, maxLag, errWaitTargetLagDeadlineExceeded)
			defer cancel()

			return s.processNextFetchesUntilLagHonored(timedCtx, targetLag, logger, recordsChan, time.Since)
		},

		// If the target lag hasn't been reached with the previous attempt then we'll move on. However,
		// we still need to guarantee that in the meanwhile the lag didn't increase and max lag is still honored.
		func() (time.Duration, error) {
			return s.processNextFetchesUntilLagHonored(ctx, maxLag, logger, recordsChan, time.Since)
		},
	}

	var currLag time.Duration
	for _, attempt := range attempts {
		var err error

		currLag, err = attempt()
		if errors.Is(err, errWaitTargetLagDeadlineExceeded) {
			continue
		}
		if err != nil {
			return err
		}
		if currLag <= targetLag {
			level.Info(logger).Log(
				"msg", "partition reader consumed partition and current lag is lower or equal to configured target consumer lag",
				"last_consumed_offset", s.committer.lastCommittedOffset,
				"current_lag", currLag,
			)
			return nil
		}
	}

	level.Warn(logger).Log(
		"msg", "partition reader consumed partition and current lag is lower than configured max consumer lag but higher than target consumer lag",
		"last_consumed_offset", s.committer.lastCommittedOffset,
		"current_lag", currLag,
	)
	return nil
}

func (s *ReaderService) processNextFetchesUntilLagHonored(ctx context.Context, maxLag time.Duration, logger log.Logger, recordsChan chan<- []Record, timeSince func(time.Time) time.Duration) (time.Duration, error) {
	boff := backoff.New(ctx, backoff.Config{
		MinBackoff: 100 * time.Millisecond,
		MaxBackoff: time.Second,
		MaxRetries: 0, // Retry forever (unless context is canceled / deadline exceeded).
	})
	currLag := time.Duration(0)

	for boff.Ongoing() {
		// Send a direct request to the Kafka backend to fetch the partition start offset.
		partitionStartOffset, err := s.reader.FetchPartitionOffset(ctx, kafkaStartOffset)
		if err != nil {
			level.Warn(logger).Log("msg", "partition reader failed to fetch partition start offset", "err", err)
			boff.Wait()
			continue
		}

		consumerGroupLastCommittedOffset, err := s.reader.FetchLastCommittedOffset(ctx)
		if err != nil {
			level.Warn(logger).Log("msg", "partition reader failed to fetch last committed offset", "err", err)
			boff.Wait()
			continue
		}

		// Send a direct request to the Kafka backend to fetch the last produced offset.
		// We intentionally don't use WaitNextFetchLastProducedOffset() to not introduce further
		// latency.
		lastProducedOffsetRequestedAt := time.Now()
		lastProducedOffset, err := s.reader.FetchPartitionOffset(ctx, kafkaEndOffset)
		if err != nil {
			level.Warn(logger).Log("msg", "partition reader failed to fetch last produced offset", "err", err)
			boff.Wait()
			continue
		}
		lastProducedOffset = lastProducedOffset - 1 // Kafka returns the next empty offset so we must subtract 1 to get the oldest written offset.

		level.Debug(logger).Log(
			"msg", "fetched latest offset information",
			"partition_start_offset", partitionStartOffset,
			"last_produced_offset", lastProducedOffset,
			"last_committed_offset", consumerGroupLastCommittedOffset,
		)

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
		level.Info(loggerWithCurrentLagIfSet(logger, currLag)).Log("msg", "partition reader is consuming records to honor target and max consumer lag", "partition_start_offset", partitionStartOffset, "last_produced_offset", lastProducedOffset, "last_processed_offset", s.lastProcessedOffset, "offset_lag", lastProducedOffset-s.lastProcessedOffset)

		for boff.Ongoing() {
			// Continue reading until we reached the desired offset.
			if lastProducedOffset <= s.lastProcessedOffset {
				break
			}
			if time.Since(lastProducedOffsetRequestedAt) > time.Minute {
				level.Info(loggerWithCurrentLagIfSet(logger, currLag)).Log("msg", "partition reader is still consuming records...", "last_processed_offset", s.lastProcessedOffset, "offset_lag", lastProducedOffset-s.lastProcessedOffset)
			}

			timedCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			records, err := s.reader.Poll(timedCtx, -1)
			cancel()

			if err != nil {
				level.Error(logger).Log("msg", "error polling records", "err", err)
				continue
			}
			if len(records) > 0 {
				recordsChan <- records
				s.lastProcessedOffset = records[len(records)-1].Offset
			}
		}

		if boff.Err() != nil {
			return 0, boff.ErrCause()
		}

		// If it took less than the max desired lag to replay the partition
		// then we can stop here, otherwise we'll have to redo it.
		if currLag = timeSince(lastProducedOffsetRequestedAt); currLag <= maxLag {
			return currLag, nil
		}
	}

	return 0, boff.ErrCause()
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

func loggerWithCurrentLagIfSet(logger log.Logger, currLag time.Duration) log.Logger {
	if currLag <= 0 {
		return logger
	}

	return log.With(logger, "current_lag", currLag)
}
