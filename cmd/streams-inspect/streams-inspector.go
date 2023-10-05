package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/grafana/dskit/concurrency"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"github.com/prometheus/common/model"
)

const (
	daySinceUnixEpoc = 19400
	indexLocation    = "path to extracted index file .tsdb"
	resultFilePath   = "path to the file where the resulting json will be stored"
)

var logger = log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))

func main() {
	ctx := context.Background()

	idx, _, err := tsdb.NewTSDBIndexFromFile(indexLocation, tsdb.IndexOpts{})
	fmt.Println("index has been opened")

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
	fmt.Println("analyzing streams")
	ranges := createRanges(100)
	err = concurrency.ForEachJob(ctx, len(streams), 100, func(ctx context.Context, jobIdx int) error {
		s := streams[jobIdx]
		for _, r := range ranges {
			if uint64(s.Fingerprint) <= r.end {
				r.fingerprintsCount += 1
				break
			}
		}
		return nil
	})

	for i, r := range ranges {
		fmt.Printf("Range %d: fingerprintsCount: %d \n", i, r.fingerprintsCount)
	}
}

func createRanges(instancesCount uint64) []*fingerprintRange {
	maxUint64 := uint64(math.MaxUint64)

	rangeSize := maxUint64 / instancesCount
	remainder := maxUint64 % instancesCount

	ranges := make([]*fingerprintRange, instancesCount)
	start := uint64(0)

	for i := uint64(0); i < instancesCount; i++ {
		end := start + rangeSize
		if i < remainder {
			end++
		}
		ranges[i] = &fingerprintRange{
			start:       start,
			end:         end,
			rangeLength: end - start,
		}
		start = end
	}

	return ranges
}

type fingerprintRange struct {
	start, end        uint64
	rangeLength       uint64
	fingerprintsCount uint64
}
