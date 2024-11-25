package partition

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/multierror"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"

	"github.com/grafana/loki/v3/pkg/kafka"

	"github.com/grafana/loki/v3/pkg/kafka/client"
)

type SpecialOffset int

const (
	KafkaStartOffset SpecialOffset = -2
	KafkaEndOffset   SpecialOffset = -1
)

type Record struct {
	// Context holds the tracing (and potentially other) info, that the record was enriched with on fetch from Kafka.
	Ctx      context.Context
	TenantID string
	Content  []byte
	Offset   int64
}

type Reader interface {
	Topic() string
	Partition() int32
	ConsumerGroup() string
	FetchLastCommittedOffset(ctx context.Context) (int64, error)
	FetchPartitionOffset(ctx context.Context, position SpecialOffset) (int64, error)
	Poll(ctx context.Context, maxPollRecords int) ([]Record, error)
	Commit(ctx context.Context, offset int64) error
	// Set the target offset for consumption. reads will begin from here.
	SetOffsetForConsumption(offset int64)
}

// readerMetrics contains metrics specific to Kafka reading operations
type readerMetrics struct {
	recordsPerFetch     prometheus.Histogram
	fetchesErrors       prometheus.Counter
	fetchesTotal        prometheus.Counter
	fetchWaitDuration   prometheus.Histogram
	receiveDelay        prometheus.Histogram
	lastCommittedOffset prometheus.Gauge
}

func newReaderMetrics(r prometheus.Registerer) *readerMetrics {
	return &readerMetrics{
		fetchWaitDuration: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:                        "loki_kafka_reader_fetch_wait_duration_seconds",
			Help:                        "How long the reader spent waiting for a batch of records from Kafka.",
			NativeHistogramBucketFactor: 1.1,
		}),
		recordsPerFetch: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:    "loki_kafka_reader_records_per_fetch",
			Help:    "The number of records received in a single fetch operation.",
			Buckets: prometheus.ExponentialBuckets(1, 2, 15),
		}),
		fetchesErrors: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_kafka_reader_fetch_errors_total",
			Help: "The number of fetch errors encountered.",
		}),
		fetchesTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_kafka_reader_fetches_total",
			Help: "Total number of Kafka fetches performed.",
		}),
		receiveDelay: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_kafka_reader_receive_delay_seconds",
			Help:                            "Delay between producing a record and receiving it.",
			NativeHistogramZeroThreshold:    math.Pow(2, -10),
			NativeHistogramBucketFactor:     1.2,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 1 * time.Hour,
			Buckets:                         prometheus.ExponentialBuckets(0.125, 2, 18),
		}),
	}
}

// StdReader provides low-level access to Kafka partition reading operations
type StdReader struct {
	client        *kgo.Client
	topic         string
	partitionID   int32
	consumerGroup string
	metrics       *readerMetrics
	logger        log.Logger
}

func NewStdReader(
	cfg kafka.Config,
	partitionID int32,
	instanceID string,
	logger log.Logger,
	reg prometheus.Registerer,
) (*StdReader, error) {
	// Create a new Kafka client for this reader
	clientMetrics := client.NewReaderClientMetrics("partition-reader", reg)
	c, err := client.NewReaderClient(
		cfg,
		clientMetrics,
		log.With(logger, "component", "kafka-client"),
	)
	if err != nil {
		return nil, fmt.Errorf("creating kafka client: %w", err)
	}

	// Create the reader
	return newStdReader(
		c,
		cfg.Topic,
		partitionID,
		cfg.GetConsumerGroup(instanceID, partitionID),
		logger,
		reg,
	), nil
}

// newStdReader creates a new StdReader instance
func newStdReader(
	client *kgo.Client,
	topic string,
	partitionID int32,
	consumerGroup string,
	logger log.Logger,
	reg prometheus.Registerer,
) *StdReader {
	return &StdReader{
		client:        client,
		topic:         topic,
		partitionID:   partitionID,
		consumerGroup: consumerGroup,
		metrics:       newReaderMetrics(reg),
		logger:        logger,
	}
}

// Topic returns the topic being read
func (r *StdReader) Topic() string {
	return r.topic
}

// Partition returns the partition being read
func (r *StdReader) Partition() int32 {
	return r.partitionID
}

// ConsumerGroup returns the consumer group
func (r *StdReader) ConsumerGroup() string {
	return r.consumerGroup
}

