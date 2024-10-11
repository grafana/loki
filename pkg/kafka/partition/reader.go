package partition

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/coder/quartz"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/multierror"
	"github.com/grafana/dskit/services"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
	"github.com/twmb/franz-go/plugin/kprom"

	"github.com/grafana/loki/v3/pkg/kafka"
)

var errWaitTargetLagDeadlineExceeded = errors.New("waiting for target lag deadline exceeded")

const (
	kafkaStartOffset = -2
	kafkaEndOffset   = -1
)

// Reader is responsible for reading data from a specific Kafka partition
// and passing it to the consumer for processing. It is a core component of the
// Loki ingester's Kafka-based ingestion pipeline.
type Reader struct {
	services.Service

	kafkaCfg            kafka.Config
	partitionID         int32
	consumerGroup       string
	consumerFactory     ConsumerFactory
	committer           *partitionCommitter
	lastProcessedOffset int64
	recordsChan         chan []Record

	client  *kgo.Client
	logger  log.Logger
	metrics readerMetrics
	reg     prometheus.Registerer
	clock   quartz.Clock
}

type Record struct {
	// Context holds the tracing (and potentially other) info, that the record was enriched with on fetch from Kafka.
	Ctx      context.Context
	TenantID string
	Content  []byte
	Offset   int64
}

type ConsumerFactory func(committer Committer) (Consumer, error)

type Consumer interface {
	Start(ctx context.Context, recordsChan <-chan []Record) func()
}

// NewReader creates and initializes a new PartitionReader.
// It sets up the basic service and initializes the reader with the provided configuration.
func NewReader(
	kafkaCfg kafka.Config,
	partitionID int32,
	instanceID string,
	consumerFactory ConsumerFactory,
	logger log.Logger,
	reg prometheus.Registerer,
) (*Reader, error) {
	r := &Reader{
		kafkaCfg:            kafkaCfg,
		partitionID:         partitionID,
		consumerGroup:       kafkaCfg.GetConsumerGroup(instanceID, partitionID),
		logger:              logger,
		metrics:             newReaderMetrics(reg),
		reg:                 reg,
		lastProcessedOffset: -1,
		consumerFactory:     consumerFactory,
	}
	r.Service = services.NewBasicService(r.start, r.run, nil)
	return r, nil
}

// start initializes the Kafka client and committer for the PartitionReader.
// This method is called when the PartitionReader service starts.
func (p *Reader) start(ctx context.Context) error {
	var err error
	p.client, err = kafka.NewReaderClient(p.kafkaCfg, p.metrics.kprom, p.logger)
	if err != nil {
		return errors.Wrap(err, "creating kafka reader client")
	}

	// We manage our commits manually, so we must fetch the last offset for our consumer group to find out where to read from.
	lastCommittedOffset := p.fetchLastCommittedOffset(ctx)
	p.client.AddConsumePartitions(map[string]map[int32]kgo.Offset{
		p.kafkaCfg.Topic: {p.partitionID: kgo.NewOffset().At(lastCommittedOffset)},
	})

	p.committer = newCommitter(p.kafkaCfg, kadm.NewClient(p.client), p.partitionID, p.consumerGroup, p.logger, p.reg)

	if targetLag, maxLag := p.kafkaCfg.TargetConsumerLagAtStartup, p.kafkaCfg.MaxConsumerLagAtStartup; targetLag > 0 && maxLag > 0 {
		consumer, err := p.consumerFactory(p.committer)
		if err != nil {
			return fmt.Errorf("creating consumer: %w", err)
		}

		cancelCtx, cancel := context.WithCancel(ctx)
		// Temporarily start a consumer to do the initial update
		recordsChan := make(chan []Record)
		wait := consumer.Start(cancelCtx, recordsChan)
		// Shutdown the consumer after catching up. We start a new instance in the run method to tie the lifecycle to the run context.
		defer func() {
			close(recordsChan)
			cancel()
			wait()
		}()

		err = p.processNextFetchesUntilTargetOrMaxLagHonored(ctx, p.kafkaCfg.MaxConsumerLagAtStartup, p.kafkaCfg.TargetConsumerLagAtStartup, recordsChan)
		if err != nil {
			level.Error(p.logger).Log("msg", "failed to catch up to max lag", "partition", p.partitionID, "consumer_group", p.consumerGroup, "err", err)
			return err
		}
	}

	return nil
}

