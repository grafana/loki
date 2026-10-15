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

// StartOffset returns the offset of the oldest record that retention has not
// deleted. It equals EndOffset if there are no records left.
func (r *OffsetReader) StartOffset(ctx context.Context, partition int32) (int64, error) {
	return r.listOffset(ctx, partition, OffsetStart)
}

// EndOffset returns the end offset.
func (r *OffsetReader) EndOffset(ctx context.Context, partition int32) (int64, error) {
	return r.listOffset(ctx, partition, OffsetEnd)
}

func (r *OffsetReader) listOffset(ctx context.Context, partition int32, timestamp int64) (int64, error) {
	partitionReq := kmsg.NewListOffsetsRequestTopicPartition()
	partitionReq.Partition = partition
	partitionReq.Timestamp = timestamp

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

// TopicOffsetReader reads the offsets of many partitions of a topic at once.
//
// Each method sends one request per broker, not one request per partition.
// StartOffsets and EndOffsets also send a metadata request to find the
// partitions of the topic.
type TopicOffsetReader struct {
	adm   *kadm.Client
	topic string
}

// NewTopicOffsetReader returns a new TopicOffsetReader.
func NewTopicOffsetReader(client *kgo.Client, topic string) *TopicOffsetReader {
	return &TopicOffsetReader{
		adm:   kadm.NewClient(client),
		topic: topic,
	}
}

// StartOffsets returns the start offset of each partition, keyed by
// partition. See [OffsetReader.StartOffset].
func (r *TopicOffsetReader) StartOffsets(ctx context.Context) (map[int32]int64, error) {
	listed, err := r.adm.ListStartOffsets(ctx, r.topic)
	return r.listedOffsets(listed, err)
}

// EndOffsets returns the end offset of each partition, keyed by partition.
func (r *TopicOffsetReader) EndOffsets(ctx context.Context) (map[int32]int64, error) {
	listed, err := r.adm.ListEndOffsets(ctx, r.topic)
	return r.listedOffsets(listed, err)
}

// listedOffsets returns an error if any partition failed. A partial result
// would make some partitions look empty.
func (r *TopicOffsetReader) listedOffsets(listed kadm.ListedOffsets, err error) (map[int32]int64, error) {
	if err != nil {
		return nil, err
	}
	if err := listed.Error(); err != nil {
		return nil, err
	}
	offsets := make(map[int32]int64, len(listed[r.topic]))
	for partition, o := range listed[r.topic] {
		offsets[partition] = o.Offset
	}
	return offsets, nil
}

// ResumeOffsets returns the next offset to consume for each partition in
// groups, keyed by partition. groups maps each partition to the consumer
// group that consumes it.
//
// The offset is [OffsetStart] if the group has no committed offset for its
// partition, as in [OffsetReader.ResumeOffset].
func (r *TopicOffsetReader) ResumeOffsets(ctx context.Context, groups map[int32]string) (map[int32]int64, error) {
	names := make([]string, 0, len(groups))
	for _, group := range groups {
		names = append(names, group)
	}
	fetched := r.adm.FetchManyOffsets(ctx, names...)

	offsets := make(map[int32]int64, len(groups))
	for partition, group := range groups {
		offsets[partition] = OffsetStart
		res, ok := fetched[group]
		if !ok || errors.Is(res.Err, kerr.GroupIDNotFound) {
			continue
		}
		if res.Err != nil {
			return nil, fmt.Errorf("failed to fetch offsets of group %s: %w", group, res.Err)
		}
		committed, ok := res.Fetched.Lookup(r.topic, partition)
		if !ok {
			continue
		}
		if committed.Err != nil {
			return nil, fmt.Errorf("failed to fetch offset of group %s: %w", group, committed.Err)
		}
		if committed.At >= 0 {
			offsets[partition] = committed.At + 1
		}
	}
	return offsets, nil
}
