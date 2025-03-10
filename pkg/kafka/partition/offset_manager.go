package partition

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
)

// Partition level metadata in a more easily digestible form than what Kafka provides
type Lag struct {
	// First Available Offset in retention
	startOffset int64
	// Exclusive; the next available offset (as of yet unwritten)
	endOffset int64
	// Last committed offset
	committedOffset int64
	// rawLag measures how far behind the most recently committed offset is from the current offset.
	// In special cases, this can be positive even when there are no more records to process,
	// which happens when there is a gap between the last committed offset and the current offset, but
	// it is out of retention (unrecoverable).
	rawLag int64
}

func NewLag(startOffset, endOffset, committedOffset, rawLag int64) Lag {
	return Lag{
		startOffset:     startOffset,
		endOffset:       endOffset,
		committedOffset: committedOffset,
		rawLag:          rawLag,
	}
}

// FirstUncommittedOffset returns the first offset that has not yet been committed
func (l Lag) FirstUncommittedOffset() int64 {
	// startOffset is the previously-committed offset, so we need to start processing the first
	// _uncommitted_ offset
	return max(l.committedOffset+1, l.startOffset)
}

func (l Lag) LastCommittedOffset() int64 {
	return l.committedOffset
}

// NextAvailableOffset returns the next unwritten offset in a partition,
// i.e. the end offset (exclusive)
func (l Lag) NextAvailableOffset() int64 {
	return l.endOffset
}

// Lag returns the difference between the last produced offset
// and the first Uncommitted (but available) offset
func (l Lag) Lag() int64 {
	return l.endOffset - l.FirstUncommittedOffset()
}

type OffsetManager interface {
	Topic() string
	ConsumerGroup() string

	// GroupLag returns the lag for the consumer group. Uses fallbackOffsetMillis to calculate the lag if the consumer group has no commits.
	GroupLag(ctx context.Context, fallbackOffsetMillis int64) (map[int32]Lag, error)
	FetchLastCommittedOffset(ctx context.Context, partition int32) (int64, error)
	FetchPartitionOffset(ctx context.Context, partition int32, position SpecialOffset) (int64, error)
	Commit(ctx context.Context, partition int32, offset int64) error
}

var _ OffsetManager = &KafkaOffsetManager{}

type KafkaOffsetManager struct {
	client      *kgo.Client
	adminClient *kadm.Client
	cfg         kafka.Config
	instanceID  string
	logger      log.Logger
}

func NewKafkaOffsetManager(
	cfg kafka.Config,
	instanceID string,
	logger log.Logger,
	reg prometheus.Registerer,
) (*KafkaOffsetManager, error) {
	// Create a new Kafka client for the partition manager.
	clientMetrics := client.NewReaderClientMetrics("partition-manager", reg)
	c, err := client.NewReaderClient(
		cfg,
		clientMetrics,
		log.With(logger, "component", "kafka-client"),
	)
	if err != nil {
		return nil, fmt.Errorf("creating kafka client: %w", err)
	}

	return newKafkaOffsetManager(
		c,
		cfg,
		instanceID,
		logger,
	), nil
}

// newKafkaReader creates a new KafkaReader instance
func newKafkaOffsetManager(
	client *kgo.Client,
	cfg kafka.Config,
	instanceID string,
	logger log.Logger,
) *KafkaOffsetManager {
	return &KafkaOffsetManager{
		client:      client,
		adminClient: kadm.NewClient(client),
		cfg:         cfg,
		instanceID:  instanceID,
		logger:      logger,
	}
}

// Topic returns the topic being read
func (r *KafkaOffsetManager) Topic() string {
	return r.cfg.Topic
}

func (r *KafkaOffsetManager) ConsumerGroup() string {
	return r.cfg.GetConsumerGroup(r.instanceID)
}

// FetchLastCommittedOffset retrieves the last committed offset for this partition
func (r *KafkaOffsetManager) FetchLastCommittedOffset(ctx context.Context, partitionID int32) (int64, error) {
	req := kmsg.NewPtrOffsetFetchRequest()
	req.Topics = []kmsg.OffsetFetchRequestTopic{{
		Topic:      r.cfg.Topic,
		Partitions: []int32{partitionID},
	}}
	req.Group = r.ConsumerGroup()

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
func (r *KafkaOffsetManager) FetchPartitionOffset(ctx context.Context, partitionID int32, position SpecialOffset) (int64, error) {
	partitionReq := kmsg.NewListOffsetsRequestTopicPartition()
	partitionReq.Partition = partitionID
	partitionReq.Timestamp = int64(position)

	topicReq := kmsg.NewListOffsetsRequestTopic()
	topicReq.Topic = r.cfg.Topic
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

// GroupLag returns the lag for the consumer group. Uses fallbackOffsetMillis to calculate the lag if the consumer group has no commits.
func (r *KafkaOffsetManager) GroupLag(ctx context.Context, fallbackOffsetMillis int64) (map[int32]Lag, error) {
	lag, err := GetGroupLag(ctx, r.adminClient, r.cfg.Topic, r.ConsumerGroup(), fallbackOffsetMillis)
	if err != nil {
		return nil, err
	}

	offsets, ok := lag[r.cfg.Topic]
	if !ok {
		return nil, errors.New("no lag found for the topic")
	}

	res := make(map[int32]Lag, len(offsets))

	for partition, partitionOffset := range offsets {
		res[partition] = Lag{
			// 1. kadm.GroupMemberLag contains valid Commit.At even when consumer group never committed any offset.
			//    no additional validation is needed here
			// 2. committed offset could be behind start offset if we are falling behind retention period.

			// startOffset is the previously-committed offset, so we need to start processing the first
			// _uncommitted_ offset
			startOffset: max(partitionOffset.Commit.At+1, partitionOffset.Start.Offset),
			// endOffset is initially the next available offset: this is why we treat jobs as end-exclusive:
			// so we won't try polling forever to a partition that won't have any more records
			endOffset:       partitionOffset.End.Offset,
			committedOffset: partitionOffset.Commit.At,
			rawLag:          partitionOffset.Lag,
		}
	}

	return res, nil
}

// Commit commits an offset to the consumer group
func (r *KafkaOffsetManager) Commit(ctx context.Context, partitionID int32, offset int64) error {
	admin := kadm.NewClient(r.client)

	// Commit the last consumed offset.
	toCommit := kadm.Offsets{}
	toCommit.AddOffset(r.cfg.Topic, partitionID, offset, -1)

	committed, err := admin.CommitOffsets(ctx, r.ConsumerGroup(), toCommit)
	if err != nil {
		return err
	} else if !committed.Ok() {
		return committed.Error()
	}

	committedOffset, _ := committed.Lookup(r.cfg.Topic, partitionID)
	level.Debug(r.logger).Log("msg", "last commit offset successfully committed to Kafka", "offset", committedOffset.At)
	return nil
}