// run is the main loop of the PartitionReader. It continuously fetches and processes
// data from Kafka, and send it to the consumer.
func (p *Reader) run(ctx context.Context) error {
	level.Info(p.logger).Log("msg", "starting partition reader", "partition", p.partitionID, "consumer_group", p.consumerGroup)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	consumer, err := p.consumerFactory(p.committer)
	if err != nil {
		return errors.Wrap(err, "creating consumer")
	}

	recordsChan := p.startFetchLoop(ctx)
	wait := consumer.Start(ctx, recordsChan)

	wait()
	p.committer.Stop()
	return nil
}

func (p *Reader) fetchLastCommittedOffset(ctx context.Context) int64 {
	// We manually create a request so that we can request the offset for a single partition
	// only, which is more performant than requesting the offsets for all partitions.
	req := kmsg.NewPtrOffsetFetchRequest()
	req.Topics = []kmsg.OffsetFetchRequestTopic{{Topic: p.kafkaCfg.Topic, Partitions: []int32{p.partitionID}}}
	req.Group = p.consumerGroup

	resps := p.client.RequestSharded(ctx, req)

	// Since we issued a request for only 1 partition, we expect exactly 1 response.
	if expected, actual := 1, len(resps); actual != expected {
		level.Error(p.logger).Log("msg", fmt.Sprintf("unexpected number of responses (expected: %d, got: %d)", expected, actual), "expected", expected, "actual", len(resps))
		return kafkaStartOffset
	}
	// Ensure no error occurred.
	res := resps[0]
	if res.Err != nil {
		level.Error(p.logger).Log("msg", "error fetching group offset for partition", "err", res.Err)
		return kafkaStartOffset
	}

	// Parse the response.
	fetchRes, ok := res.Resp.(*kmsg.OffsetFetchResponse)
	if !ok {
		level.Error(p.logger).Log("msg", "unexpected response type")
		return kafkaStartOffset
	}
	if expected, actual := 1, len(fetchRes.Groups); actual != expected {
		level.Error(p.logger).Log("msg", fmt.Sprintf("unexpected number of groups in the response (expected: %d, got: %d)", expected, actual))
		return kafkaStartOffset
	}
	if expected, actual := 1, len(fetchRes.Groups[0].Topics); actual != expected {
		level.Error(p.logger).Log("msg", fmt.Sprintf("unexpected number of topics in the response (expected: %d, got: %d)", expected, actual))
		return kafkaStartOffset
	}
	if expected, actual := p.kafkaCfg.Topic, fetchRes.Groups[0].Topics[0].Topic; expected != actual {
		level.Error(p.logger).Log("msg", fmt.Sprintf("unexpected topic in the response (expected: %s, got: %s)", expected, actual))
		return kafkaStartOffset
	}
	if expected, actual := 1, len(fetchRes.Groups[0].Topics[0].Partitions); actual != expected {
		level.Error(p.logger).Log("msg", fmt.Sprintf("unexpected number of partitions in the response (expected: %d, got: %d)", expected, actual))
		return kafkaStartOffset
	}
	if expected, actual := p.partitionID, fetchRes.Groups[0].Topics[0].Partitions[0].Partition; actual != expected {
		level.Error(p.logger).Log("msg", fmt.Sprintf("unexpected partition in the response (expected: %d, got: %d)", expected, actual))
		return kafkaStartOffset
	}
	if err := kerr.ErrorForCode(fetchRes.Groups[0].Topics[0].Partitions[0].ErrorCode); err != nil {
		level.Error(p.logger).Log("msg", "unexpected error in the response", "err", err)
		return kafkaStartOffset
	}

	return fetchRes.Groups[0].Topics[0].Partitions[0].Offset
}

