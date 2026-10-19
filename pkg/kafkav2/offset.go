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
	logger log.Logger
}

// NewOffsetReader returns a new OffsetReader.
func NewOffsetReader(
	client *kgo.Client,
	topic string,
	logger log.Logger,
) *OffsetReader {
	return &OffsetReader{
		adm:    kadm.NewClient(client),
		client: client,
		topic:  topic,
		logger: log.With(logger, "topic", topic),
	}
}

// LastCommittedOffset returns the last committed offset of consumerGroup for
// the partition.
func (r *OffsetReader) LastCommittedOffset(ctx context.Context, consumerGroup string, partition int32) (int64, error) {
	req := kmsg.NewPtrOffsetFetchRequest()
	req.Group = consumerGroup
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
			"consumer_group", consumerGroup,
		)
		return OffsetStart, nil
	}
	partitionRes := fetchRes.Groups[0].Topics[0].Partitions[0]
	if err := kerr.ErrorForCode(partitionRes.ErrorCode); err != nil {
		return 0, err
	}
	return partitionRes.Offset, nil
}

// ResumeOffset returns the next offset for consumerGroup to consume from the
// partition.
func (r *OffsetReader) ResumeOffset(ctx context.Context, consumerGroup string, partition int32) (int64, error) {
	lastCommittedOffset, err := r.LastCommittedOffset(ctx, consumerGroup, partition)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch last committed offset: %w", err)
	}
	initialOffset := OffsetStart
	if lastCommittedOffset >= 0 {
		initialOffset = lastCommittedOffset + 1
	}
	return initialOffset, nil
}

// StartOffsets returns the start offset of each partition, keyed by
// partition. The start offset is the offset of the oldest record that
// retention has not deleted. It equals the end offset if the partition has no
// records left.
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
// group that consumes it. It sends one request per group coordinator.
//
// The offset is negative if the group has no committed offset for its
// partition.
func (r *OffsetReader) LastCommittedOffsets(ctx context.Context, groups map[int32]string) (map[int32]int64, error) {
	offsets := make(map[int32]int64, len(groups))
	if len(groups) == 0 {
		return offsets, nil
	}
	// Name the partitions of each group, as LastCommittedOffset does, rather
	// than ask for every committed offset of the group. That keeps the load
	// on the broker low.
	partitions := make(map[string][]int32)
	for partition, group := range groups {
		offsets[partition] = OffsetStart
		partitions[group] = append(partitions[group], partition)
	}
	req := kmsg.NewPtrOffsetFetchRequest()
	for group, groupPartitions := range partitions {
		topic := kmsg.NewOffsetFetchRequestGroupTopic()
		topic.Topic = r.topic
		topic.Partitions = groupPartitions
		reqGroup := kmsg.NewOffsetFetchRequestGroup()
		reqGroup.Group = group
		reqGroup.Topics = []kmsg.OffsetFetchRequestGroupTopic{topic}
		req.Groups = append(req.Groups, reqGroup)
	}

	for _, shard := range r.client.RequestSharded(ctx, req) {
		if shard.Err != nil {
			return nil, shard.Err
		}
		fetchRes, ok := shard.Resp.(*kmsg.OffsetFetchResponse)
		if !ok {
			return nil, errors.New("unexpected response type")
		}
		for _, group := range fetchRes.Groups {
			if err := groupError(group.ErrorCode); err != nil {
				return nil, fmt.Errorf("failed to fetch offsets of group %s: %w", group.Group, err)
			}
			for _, topic := range group.Topics {
				for _, partitionRes := range topic.Partitions {
					if err := kerr.ErrorForCode(partitionRes.ErrorCode); err != nil {
						return nil, fmt.Errorf("failed to fetch offset of group %s: %w", group.Group, err)
					}
					offsets[partitionRes.Partition] = partitionRes.Offset
				}
			}
		}
	}
	return offsets, nil
}

// groupError returns the error for the error code of a group in an
// OffsetFetch response. A group that does not exist has no committed offsets,
// so GROUP_ID_NOT_FOUND is not an error.
func groupError(code int16) error {
	err := kerr.ErrorForCode(code)
	if errors.Is(err, kerr.GroupIDNotFound) {
		return nil
	}
	return err
}
