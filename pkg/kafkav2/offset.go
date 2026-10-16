package kafkav2

import (
	"context"
	"errors"
	"fmt"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// Special offsets in Kafka that refer to the start or end offset for
// a partition.
const (
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
	adm    *kadm.Client
	client *kgo.Client
	topic  string
}

// NewOffsetReader returns a new OffsetReader.
func NewOffsetReader(client *kgo.Client, topic string) *OffsetReader {
	return &OffsetReader{
		adm:    kadm.NewClient(client),
		client: client,
		topic:  topic,
	}
}

// ResumeOffset returns the next offset for consumerGroup to consume from the
// partition. It returns [OffsetStart] if the group has no committed offset for
// the partition.
func (r *OffsetReader) ResumeOffset(ctx context.Context, consumerGroup string, partition int32) (int64, error) {
	offsets, err := r.LastCommittedOffsets(ctx, map[int32]string{partition: consumerGroup})
	if err != nil {
		return 0, fmt.Errorf("failed to fetch last committed offset: %w", err)
	}
	if offsets[partition] < 0 {
		return OffsetStart, nil
	}
	return offsets[partition] + 1, nil
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

// StartOffsets returns the start offset of each partition, keyed by
// partition. See [OffsetReader.StartOffset].
func (r *OffsetReader) StartOffsets(ctx context.Context) (map[int32]int64, error) {
	listed, err := r.adm.ListStartOffsets(ctx, r.topic)
	return r.listedOffsets(listed, err)
}

// EndOffsets returns the end offset of each partition, keyed by partition.
func (r *OffsetReader) EndOffsets(ctx context.Context) (map[int32]int64, error) {
	listed, err := r.adm.ListEndOffsets(ctx, r.topic)
	return r.listedOffsets(listed, err)
}

// listedOffsets returns an error if any partition failed. A partial result
// would make some partitions look empty.
func (r *OffsetReader) listedOffsets(listed kadm.ListedOffsets, err error) (map[int32]int64, error) {
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

// LastCommittedOffsets returns the last committed offset for each partition
// in groups, keyed by partition. groups maps each partition to the consumer
// group that consumes it.
//
// The offset is negative if the group has no committed offset for its
// partition.
func (r *OffsetReader) LastCommittedOffsets(ctx context.Context, groups map[int32]string) (map[int32]int64, error) {
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
		offsets[partition] = committed.At
	}
	return offsets, nil
}
