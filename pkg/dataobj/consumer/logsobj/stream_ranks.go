package logsobj

import (
	"fmt"
	"slices"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

// MultiSourceRankedStreams gathers and sorts input streams from multiple sources.
// It assigns new IDs (1..N) in SortKey order for each unique sort key and maintains a mapping from source local ID to global ID.
// Therefore, equal streams from two sources share one global ID
type MultiSourceRankedStreams struct {
	ordered  []streams.SortKey // index = new ID (1..N); [0] unused
	mappings []map[int64]int64 // per source: old stream ID -> new ID
}

// RankMixedStreams extracts uniques streams from all sources, sorts them by SortKey,
// and assigns IDs according to rank. sources[i] is the localID -> stream map for one
// input object.
func RankMixedStreams(schemaLabels []string, sources ...map[int64]streams.Stream) (*MultiSourceRankedStreams, error) {
	type localRef struct {
		sourceIdx int
		localID   int64
		labelsKey string
	}

	byLabels := make(map[string]streams.SortKey)
	var allRefs []localRef
	for sourceIdx, src := range sources {
		for localID, s := range src {
			schemaKey, err := ComputeSchemaKey(s.Labels, schemaLabels)
			if err != nil {
				return nil, fmt.Errorf("computing schema key for source %d: %w", sourceIdx, err)
			}

			key := streams.NewSortKey(s.Labels, schemaKey)
			lk := s.Labels.String()
			if _, ok := byLabels[lk]; !ok {
				byLabels[lk] = key
			}
			allRefs = append(allRefs, localRef{sourceIdx: sourceIdx, localID: localID, labelsKey: lk})
		}
	}

	unique := make([]streams.SortKey, 0, len(byLabels))
	for _, u := range byLabels {
		unique = append(unique, u)
	}
	slices.SortFunc(unique, func(a, b streams.SortKey) int {
		return streams.CompareSortKey(a, b)
	})

	ranks := &MultiSourceRankedStreams{
		ordered:  make([]streams.SortKey, len(unique)+1),
		mappings: make([]map[int64]int64, len(sources)),
	}
	for i := range ranks.mappings {
		ranks.mappings[i] = make(map[int64]int64)
	}

	labelToID := make(map[string]int64, len(unique))
	for i, k := range unique {
		id := int64(i + 1)
		labelToID[k.Labels.String()] = id
		ranks.ordered[id] = k
	}
	for _, r := range allRefs {
		ranks.mappings[r.sourceIdx][r.localID] = labelToID[r.labelsKey]
	}
	return ranks, nil
}

// ByID returns the sort key assigned to new ID id.
func (r *MultiSourceRankedStreams) ByID(id int64) streams.SortKey {
	return r.ordered[id]
}

// Remap returns the old-to-new ID map for one source.
func (r *MultiSourceRankedStreams) Remap(sourceIdx int) map[int64]int64 {
	return r.mappings[sourceIdx]
}

// Resolve returns the global ID of the local stream ID
func (r *MultiSourceRankedStreams) Resolve(sourceIdx int, localID int64) int64 {
	return r.mappings[sourceIdx][localID]
}

// Size returns the number of streams held
func (r *MultiSourceRankedStreams) Size() int {
	return len(r.ordered) - 1
}