func (p *Reader) fetchPartitionOffset(ctx context.Context, position int64) (int64, error) {
	// Create a custom request to fetch the latest offset of a specific partition.
	// We manually create a request so that we can request the offset for a single partition
	// only, which is more performant than requesting the offsets for all partitions.
	partitionReq := kmsg.NewListOffsetsRequestTopicPartition()
	partitionReq.Partition = p.partitionID
	partitionReq.Timestamp = position

	topicReq := kmsg.NewListOffsetsRequestTopic()
	topicReq.Topic = p.kafkaCfg.Topic
	topicReq.Partitions = []kmsg.ListOffsetsRequestTopicPartition{partitionReq}

	req := kmsg.NewPtrListOffsetsRequest()
	req.IsolationLevel = 0 // 0 means READ_UNCOMMITTED.
	req.Topics = []kmsg.ListOffsetsRequestTopic{topicReq}

	// Even if we share the same client, other in-flight requests are not canceled once this context is canceled
	// (or its deadline is exceeded). We've verified it with a unit test.
	resps := p.client.RequestSharded(ctx, req)

	// Since we issued a request for only 1 partition, we expect exactly 1 response.
	if expected := 1; len(resps) != 1 {
		return 0, fmt.Errorf("unexpected number of responses (expected: %d, got: %d)", expected, len(resps))
	}

	// Ensure no error occurred.
	res := resps[0]
	if res.Err != nil {
		return 0, res.Err
	}

	// Parse the response.
	listRes, ok := res.Resp.(*kmsg.ListOffsetsResponse)
	if !ok {
		return 0, errors.New("unexpected response type")
	}
	if expected, actual := 1, len(listRes.Topics); actual != expected {
		return 0, fmt.Errorf("unexpected number of topics in the response (expected: %d, got: %d)", expected, actual)
	}
	if expected, actual := p.kafkaCfg.Topic, listRes.Topics[0].Topic; expected != actual {
		return 0, fmt.Errorf("unexpected topic in the response (expected: %s, got: %s)", expected, actual)
	}
	if expected, actual := 1, len(listRes.Topics[0].Partitions); actual != expected {
		return 0, fmt.Errorf("unexpected number of partitions in the response (expected: %d, got: %d)", expected, actual)
	}
	if expected, actual := p.partitionID, listRes.Topics[0].Partitions[0].Partition; actual != expected {
		return 0, fmt.Errorf("unexpected partition in the response (expected: %d, got: %d)", expected, actual)
	}
	if err := kerr.ErrorForCode(listRes.Topics[0].Partitions[0].ErrorCode); err != nil {
		return 0, err
	}

	return listRes.Topics[0].Partitions[0].Offset, nil
}

// processNextFetchesUntilTargetOrMaxLagHonored process records from Kafka until at least the maxLag is honored.
// This function does a best-effort to get lag below targetLag, but it's not guaranteed that it will be
// reached once this function successfully returns (only maxLag is guaranteed).
func (p *Reader) processNextFetchesUntilTargetOrMaxLagHonored(ctx context.Context, targetLag, maxLag time.Duration, recordsChan chan<- []Record) error {
	logger := log.With(p.logger, "target_lag", targetLag, "max_lag", maxLag)
	level.Info(logger).Log("msg", "partition reader is starting to consume partition until target and max consumer lag is honored")

	attempts := []func() (time.Duration, error){
		// First process fetches until at least the max lag is honored.
		func() (time.Duration, error) {
			return p.processNextFetchesUntilLagHonored(ctx, maxLag, logger, recordsChan, time.Since)
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

			return p.processNextFetchesUntilLagHonored(timedCtx, targetLag, logger, recordsChan, time.Since)
		},

		// If the target lag hasn't been reached with the previous attempt then we'll move on. However,
		// we still need to guarantee that in the meanwhile the lag didn't increase and max lag is still honored.
		func() (time.Duration, error) {
			return p.processNextFetchesUntilLagHonored(ctx, maxLag, logger, recordsChan, time.Since)
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
				"last_consumed_offset", p.committer.lastCommittedOffset,
				"current_lag", currLag,
			)
			return nil
		}
	}

	level.Warn(logger).Log(
		"msg", "partition reader consumed partition and current lag is lower than configured max consumer lag but higher than target consumer lag",
		"last_consumed_offset", p.committer.lastCommittedOffset,
		"current_lag", currLag,
	)
	return nil
}

