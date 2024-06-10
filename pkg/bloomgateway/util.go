package bloomgateway

import (
	"sort"
	"time"

	"github.com/prometheus/common/model"
	"golang.org/x/exp/slices"

	"github.com/grafana/loki/v3/pkg/logproto"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
)

func truncateDay(ts model.Time) model.Time {
	return model.TimeFromUnix(ts.Time().Truncate(Day).Unix())
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
		return int(a.Through) - int(b.Through)
	})

	return refs[0].From, maxItem.Through
}

// convertToChunkRefs converts a []*logproto.ShortRef into v1.ChunkRefs
func convertToChunkRefs(refs []*logproto.ShortRef) v1.ChunkRefs {
	result := make(v1.ChunkRefs, 0, len(refs))
	for i := range refs {
		result = append(result, v1.ChunkRef(*refs[i]))
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
			if min == len(refs) || max == 0 || min == max {
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
	return partitionSeriesByDay(req.From, req.Through, req.Refs)
}

func partitionSeriesByDay(from, through model.Time, seriesWithChunks []*logproto.GroupedChunkRefs) []seriesWithInterval {
	result := make([]seriesWithInterval, 0)

	fromDay, throughDay := truncateDay(from), truncateDay(through)

	// because through is exclusive, if it's equal to the truncated day, it means it's the start of the day
	// and we should not include it in the range
	if through.Equal(throughDay) {
		throughDay = throughDay.Add(-24 * time.Hour)
	}

	for day := fromDay; !throughDay.Before(day); day = day.Add(Day) {
		minTs, maxTs := model.Latest, model.Earliest
		res := make([]*logproto.GroupedChunkRefs, 0, len(seriesWithChunks))

		for _, series := range seriesWithChunks {
			chunks := series.Refs

			var relevantChunks []*logproto.ShortRef
			minTs, maxTs, relevantChunks = overlappingChunks(day, day.Add(Day), minTs, maxTs, chunks)

			if len(relevantChunks) == 0 {
				continue
			}

			res = append(res, &logproto.GroupedChunkRefs{
				Fingerprint: series.Fingerprint,
				Tenant:      series.Tenant,
				Refs:        relevantChunks,
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

func overlappingChunks(from, through, minTs, maxTs model.Time, chunks []*logproto.ShortRef) (model.Time, model.Time, []*logproto.ShortRef) {

	// chunks are ordered first by `From`. Can disregard all chunks
	// that start later than the search range ends
	maxIdx := sort.Search(len(chunks), func(i int) bool {
		return chunks[i].From > through
	})

	res := make([]*logproto.ShortRef, 0, len(chunks[:maxIdx]))

	for _, chunk := range chunks[:maxIdx] {
		// if chunk ends before the search range starts, skip
		if from.After(chunk.Through) {
			continue
		}

		// Bound min & max ranges to the search range
		minTs = max(min(minTs, chunk.From), from)
		maxTs = min(max(maxTs, chunk.Through), through)
		res = append(res, chunk)
	}

	return minTs, maxTs, res
}
