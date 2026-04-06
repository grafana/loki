package kafkav2

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

const (
	// Special offsets in Kafka that refer to the start or end offset for
	// a partition.
	OffsetStart = int64(-2)
	OffsetEnd   = int64(-1)
)

type Committer struct {
	client *kadm.Client
}

// NewCommitter returns a new Committer.
func NewCommitter(client *kadm.Client) *Committer {
	return &Committer{
		client: client,
	}
}

// Commit commits the offset. It returns an error if the offset could not
// be committed.
func (c *Committer) Commit(ctx context.Context, topic string, partition int32, consumerGroup string, offset int64) error {
	offsets := kadm.Offsets{}
	offsets.AddOffset(topic, partition, offset, -1)
	committed, err := c.client.CommitOffsets(ctx, consumerGroup, offsets)
	if err != nil {
		return err
	}
	if !committed.Ok() {
		return committed.Error()
	}
	return nil
}

type GroupCommitter struct {
	topic         string
	consumerGroup string
	*Committer
}

// NewGroupCommitter returns a new GroupCommitter.
func NewGroupCommitter(client *kadm.Client, topic string, consumerGroup string) *GroupCommitter {
	return &GroupCommitter{
		topic:         topic,
		consumerGroup: consumerGroup,
		Committer:     NewCommitter(client),
	}
}

// Commit commits the offset. It returns an error if the offset could not
// be committed.
func (c *GroupCommitter) Commit(ctx context.Context, partition int32, offset int64) error {
	return c.Committer.Commit(ctx, c.topic, partition, c.consumerGroup, offset)
}

type OffsetReader struct {
	adm           *kadm.Client
	client        *kgo.Client
	topic         string
	consumerGroup string
	logger        log.Logger
}

// NewOffsetReader returns a new OffsetReader.
func NewOffsetReader(
	client *kgo.Client,
	topic string,
	consumerGroup string,
	logger log.Logger,
) *OffsetReader {
	return &OffsetReader{
		adm:           kadm.NewClient(client),
		client:        client,
		topic:         topic,
		consumerGroup: consumerGroup,
		logger:        log.With(logger, "topic", topic, "consumer_group", consumerGroup),
	}
}

// LastCommittedOffset returns the last committed offset for the partition.
func (r *OffsetReader) LastCommittedOffset(ctx context.Context, partition int32) (int64, error) {
	req := kmsg.NewPtrOffsetFetchRequest()
	req.Group = r.consumerGroup
	req.Topics = []kmsg.OffsetFetchRequestTopic{{
		Topic:      r.topic,
		Partitions: []int32{partition},
	}}
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
		return OffsetStart, nil
	}
	partitionRes := fetchRes.Groups[0].Topics[0].Partitions[0]
	if err := kerr.ErrorForCode(partitionRes.ErrorCode); err != nil {
		return 0, err
	}
	return partitionRes.Offset, nil
}

// ResumeOffset returns the next offset to consume.
func (r *OffsetReader) ResumeOffset(ctx context.Context, partition int32) (int64, error) {
	lastCommittedOffset, err := r.LastCommittedOffset(ctx, partition)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch last committed offset: %w", err)
	}
	initialOffset := OffsetStart
	if lastCommittedOffset >= 0 {
		initialOffset = lastCommittedOffset + 1
	}
	return initialOffset, nil
}

// EndOffset returns the end offset.
func (r *OffsetReader) EndOffset(ctx context.Context, partition int32) (int64, error) {
	partitionReq := kmsg.NewListOffsetsRequestTopicPartition()
	partitionReq.Partition = partition
	partitionReq.Timestamp = OffsetEnd

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

	partitionRes := listRes.Topics[0].Partitions[0]
	if err := kerr.ErrorForCode(partitionRes.ErrorCode); err != nil {
		return 0, err
	}
	return partitionRes.Offset, nil
}
