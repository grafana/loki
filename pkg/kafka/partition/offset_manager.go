package partition

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// An OffsetManager manages commit offsets for a consumer group.
type OffsetManager interface {
	// LastCommittedOffset returns the last committed offset for the partition.
	LastCommittedOffset(ctx context.Context, partition int32) (int64, error)

	// PartitionOffset returns the last produced offset for the partition.
	PartitionOffset(ctx context.Context, partition int32, position SpecialOffset) (int64, error)

	// NextOffset returns the first offset after t. If there are no offsets
	// after t, it returns the latest offset instead.
	NextOffset(ctx context.Context, partition int32, t time.Time) (int64, error)

	// Commits the offset for the partition.
	Commit(ctx context.Context, partition int32, offset int64) error
}

// Compile time check that KafkaOffsetManager implements OffsetManager.
var _ OffsetManager = &KafkaOffsetManager{}

// KafkaOffsetManager implements the [OffsetManager] interface.
type KafkaOffsetManager struct {
	admin         *kadm.Client
	client        *kgo.Client
	topic         string
	consumerGroup string
	logger        log.Logger
}

// NewKafkaOffsetManager creates a new KafkaOffsetManager.
func NewKafkaOffsetManager(
	client *kgo.Client,
	topic string,
	consumerGroup string,
	logger log.Logger,
) *KafkaOffsetManager {
	return &KafkaOffsetManager{
		admin:         kadm.NewClient(client),
		client:        client,
		topic:         topic,
		consumerGroup: consumerGroup,
		logger:        log.With(logger, "topic", topic, "consumer_group", consumerGroup),
	}
}

// NextOffset implements the [OffsetManager] interface.
func (m *KafkaOffsetManager) NextOffset(ctx context.Context, partition int32, t time.Time) (int64, error) {
	resp, err := m.admin.ListOffsetsAfterMilli(ctx, t.UnixMilli(), m.topic)
	if err != nil {
		return 0, err
	}
	// If a topic does not exist, a special -1 partition for each non-existing
	// topic is added to the response.
	partitions := resp[m.topic]
	if special, ok := partitions[-1]; ok {
		return 0, special.Err
	}
	// If a partition does not exist, it will be missing.
	listed, ok := partitions[partition]
	if !ok {
		return 0, fmt.Errorf("unknown partition %d", partition)
	}
	// Err is non-nil if the partition has a load error.
	if listed.Err != nil {
		return 0, listed.Err
	}
	return listed.Offset, nil
}

// LastCommittedOffset implements the [OffsetManager] interface.
func (m *KafkaOffsetManager) LastCommittedOffset(ctx context.Context, partitionID int32) (int64, error) {
	req := kmsg.NewPtrOffsetFetchRequest()
	req.Topics = []kmsg.OffsetFetchRequestTopic{{
		Topic:      m.topic,
		Partitions: []int32{partitionID},
	}}
	req.Group = m.consumerGroup

	resps := m.client.RequestSharded(ctx, req)

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
		level.Debug(m.logger).Log(
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

// PatitionOffset implements the [OffsetManager] interface.
func (m *KafkaOffsetManager) PartitionOffset(ctx context.Context, partitionID int32, position SpecialOffset) (int64, error) {
	partitionReq := kmsg.NewListOffsetsRequestTopicPartition()
	partitionReq.Partition = partitionID
	partitionReq.Timestamp = int64(position)

	topicReq := kmsg.NewListOffsetsRequestTopic()
	topicReq.Topic = m.topic
	topicReq.Partitions = []kmsg.ListOffsetsRequestTopicPartition{partitionReq}

	req := kmsg.NewPtrListOffsetsRequest()
	req.IsolationLevel = 0 // 0 means READ_UNCOMMITTED.
	req.Topics = []kmsg.ListOffsetsRequestTopic{topicReq}

	// Even if we share the same client, other in-flight requests are not canceled once this context is canceled
	// (or its deadline is exceeded). We've verified it with a unit test.
	resps := m.client.RequestSharded(ctx, req)

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

// Commit implements the [OffsetManager] interface.
func (m *KafkaOffsetManager) Commit(ctx context.Context, partitionID int32, offset int64) error {
	// Commit the last consumed offset.
	toCommit := kadm.Offsets{}
	toCommit.AddOffset(m.topic, partitionID, offset, -1)
	committed, err := m.admin.CommitOffsets(ctx, m.consumerGroup, toCommit)
	if err != nil {
		return err
	} else if !committed.Ok() {
		return committed.Error()
	}
	committedOffset, _ := committed.Lookup(m.topic, partitionID)
	level.Debug(m.logger).Log("msg", "last commit offset successfully committed to Kafka", "offset", committedOffset.At)
	return nil
}
