package main

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/grafana/dskit/concurrency"
	"github.com/pkg/errors"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage/stores/index/stats"
	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/storage/stores/tsdb"
)

const (
	daySinceUnixEpoc = 19400
	indexLocation    = "path to extracted index file .tsdb"
	resultFilePath   = "path to the file where the resulting json will be stored"
)

var logger = log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))

func main() {
	ctx := context.Background()

	idx, _, err := tsdb.NewTSDBIndexFromFile(indexLocation)

	if err != nil {
		level.Error(logger).Log("msg", "can not read index file", "err", err)
		return
	}
	start := time.Unix(0, 0).UTC().AddDate(0, 0, daySinceUnixEpoc)
	end := start.AddDate(0, 0, 1).Add(-1 * time.Millisecond)
	fromUnix := model.TimeFromUnix(start.Unix())
	toUnix := model.TimeFromUnix(end.Unix())
	level.Info(logger).Log("from", fromUnix.Time().UTC(), "to", toUnix.Time().UTC())
	streams, err := idx.Series(ctx, "", fromUnix, toUnix, nil, nil, labels.MustNewMatcher(labels.MatchNotEqual, "job", "non-existent"))
	if err != nil {
		level.Error(logger).Log("msg", "error while fetching all the streams", "err", err)
		return
	}

	streamsStats := make([]StreamStats, len(streams))
	indexStats := IndexStats{Streams: streamsStats}
	err = concurrency.ForEachJob(ctx, len(streams), 100, func(ctx context.Context, jobIdx int) error {
		s := streams[jobIdx]
		currentStreamMatchers := make([]*labels.Matcher, 0, len(s.Labels))
		for _, label := range s.Labels {
			currentStreamMatchers = append(currentStreamMatchers, labels.MustNewMatcher(labels.MatchEqual, label.Name, label.Value))
		}
		acc := StreamStats{Stream: s.Labels.String()}
		err = idx.Stats(ctx, "", fromUnix, toUnix, &acc, nil, func(meta index.ChunkMeta) bool {
			return true
		}, currentStreamMatchers...)
		if err != nil {
			level.Error(logger).Log("msg", "error while collecting stats for the stream", "stream", s.Labels.String(), "err", err)
			return errors.New("error while collecting stats for the stream: " + s.Labels.String())
		}

		streamsStats[jobIdx] = acc
		return nil
	})
	indexStats.StreamsCount = uint32(len(streamsStats))
	for _, stat := range streamsStats {
		indexStats.SizeKB += stat.SizeKB
		indexStats.ChunksCount += stat.ChunksCount
	}
	if err != nil {
		level.Error(logger).Log("msg", "error while processing streams concurrently", "err", err)
		return
	}

	content, err := json.MarshalIndent(indexStats, "  ", "  ")
	if err != nil {
		panic(err)
		return
	}
	err = os.WriteFile(resultFilePath, content, 0644)
	if err != nil {
		panic(err)
		return
	}
}

type ChunkStats struct {
	Start  time.Time
	End    time.Time
	SizeKB uint32
}

type StreamStats struct {
	Stream      string
	Chunks      []ChunkStats
	ChunksCount uint32
	SizeKB      uint32
}

type IndexStats struct {
	Streams      []StreamStats
	StreamsCount uint32
	ChunksCount  uint32
	SizeKB       uint32
}

func (i *StreamStats) AddStream(_ model.Fingerprint) {

}

func (i *StreamStats) AddChunk(_ model.Fingerprint, chk index.ChunkMeta) {
	i.Chunks = append(i.Chunks, ChunkStats{
		Start:  time.UnixMilli(chk.MinTime).UTC(),
		End:    time.UnixMilli(chk.MaxTime).UTC(),
		SizeKB: chk.KB,
	})
	i.ChunksCount++
	i.SizeKB += chk.KB
}

func (i StreamStats) Stats() stats.Stats {
	return logproto.IndexStatsResponse{}
}
