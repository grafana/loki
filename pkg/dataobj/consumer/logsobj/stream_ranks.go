package logsobj

import (
	"fmt"
	"slices"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

// RankedStream is one unique label set after ranking.
type RankedStream struct {
	Stream streams.Stream
	Key    StreamOrderKey
}

// Shard returns the stream's physical shard bucket.
func (s RankedStream) Shard() uint32 { return uint32(s.Stream.ShardBucket) }

// StreamRanks assigns dense IDs 1..N in StreamOrderKey order to unique label
// sets from one or more source stream maps. Same labels in two sources share
// one ID so a merge by remapped stream ID timestamp-interleaves them.
type StreamRanks struct {
	byNewID []RankedStream    // index = new ID (1..N); [0] unused
	remap   []map[int64]int64 // per source: old stream ID -> new ID
}

// RankStreams uniques streams by full labels, sorts them by StreamOrderKey,
// and assigns dense IDs. sources[i] is the localID -> stream map for one
// input object.
func RankStreams(schemaLabels []string, sources ...map[int64]streams.Stream) (*StreamRanks, error) {
	type uniqStream struct {
		key    StreamOrderKey
		stream streams.Stream
	}
	type localRef struct {
		sourceIdx int
		localID   int64
		labelsKey string
	}

	byLabels := make(map[string]*uniqStream)
	var allRefs []localRef
	for sourceIdx, src := range sources {
		for localID, s := range src {
			key, err := NewStreamOrderKey(s.Labels, schemaLabels)
			if err != nil {
				return nil, fmt.Errorf("computing sort key for source %d: %w", sourceIdx, err)
			}
			lk := s.Labels.String()
			if _, ok := byLabels[lk]; !ok {
				byLabels[lk] = &uniqStream{key: key, stream: s}
			}
			allRefs = append(allRefs, localRef{sourceIdx: sourceIdx, localID: localID, labelsKey: lk})
		}
	}

	unique := make([]uniqStream, 0, len(byLabels))
	for _, u := range byLabels {
		unique = append(unique, *u)
	}
	slices.SortFunc(unique, func(a, b uniqStream) int {
		return CompareStreamOrderKey(a.key, b.key)
	})

	ranks := &StreamRanks{
		byNewID: make([]RankedStream, len(unique)+1),
		remap:   make([]map[int64]int64, len(sources)),
	}
	for i := range ranks.remap {
		ranks.remap[i] = make(map[int64]int64)
	}

	labelToID := make(map[string]int64, len(unique))
	for i, u := range unique {
		id := int64(i + 1)
		labelToID[u.stream.Labels.String()] = id
		s := u.stream
		s.ID = id
		ranks.byNewID[id] = RankedStream{Stream: s, Key: u.key}
	}
	for _, r := range allRefs {
		ranks.remap[r.sourceIdx][r.localID] = labelToID[r.labelsKey]
	}
	return ranks, nil
}

// ByID returns the stream assigned to new ID id.
func (r *StreamRanks) ByID(id int64) RankedStream {
	return r.byNewID[id]
}

// Remap returns the old-to-new ID map for one source.
func (r *StreamRanks) Remap(sourceIdx int) map[int64]int64 {
	return r.remap[sourceIdx]
}

// Resolve returns the ranked stream for a source's local stream ID.
func (r *StreamRanks) Resolve(sourceIdx int, localID int64) RankedStream {
	return r.ByID(r.remap[sourceIdx][localID])
}

// Streams returns ranked streams in ID order (1..N).
func (r *StreamRanks) Streams() []streams.Stream {
	out := make([]streams.Stream, 0, len(r.byNewID)-1)
	for id := 1; id < len(r.byNewID); id++ {
		out = append(out, r.byNewID[id].Stream)
	}
	return out
}