func (p *Reader) processNextFetchesUntilLagHonored(ctx context.Context, maxLag time.Duration, logger log.Logger, recordsChan chan<- []Record, timeSince func(time.Time) time.Duration) (time.Duration, error) {
	boff := backoff.New(ctx, backoff.Config{
		MinBackoff: 100 * time.Millisecond,
		MaxBackoff: time.Second,
		MaxRetries: 0, // Retry forever (unless context is canceled / deadline exceeded).
	})
	currLag := time.Duration(0)

	for boff.Ongoing() {
		// Send a direct request to the Kafka backend to fetch the partition start offset.
		partitionStartOffset, err := p.fetchPartitionOffset(ctx, kafkaStartOffset)
		if err != nil {
			level.Warn(logger).Log("msg", "partition reader failed to fetch partition start offset", "err", err)
			boff.Wait()
			continue
		}

		// Send a direct request to the Kafka backend to fetch the last produced offset.
		// We intentionally don't use WaitNextFetchLastProducedOffset() to not introduce further
		// latency.
		lastProducedOffsetRequestedAt := time.Now()
		lastProducedOffset, err := p.fetchPartitionOffset(ctx, kafkaEndOffset)
		if err != nil {
			level.Warn(logger).Log("msg", "partition reader failed to fetch last produced offset", "err", err)
			boff.Wait()
			continue
		}
		lastProducedOffset = lastProducedOffset - 1 // Kafka returns the next empty offset so we must subtract 1 to get the oldest written offset.

		// Ensure there are some records to consume. For example, if the partition has been inactive for a long
		// time and all its records have been deleted, the partition start offset may be > 0 but there are no
		// records to actually consume.
		if partitionStartOffset > lastProducedOffset {
			level.Info(logger).Log("msg", "partition reader found no records to consume because partition is empty", "partition_start_offset", partitionStartOffset, "last_produced_offset", lastProducedOffset)
			return 0, nil
		}

		// This message is NOT expected to be logged with a very high rate. In this log we display the last measured
		// lag. If we don't have it (lag is zero value), then it will not be logged.
		level.Info(loggerWithCurrentLagIfSet(logger, currLag)).Log("msg", "partition reader is consuming records to honor target and max consumer lag", "partition_start_offset", partitionStartOffset, "last_produced_offset", lastProducedOffset)

		for boff.Ongoing() {
			// Continue reading until we reached the desired offset.
			if lastProducedOffset <= p.lastProcessedOffset {
				break
			}

			records := p.poll(ctx)
			recordsChan <- records
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

func loggerWithCurrentLagIfSet(logger log.Logger, currLag time.Duration) log.Logger {
	if currLag <= 0 {
		return logger
	}

	return log.With(logger, "current_lag", currLag)
}

func (p *Reader) startFetchLoop(ctx context.Context) chan []Record {
	records := make(chan []Record)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				records <- p.poll(ctx)
			}
		}
	}()
	return records
}

// logFetchErrors logs any errors encountered during the fetch operation.
func (p *Reader) logFetchErrors(fetches kgo.Fetches) {
	mErr := multierror.New()
	fetches.EachError(func(topic string, partition int32, err error) {
		if errors.Is(err, context.Canceled) {
			return
		}

		// kgo advises to "restart" the kafka client if the returned error is a kerr.Error.
		// Recreating the client would cause duplicate metrics registration, so we don't do it for now.
		mErr.Add(fmt.Errorf("topic %q, partition %d: %w", topic, partition, err))
	})
	if len(mErr) == 0 {
		return
	}
	p.metrics.fetchesErrors.Add(float64(len(mErr)))
	level.Error(p.logger).Log("msg", "encountered error while fetching", "err", mErr.Err())
}

// pollFetches retrieves the next batch of records from Kafka and measures the fetch duration.
func (p *Reader) poll(ctx context.Context) []Record {
	defer func(start time.Time) {
		p.metrics.fetchWaitDuration.Observe(time.Since(start).Seconds())
	}(time.Now())
	fetches := p.client.PollFetches(ctx)
	p.recordFetchesMetrics(fetches)
	p.logFetchErrors(fetches)
	fetches = filterOutErrFetches(fetches)
	if fetches.NumRecords() == 0 {
		return nil
	}
	records := make([]Record, 0, fetches.NumRecords())
	fetches.EachRecord(func(rec *kgo.Record) {
		if rec.Partition != p.partitionID {
			level.Error(p.logger).Log("msg", "wrong partition record received", "partition", rec.Partition, "expected_partition", p.partitionID)
			return
		}
		records = append(records, Record{
			// This context carries the tracing data for this individual record;
			// kotel populates this data when it fetches the messages.
			Ctx:      rec.Context,
			TenantID: string(rec.Key),
			Content:  rec.Value,
			Offset:   rec.Offset,
		})
	})
	p.lastProcessedOffset = records[len(records)-1].Offset
	return records
}

