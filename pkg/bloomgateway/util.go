package bloomgateway

import (
	"sort"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/exp/slices"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper"
)

func getDayTime(ts model.Time) time.Time {
	return time.Date(ts.Time().Year(), ts.Time().Month(), ts.Time().Day(), 0, 0, 0, 0, time.UTC)
}

// getFromThrough assumes a list of ShortRefs sorted by From time
func getFromThrough(refs []*logproto.ShortRef) (model.Time, model.Time) {
	if len(refs) == 0 {
		return model.Earliest, model.Latest
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

// convertToSearches converts a list of line filter expressions to a list of
// byte slices that can be used with the bloom filters.
func convertToSearches(filters []syntax.LineFilter, t *v1.NGramTokenizer) [][]byte {
	searches := make([][]byte, 0, (13-t.N)*len(filters))
	for _, f := range filters {
		if f.Ty == labels.MatchEqual {
			it := t.Tokens(f.Match)
			for it.Next() {
				key := make([]byte, t.N)
				_ = copy(key, it.At())
				searches = append(searches, key)
			}
		}
	}
	return searches
}

// convertToShortRefs converts a v1.ChunkRefs into []*logproto.ShortRef
// TODO(chaudum): Avoid conversion by transferring v1.ChunkRefs in gRPC request.
func convertToShortRefs(refs v1.ChunkRefs) []*logproto.ShortRef {
	result := make([]*logproto.ShortRef, 0, len(refs))
	for _, ref := range refs {
		result = append(result, &logproto.ShortRef{From: ref.Start, Through: ref.End, Checksum: ref.Checksum})
	}
	return result
}

// convertToChunkRefs converts a []*logproto.ShortRef into v1.ChunkRefs
// TODO(chaudum): Avoid conversion by transferring v1.ChunkRefs in gRPC request.
func convertToChunkRefs(refs []*logproto.ShortRef) v1.ChunkRefs {
	result := make(v1.ChunkRefs, 0, len(refs))
	for _, ref := range refs {
		result = append(result, v1.ChunkRef{Start: ref.From, End: ref.Through, Checksum: ref.Checksum})
	}
	return result
}

// getFirstLast returns the first and last item of a fingerprint slice
// It assumes an ascending sorted list of fingerprints.
func getFirstLast[T any](s []T) (T, T) {
	var zero T
	if len(s) == 0 {
		return zero, zero
	}
	return s[0], s[len(s)-1]
}

type boundedTasks struct {
	blockRef bloomshipper.BlockRef
	tasks    []Task
}

func partitionFingerprintRange(tasks []Task, blocks []bloomshipper.BlockRef) (result []boundedTasks) {
	for _, block := range blocks {
		bounded := boundedTasks{
			blockRef: block,
		}

		for _, task := range tasks {
			refs := task.Request.Refs
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
