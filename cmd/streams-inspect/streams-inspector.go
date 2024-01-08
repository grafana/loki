package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/grafana/loki/pkg/storage/stores/index/seriesvolume"
	stream_inspector "github.com/grafana/loki/pkg/stream-inspector"
	"math"
	"os"
	"strings"
	"time"

	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/storage/stores/tsdb"
)

const (
	daySinceUnixEpoc = 19610
	indexLocation    = "<PATH_TO_TSDB_FILE>"
	resultFilePath   = "<PATH_TO_THE_FILE_TO_WRITE_THE_RESULTS_JSON>"
)

var logger = log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))

func main() {
	ctx := context.Background()

	idx, _, err := tsdb.NewTSDBIndexFromFile(indexLocation, tsdb.IndexOpts{})

	if err != nil {
		level.Error(logger).Log("msg", "can not read index file", "err", err)
		return
	}
	//first 6 hours
	start := time.Unix(0, 0).UTC().AddDate(0, 0, daySinceUnixEpoc)
	end := start.AddDate(0, 0, 1).Add(-18 * time.Hour)
	var streamMatchers []*labels.Matcher
	//streamMatchers = []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "job", "hosted-grafana/grafana"), labels.MustNewMatcher(labels.MatchEqual, "cluster", "prod-us-central-0"), labels.MustNewMatcher(labels.MatchEqual, "container", "grafana")}
	limit := int32(5000)

	leftTrees, err := buildTrees(start, end, limit, idx, ctx, streamMatchers)
	if err != nil {
		fmt.Println("err", err)
		return
	}
	//last 6 hours
	start = start.Add(18 * time.Hour)
	end = start.Add(6 * time.Hour)
	rightTrees, err := buildTrees(start, end, limit, idx, ctx, streamMatchers)
	if err != nil {
		fmt.Println("err", err)
		return
	}

	level.Info(logger).Log("msg", "starting building flamegraph model")
	converter := stream_inspector.FlamegraphConverter{Left: leftTrees, Right: rightTrees, Mode: stream_inspector.Diff}
	flameBearer := converter.CovertTrees()
	level.Info(logger).Log("msg", "completed building flamegraph model")

	level.Info(logger).Log("msg", "starting writing json")
	content, err := json.Marshal(flameBearer)
	if err != nil {
		panic(err)
		return
	}
	_, err = os.Stat(resultFilePath)
	if err == nil {
		level.Info(logger).Log("msg", "results file already exists. deleting previous one.")
		err := os.Remove(resultFilePath)
		if err != nil {
			panic(err)
			return
		}
	}
	err = os.WriteFile(resultFilePath, content, 0644)
	if err != nil {
		panic(err)
		return
	}
}

func buildTrees(start time.Time, end time.Time, limit int32, idx tsdb.Index, ctx context.Context, streamMatchers []*labels.Matcher) ([]*stream_inspector.Tree, error) {
	fromUnix := model.TimeFromUnix(start.Unix())
	toUnix := model.TimeFromUnix(end.Unix())
	level.Info(logger).Log("msg", "starting extracting volumes")
	level.Info(logger).Log("from", fromUnix.Time().UTC(), "to", toUnix.Time().UTC())
	accumulator := seriesvolume.NewAccumulator(limit, math.MaxInt32)
	err := idx.Volume(ctx, "", fromUnix, toUnix, accumulator, nil, func(meta index.ChunkMeta) bool {
		return true
	}, nil, seriesvolume.Series, append(streamMatchers, labels.MustNewMatcher(labels.MatchNotEqual, "", "non-existent"))...)
	if err != nil {
		level.Error(logger).Log("msg", "error while fetching all the streams", "err", err)
		return nil, fmt.Errorf("error while fetching all the streams: %w", err)
	}
	volumes := accumulator.Volumes().GetVolumes()
	streams := make([]stream_inspector.StreamWithVolume, 0, len(volumes))
	for _, volume := range volumes {
		labelsString := strings.Trim(volume.Name, "{}")
		pairs := strings.Split(labelsString, ", ")
		lbls := make([]string, 0, len(pairs)*2)
		for _, pair := range pairs {
			lblVal := strings.Split(pair, "=")
			lbls = append(lbls, lblVal[0])
			lbls = append(lbls, strings.Trim(lblVal[1], "\""))
		}
		streams = append(streams, stream_inspector.StreamWithVolume{
			Labels: labels.FromStrings(lbls...),
			Volume: float64(volume.Volume),
		})
	}

	level.Info(logger).Log("msg", "completed extracting volumes")

	level.Info(logger).Log("msg", "starting building trees")
	inspector := stream_inspector.Inspector{}
	trees, err := inspector.BuildTrees(streams, streamMatchers)
	if err != nil {
		level.Error(logger).Log("msg", "error while building trees", "err", err)
		return nil, fmt.Errorf("error while building trees: %w", err)
	}
	level.Info(logger).Log("msg", "completed building trees")
	return trees, nil
}
