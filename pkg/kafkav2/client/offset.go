package client

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

const (
	// Special offsets in Kafka that refer to the start or end offset for
	// a partition.
	OffsetStart = -2
	OffsetEnd   = -1
)

// A Committer commits offsets.
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

// A GroupCommitter commits offsets for a consumer group.
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

type commitWithPromise struct {
	topic         string
	partition     int32
	consumerGroup string
	offset        int64
	promise       func(error)
}

// An AsyncCommitter commits offsets asynchronously.
type AsyncCommitter struct {
	client *kadm.Client
	jobs   chan commitWithPromise
}

// NewAsyncCommitter returns a new AsyncCommitter. Up to n jobs can be
// queued at a time.
func NewAsyncCommitter(client *kadm.Client, n int64) *AsyncCommitter {
	return &AsyncCommitter{
		client: client,
		jobs:   make(chan commitWithPromise, n),
	}
}

// Run the committer loop until the context is cancelled.
func (c *AsyncCommitter) Run(ctx context.Context) error {
	for {
		select {
		case job := <-c.jobs:
			offsets := kadm.Offsets{}
			offsets.AddOffset(job.topic, job.partition, job.offset, -1)
			committed, err := c.client.CommitOffsets(ctx, job.consumerGroup, offsets)
			if err != nil {
				job.promise(err)
			} else if !committed.Ok() {
				job.promise(committed.Error())
			} else {
				job.promise(nil)
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// CommitAsync the offset and call the callback on completion. If the offset
// could not be committed then an error is passed to the callback.
func (c *AsyncCommitter) CommitAsync(
	ctx context.Context,
	topic string,
	partition int32,
	consumerGroup string,
	offset int64,
	callback func(err error),
) {
	select {
	case <-ctx.Done():
		callback(ctx.Err())
	case c.jobs <- commitWithPromise{
		topic:         topic,
		partition:     partition,
		consumerGroup: consumerGroup,
		offset:        offset,
		promise:       callback,
	}:
	}
}

// An AsyncGroupCommitter commits offsets for a consumer group asynchronously.
type AsyncGroupCommitter struct {
	*AsyncCommitter
	topic         string
	consumerGroup string
}

func NewAsyncGroupCommitter(client *kadm.Client, topic string, consumerGroup string, n int64) *AsyncGroupCommitter {
	return &AsyncGroupCommitter{
		topic:          topic,
		consumerGroup:  consumerGroup,
		AsyncCommitter: NewAsyncCommitter(client, n),
	}
}

// CommitAsync the offset and call the callback on completion. If the offset
// could not be committed then an error is passed to the callback.
func (c *AsyncGroupCommitter) CommitAsync(
	ctx context.Context,
	partition int32,
	offset int64,
	callback func(err error),
) {
	select {
	case <-ctx.Done():
		callback(ctx.Err())
	case c.jobs <- commitWithPromise{
		topic:         c.topic,
		partition:     partition,
		consumerGroup: c.consumerGroup,
		offset:        offset,
		promise:       callback,
	}:
	}
}

// OffsetReader reads commit offsets.
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
		return int64(OffsetStart), nil
	}
	partitionRes := fetchRes.Groups[0].Topics[0].Partitions[0]
	if err := kerr.ErrorForCode(partitionRes.ErrorCode); err != nil {
		return 0, err
	}
	return partitionRes.Offset, nil
}

// NextOffset returns the first offset at or after t. If there are no offsets
// at or after t, it returns the current end offset instead.
func (r *OffsetReader) NextOffset(ctx context.Context, partition int32, t time.Time) (int64, error) {
	resp, err := r.adm.ListOffsetsAfterMilli(ctx, t.UnixMilli(), r.topic)
	if err != nil {
		return 0, err
	}
	// If a topic does not exist, a special -1 partition for each non-existing
	// topic is added to the response.
	partitions := resp[r.topic]
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

// PartitionOffset returns the last produced offset for the partition.
func (r *OffsetReader) PartitionOffset(ctx context.Context, partition int32, position int64) (int64, error) {
	partitionReq := kmsg.NewListOffsetsRequestTopicPartition()
	partitionReq.Partition = partition
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

	partitionRes := listRes.Topics[0].Partitions[0]
	if err := kerr.ErrorForCode(partitionRes.ErrorCode); err != nil {
		return 0, err
	}
	return partitionRes.Offset, nil
}
