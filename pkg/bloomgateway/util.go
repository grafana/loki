package bloomgateway

import (
	"sort"
	"time"

	"github.com/prometheus/common/model"
	"golang.org/x/exp/slices"

	"github.com/grafana/loki/pkg/logproto"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper"
)

func getDayTime(ts model.Time) time.Time {
	return ts.Time().UTC().Truncate(Day)
}

func truncateDay(ts model.Time) model.Time {
	// model.minimumTick is time.Millisecond
	return ts - (ts % model.Time(24*time.Hour/time.Millisecond))
}

// getFromThrough assumes a list of ShortRefs sorted by From time
func getFromThrough(refs []*logproto.ShortRef) (model.Time, model.Time) {
	if len(refs) == 0 {
		return model.Earliest, model.Latest
	}

	if len(refs) == 1 {
		return refs[0].From, refs[0].Through
	}

	maxItem := slices.MaxFunc(refs, func(a, b *logproto.ShortRef) int {
		if a.Through > b.Through {
			return 1
		} else if a.Through < b.Through {
			return -1
		}
		return 0
	})

	return refs[0].From, maxItem.Through
}

// convertToChunkRefs converts a []*logproto.ShortRef into v1.ChunkRefs
// TODO(chaudum): Avoid conversion by transferring v1.ChunkRefs in gRPC request.
func convertToChunkRefs(refs []*logproto.ShortRef) v1.ChunkRefs {
	result := make(v1.ChunkRefs, 0, len(refs))
	for _, ref := range refs {
		result = append(result, v1.ChunkRef{From: ref.From, Through: ref.Through, Checksum: ref.Checksum})
	}
	return result
}

type blockWithTasks struct {
	ref   bloomshipper.BlockRef
	tasks []Task
}

func partitionTasks(tasks []Task, blocks []bloomshipper.BlockRef) []blockWithTasks {
	result := make([]blockWithTasks, 0, len(blocks))

	for _, block := range blocks {
		bounded := blockWithTasks{
			ref: block,
		}

		for _, task := range tasks {
			refs := task.series
			min := sort.Search(len(refs), func(i int) bool {
				return block.Cmp(refs[i].Fingerprint) > v1.Before
			})

			max := sort.Search(len(refs), func(i int) bool {
				return block.Cmp(refs[i].Fingerprint) == v1.After
			})

			// All fingerprints fall outside of the consumer's range
			if min == len(refs) || max == 0 {
				continue
			}

			bounded.tasks = append(bounded.tasks, task.Copy(refs[min:max]))
		}

		if len(bounded.tasks) > 0 {
			result = append(result, bounded)
		}

	}
	return result
}

type seriesWithInterval struct {
	day      config.DayTime
	series   []*logproto.GroupedChunkRefs
	interval bloomshipper.Interval
}

func partitionRequest(req *logproto.FilterChunkRefRequest) []seriesWithInterval {
	result := make([]seriesWithInterval, 0)

	fromDay, throughDay := truncateDay(req.From), truncateDay(req.Through)

	for day := fromDay; day.Equal(throughDay) || day.Before(throughDay); day = day.Add(Day) {
		minTs, maxTs := model.Latest, model.Earliest
		nextDay := day.Add(Day)
		res := make([]*logproto.GroupedChunkRefs, 0, len(req.Refs))

		for _, series := range req.Refs {
			chunks := series.Refs

			min := sort.Search(len(chunks), func(i int) bool {
				return chunks[i].Through >= day
			})

			max := sort.Search(len(chunks), func(i int) bool {
				return chunks[i].From >= nextDay
			})

			// All chunks fall outside of the range
			if min == len(chunks) || max == 0 {
				continue
			}

			if chunks[min].From < minTs {
				minTs = chunks[min].From
			}
			if chunks[max-1].Through > maxTs {
				maxTs = chunks[max-1].Through
			}
			// fmt.Println("day", day, "series", series.Fingerprint, "minTs", minTs, "maxTs", maxTs)

			res = append(res, &logproto.GroupedChunkRefs{
				Fingerprint: series.Fingerprint,
				Tenant:      series.Tenant,
				Refs:        chunks[min:max],
			})

		}

		if len(res) > 0 {
			result = append(result, seriesWithInterval{
				interval: bloomshipper.Interval{
					Start: minTs,
					End:   maxTs,
				},
				day:    config.NewDayTime(day),
				series: res,
			})
		}
	}

	return result
}
