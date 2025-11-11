package partition

import (
	"context"
	"errors"
	"fmt"
	"time"

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
	ConsumerGroup() string

	LastCommittedOffset(ctx context.Context, partition int32) (int64, error)
	PartitionOffset(ctx context.Context, partition int32, position SpecialOffset) (int64, error)
	NextOffset(ctx context.Context, partition int32, t time.Time) (int64, error)
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
	c, err := client.NewReaderClient("partition-manager", cfg, log.With(logger, "component", "kafka-client"), reg)
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

// NextOffset returns the first offset after the timestamp t. If the partition
// does not have an offset after t, it returns the current end offset.
func (r *KafkaOffsetManager) NextOffset(ctx context.Context, partition int32, t time.Time) (int64, error) {
	resp, err := r.adminClient.ListOffsetsAfterMilli(ctx, t.UnixMilli(), r.cfg.Topic)
	if err != nil {
		return 0, err
	}
	// If a topic does not exist, a special -1 partition for each non-existing
	// topic is added to the response.
	partitions := resp[r.cfg.Topic]
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

// LastCommittedOffset retrieves the last committed offset for this partition
func (r *KafkaOffsetManager) LastCommittedOffset(ctx context.Context, partitionID int32) (int64, error) {
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
func (r *KafkaOffsetManager) PartitionOffset(ctx context.Context, partitionID int32, position SpecialOffset) (int64, error) {
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
