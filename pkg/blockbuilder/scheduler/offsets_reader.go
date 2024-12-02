package scheduler

import (
	"context"
	"errors"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

type offsetReader struct {
	topic                string
	consumerGroup        string
	fallbackOffsetMillis int64

	adminClient *kadm.Client
}

func NewOffsetReader(topic, consumerGroup string, lookbackPeriodInMs int64, client *kgo.Client) OffsetReader {
	var fallbackOffsetMillis int64
	if lookbackPeriodInMs >= 0 {
		fallbackOffsetMillis = time.Now().UnixMilli() - lookbackPeriodInMs
	} else {
		fallbackOffsetMillis = lookbackPeriodInMs
	}

	return &offsetReader{
		topic:                topic,
		consumerGroup:        consumerGroup,
		adminClient:          kadm.NewClient(client),
		fallbackOffsetMillis: fallbackOffsetMillis,
	}
}

func (r *offsetReader) GroupLag(ctx context.Context) (map[int32]kadm.GroupMemberLag, error) {
	lag, err := GetGroupLag(ctx, r.adminClient, r.topic, r.consumerGroup, r.fallbackOffsetMillis)
	if err != nil {
		return nil, err
	}

	offsets, ok := lag[r.topic]
	if !ok {
		return nil, errors.New("no lag found for the topic")
	}

	return offsets, nil
}

func (r *offsetReader) ListOffsetsAfterMilli(ctx context.Context, ts int64) (map[int32]kadm.ListedOffset, error) {
	offsets, err := r.adminClient.ListOffsetsAfterMilli(ctx, ts, r.topic)
	if err != nil {
		return nil, err
	}

	resp, ok := offsets[r.topic]
	if !ok {
		return nil, errors.New("no offsets found for the topic")
	}

	return resp, nil
}
