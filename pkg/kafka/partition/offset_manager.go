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

type OffsetManager interface {
	Topic() string
	Partition() int32
	ConsumerGroup() string

	FetchLastCommittedOffset(ctx context.Context) (int64, error)
	FetchPartitionOffset(ctx context.Context, position SpecialOffset) (int64, error)
	Commit(ctx context.Context, offset int64) error
}

var _ OffsetManager = &KafkaOffsetManager{}

type KafkaOffsetManager struct {
	client        *kgo.Client
	topic         string
	partitionID   int32
	consumerGroup string
	logger        log.Logger
}

func NewKafkaOffsetManager(
	cfg kafka.Config,
	partitionID int32,
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
		cfg.Topic,
		partitionID,
		cfg.GetConsumerGroup(instanceID, partitionID),
		logger,
	), nil
}

// newKafkaReader creates a new KafkaReader instance
func newKafkaOffsetManager(
	client *kgo.Client,
	topic string,
	partitionID int32,
	consumerGroup string,
	logger log.Logger,
) *KafkaOffsetManager {
	return &KafkaOffsetManager{
		client:        client,
		topic:         topic,
		partitionID:   partitionID,
		consumerGroup: consumerGroup,
		logger:        logger,
	}
}

// Topic returns the topic being read
func (r *KafkaOffsetManager) Topic() string {
	return r.topic
}

// Partition returns the partition being read
func (r *KafkaOffsetManager) Partition() int32 {
	return r.partitionID
}

// ConsumerGroup returns the consumer group
func (r *KafkaOffsetManager) ConsumerGroup() string {
	return r.consumerGroup
}

// FetchLastCommittedOffset retrieves the last committed offset for this partition
func (r *KafkaOffsetManager) FetchLastCommittedOffset(ctx context.Context) (int64, error) {
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
func (r *KafkaOffsetManager) FetchPartitionOffset(ctx context.Context, position SpecialOffset) (int64, error) {
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

// Commit commits an offset to the consumer group
func (r *KafkaOffsetManager) Commit(ctx context.Context, offset int64) error {
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