// recordFetchesMetrics updates various metrics related to the fetch operation.
func (p *Reader) recordFetchesMetrics(fetches kgo.Fetches) {
	var (
		now        = time.Now()
		numRecords = 0
	)
	fetches.EachRecord(func(record *kgo.Record) {
		numRecords++
		delay := now.Sub(record.Timestamp).Seconds()
		if p.lastProcessedOffset == -1 {
			p.metrics.receiveDelayWhenStarting.Observe(delay)
		} else {
			p.metrics.receiveDelayWhenRunning.Observe(delay)
		}
	})

	p.metrics.fetchesTotal.Add(float64(len(fetches)))
	p.metrics.recordsPerFetch.Observe(float64(numRecords))
}

// filterOutErrFetches removes any fetches that resulted in errors from the provided slice.
func filterOutErrFetches(fetches kgo.Fetches) kgo.Fetches {
	filtered := make(kgo.Fetches, 0, len(fetches))
	for i, fetch := range fetches {
		if !isErrFetch(fetch) {
			filtered = append(filtered, fetches[i])
		}
	}

	return filtered
}

// isErrFetch checks if a given fetch resulted in any errors.
func isErrFetch(fetch kgo.Fetch) bool {
	for _, t := range fetch.Topics {
		for _, p := range t.Partitions {
			if p.Err != nil {
				return true
			}
		}
	}
	return false
}

type readerMetrics struct {
	receiveDelayWhenStarting prometheus.Observer
	receiveDelayWhenRunning  prometheus.Observer
	recordsPerFetch          prometheus.Histogram
	fetchesErrors            prometheus.Counter
	fetchesTotal             prometheus.Counter
	fetchWaitDuration        prometheus.Histogram
	// strongConsistencyInstrumentation *StrongReadConsistencyInstrumentation[struct{}]
	// lastConsumedOffset               prometheus.Gauge
	consumeLatency prometheus.Histogram
	kprom          *kprom.Metrics
}

// newReaderMetrics initializes and returns a new set of metrics for the PartitionReader.
func newReaderMetrics(reg prometheus.Registerer) readerMetrics {
	receiveDelay := promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
		Name:                            "loki_ingest_storage_reader_receive_delay_seconds",
		Help:                            "Delay between producing a record and receiving it in the consumer.",
		NativeHistogramZeroThreshold:    math.Pow(2, -10), // Values below this will be considered to be 0. Equals to 0.0009765625, or about 1ms.
		NativeHistogramBucketFactor:     1.2,              // We use higher factor (scheme=2) to have wider spread of buckets.
		NativeHistogramMaxBucketNumber:  100,
		NativeHistogramMinResetDuration: 1 * time.Hour,
		Buckets:                         prometheus.ExponentialBuckets(0.125, 2, 18), // Buckets between 125ms and 9h.
	}, []string{"phase"})

	return readerMetrics{
		receiveDelayWhenStarting: receiveDelay.WithLabelValues("starting"),
		receiveDelayWhenRunning:  receiveDelay.WithLabelValues("running"),
		kprom:                    kafka.NewReaderClientMetrics("partition-reader", reg),
		fetchWaitDuration: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:                        "loki_ingest_storage_reader_records_batch_wait_duration_seconds",
			Help:                        "How long a consumer spent waiting for a batch of records from the Kafka client. If fetching is faster than processing, then this will be close to 0.",
			NativeHistogramBucketFactor: 1.1,
		}),
		recordsPerFetch: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:    "loki_ingest_storage_reader_records_per_fetch",
			Help:    "The number of records received by the consumer in a single fetch operation.",
			Buckets: prometheus.ExponentialBuckets(1, 2, 15),
		}),
		fetchesErrors: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingest_storage_reader_fetch_errors_total",
			Help: "The number of fetch errors encountered by the consumer.",
		}),
		fetchesTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingest_storage_reader_fetches_total",
			Help: "Total number of Kafka fetches received by the consumer.",
		}),
	}
}
