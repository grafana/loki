package pattern

import (
	"context"

	"github.com/go-kit/log"
	"github.com/grafana/loki/pkg/ingester/index"
	"github.com/grafana/loki/pkg/logproto"
)

const indexShards = 32

// instance is a tenant instance of the pattern ingester.
type instance struct {
	instanceID string
	logger     log.Logger
	streams    *streamsMap
	index      *index.BitPrefixInvertedIndex
}

func newInstance(instanceID string, logger log.Logger) (*instance, error) {
	index, err := index.NewBitPrefixWithShards(indexShards)
	if err != nil {
		return nil, err
	}
	return &instance{
		logger:     logger,
		instanceID: instanceID,
		streams:    newStreamsMap(),
		index:      index,
	}, nil
}

func (i *instance) Push(ctx context.Context, req *logproto.PushRequest) error {
	return nil
}
