package distributor

import (
	"fmt"

	"github.com/go-kit/log/level"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/util"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/prometheus/prometheus/model/labels"
)

const (
	// ShardLbName is the internal label to be used by Loki when dividing a stream into smaller pieces.
	// Possible values are only increasing integers starting from 0.
	ShardLbName = "__stream_shard__"
)

type NoopStreamSharder struct{}

func (s *NoopStreamSharder) ShardsFor(_ logproto.Stream) ShardIter {
	return &NoopShardIter{}
}
func (s *NoopStreamSharder) IncreaseShardsFor(_ string) {}

type NoopShardIter struct{}

func (i *NoopShardIter) NumShards() int {
	return 0
}

func (i *NoopShardIter) NextShardID() int {
	return 0
}

// shardStream shards (divides) the given stream into smaller streams.
//
// It will derive N streams (and its entries) from the given stream, where N is the sharding size for the given stream.
func shardStream(stream logproto.Stream, cfg Config, streamSharder StreamSharder, userID string) ([]uint32, []streamTracker) {
	logger := util_log.Logger
	shardsIter := streamSharder.ShardsFor(stream)

	derivedKeys := make([]uint32, 0, shardsIter.NumShards())
	derivedStreams := make([]streamTracker, 0, shardsIter.NumShards())

	if cfg.ShardStreams.Debug {
		level.Warn(logger).Log("msg", "sharding request with mode", "mode", cfg.ShardStreams.Mode)
	}

	for i := 0; i < shardsIter.NumShards(); i++ {
		idx := shardsIter.NextShardID()
		streamCopy := stream
		lbs, err := syntax.ParseLabels(streamCopy.Labels)
		if err != nil {
			level.Error(logger).Log("msg", "couldn't extract labels from stream", "stream", streamCopy.Labels)
			continue
		}
		lbs = append(lbs, labels.Label{Name: ShardLbName, Value: fmt.Sprintf("%d", idx)})
		streamCopy.Labels = lbs.String()
		streamCopy.Hash = lbs.Hash()
		if cfg.ShardStreams.Debug {
			level.Info(logger).Log("msg", "stream derived from sharding", "src-stream", stream.Labels, "derived-stream", streamCopy.Labels)
		}

		derivedKeys = append(derivedKeys, util.TokenFor(userID, streamCopy.Labels))
		derivedStreams = append(derivedStreams, streamTracker{stream: streamCopy})
	}

	return derivedKeys, derivedStreams
}