// FetchLastCommittedOffset retrieves the last committed offset for this partition
func (r *StdReader) FetchLastCommittedOffset(ctx context.Context) (int64, error) {
	req := kmsg.NewPtrOffsetFetchRequest()
	req.Topics = []kmsg.OffsetFetchRequestTopic{{
		Topic:      r.topic,
		Partitions: []int32{r.partitionID},
	}}
	req.Group = r.consumerGroup

	resps := r.client.RequestSharded(ctx, req)

	// Since we issued a request for only 1 partition, we expect exactly 1 response.
	if expected, actual := 1, len(resps); actual != expected {
		return 0, fmt.Errorf("unexpected number of responses: %d", len(resps))
	}

	// Ensure no error occurred.
	res := resps[0]
	if res.Err != nil {
		return 0, res.Err
	}

	// Parse the response.
	fetchRes, ok := res.Resp.(*kmsg.OffsetFetchResponse)
	if !ok {
		return 0, errors.New("unexpected response type")
	}

	if len(fetchRes.Groups) != 1 ||
		len(fetchRes.Groups[0].Topics) != 1 ||
		len(fetchRes.Groups[0].Topics[0].Partitions) != 1 {
		level.Debug(r.logger).Log(
			"msg", "malformed response, setting to start offset",
		)
		return int64(KafkaStartOffset), nil
	}

	partition := fetchRes.Groups[0].Topics[0].Partitions[0]
	if err := kerr.ErrorForCode(partition.ErrorCode); err != nil {
		return 0, err
	}

	return partition.Offset, nil
}

// FetchPartitionOffset retrieves the offset for a specific position
func (r *StdReader) FetchPartitionOffset(ctx context.Context, position SpecialOffset) (int64, error) {
	partitionReq := kmsg.NewListOffsetsRequestTopicPartition()
	partitionReq.Partition = r.partitionID
	partitionReq.Timestamp = int64(position)

	topicReq := kmsg.NewListOffsetsRequestTopic()
	topicReq.Topic = r.topic
	topicReq.Partitions = []kmsg.ListOffsetsRequestTopicPartition{partitionReq}

	req := kmsg.NewPtrListOffsetsRequest()
	req.IsolationLevel = 0 // 0 means READ_UNCOMMITTED.
	req.Topics = []kmsg.ListOffsetsRequestTopic{topicReq}

	// Even if we share the same client, other in-flight requests are not canceled once this context is canceled
	// (or its deadline is exceeded). We've verified it with a unit test.
	resps := r.client.RequestSharded(ctx, req)

	// Since we issued a request for only 1 partition, we expect exactly 1 response.
	if len(resps) != 1 {
		return 0, fmt.Errorf("unexpected number of responses: %d", len(resps))
	}

	// Ensure no error occurred.
	res := resps[0]
	if res.Err != nil {
		return 0, res.Err
	}

	listRes, ok := res.Resp.(*kmsg.ListOffsetsResponse)
	if !ok {
		return 0, errors.New("unexpected response type")
	}

	if len(listRes.Topics) != 1 ||
		len(listRes.Topics[0].Partitions) != 1 {
		return 0, errors.New("malformed response")
	}

	partition := listRes.Topics[0].Partitions[0]
	if err := kerr.ErrorForCode(partition.ErrorCode); err != nil {
		return 0, err
	}

	return partition.Offset, nil
}

// Poll retrieves the next batch of records from Kafka
// Number of records fetched can be limited by configuring maxPollRecords to a non-zero value.
func (r *StdReader) Poll(ctx context.Context, maxPollRecords int) ([]Record, error) {
	start := time.Now()
	fetches := r.client.PollRecords(ctx, maxPollRecords)
	r.metrics.fetchWaitDuration.Observe(time.Since(start).Seconds())

	// Record metrics
	r.metrics.fetchesTotal.Add(float64(len(fetches)))
	var numRecords int
	fetches.EachRecord(func(record *kgo.Record) {
		numRecords++
		r.metrics.receiveDelay.Observe(time.Since(record.Timestamp).Seconds())
	})
	r.metrics.recordsPerFetch.Observe(float64(numRecords))

	// Handle errors
	var errs multierror.MultiError
	fetches.EachError(func(topic string, partition int32, err error) {
		if errors.Is(err, context.Canceled) {
			return
		}
		errs.Add(fmt.Errorf("topic %q, partition %d: %w", topic, partition, err))
	})
	if len(errs) > 0 {
		r.metrics.fetchesErrors.Add(float64(len(errs)))
		return nil, fmt.Errorf("fetch errors: %v", errs.Err())
	}

	// Build records slice
	records := make([]Record, 0, fetches.NumRecords())
	fetches.EachRecord(func(rec *kgo.Record) {
		if rec.Partition != r.partitionID {
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

	return records, nil
}

func (r *StdReader) SetOffsetForConsumption(offset int64) {
	r.client.AddConsumePartitions(map[string]map[int32]kgo.Offset{
		r.topic: {r.partitionID: kgo.NewOffset().At(offset)},
	})
}

// Commit commits an offset to the consumer group
func (r *StdReader) Commit(ctx context.Context, offset int64) error {
	admin := kadm.NewClient(r.client)

	// Commit the last consumed offset.
	toCommit := kadm.Offsets{}
	toCommit.AddOffset(r.topic, r.partitionID, offset, -1)

	committed, err := admin.CommitOffsets(ctx, r.consumerGroup, toCommit)
	if err != nil {
		return err
	} else if !committed.Ok() {
		return committed.Error()
	}

	committedOffset, _ := committed.Lookup(r.topic, r.partitionID)
	level.Debug(r.logger).Log("msg", "last commit offset successfully committed to Kafka", "offset", committedOffset.At)
	return nil
}
