package indexgateway

import (
	"container/heap"
	"context"
	"errors"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/sharding"
)

// Use the legacy lookup if a cursor violates its ordering guarantee.
var errUnorderedChunkCursor = errors.New("chunk cursor returned a key out of order")

type cursorChunk struct {
	file int
	ref  logproto.ChunkRefWithSizingInfo
}

type chunkMergeHeap []cursorChunk

func (h chunkMergeHeap) Len() int           { return len(h) }
func (h chunkMergeHeap) Less(i, j int) bool { return h[i].ref.Less(h[j].ref.ChunkRef) }
func (h chunkMergeHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *chunkMergeHeap) Push(x any)        { *h = append(*h, x.(cursorChunk)) }
func (h *chunkMergeHeap) Pop() any {
	last := len(*h) - 1
	x := (*h)[last]
	*h = (*h)[:last]
	return x
}

func chunkAt(file int, cursor sharding.ChunkCursor) cursorChunk {
	return cursorChunk{file: file, ref: cursor.At()}
}

// planShardsFromCursors takes ownership of cursors and merges one chunk from
// each active file at a time. Each cursor holds the chunk lists for one
// fingerprint. The planner needs only the last key to deduplicate exact refs.
func planShardsFromCursors(ctx context.Context, req *logproto.ShardsRequest, precompute bool, cursors []sharding.ChunkCursor) (resp *logproto.ShardsResponse, err error) {
	defer func() {
		if closeErr := sharding.CloseChunkCursors(cursors); closeErr != nil {
			resp = nil
			if errors.Is(err, errUnorderedChunkCursor) {
				err = closeErr
			} else {
				err = errors.Join(err, closeErr)
			}
		}
	}()
	return planShardResponse(ctx, req, precompute, func(add func(logproto.ChunkRefWithSizingInfo) error) error {
		h := make(chunkMergeHeap, 0, len(cursors))
		for i, cursor := range cursors {
			if cursor.Next() {
				h = append(h, chunkAt(i, cursor))
				continue
			}
			if err := cursor.Err(); err != nil {
				return err
			}
			cursors[i] = nil
			if err := cursor.Close(); err != nil {
				return err
			}
		}
		heap.Init(&h)
		var last logproto.ChunkRef
		haveLast := false
		for h.Len() != 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
			head := h[0]
			if haveLast && head.ref.Less(last) {
				return errUnorderedChunkCursor
			}
			if !haveLast || head.ref.ChunkRef != last {
				if err := add(head.ref); err != nil {
					return err
				}
				last, haveLast = head.ref.ChunkRef, true
			}
			cursor := cursors[head.file]
			if cursor.Next() {
				h[0] = chunkAt(head.file, cursor)
				heap.Fix(&h, 0)
			} else {
				if err := cursor.Err(); err != nil {
					return err
				}
				cursors[head.file] = nil
				if err := cursor.Close(); err != nil {
					return err
				}
				heap.Pop(&h)
			}
		}
		return ctx.Err()
	})
}

func planShardResponse(ctx context.Context, req *logproto.ShardsRequest, precompute bool, walk func(func(logproto.ChunkRefWithSizingInfo) error) error) (*logproto.ShardsResponse, error) {
	resp := &logproto.ShardsResponse{}
	acc := sharding.NewShardAccumulator(req.TargetBytesPerShard)
	var current sharding.SizedFP
	// With precompute enabled, the response owns every unique chunk reference.
	var precomputed []*logproto.ChunkRef
	err := walk(func(ref logproto.ChunkRefWithSizingInfo) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		fp := model.Fingerprint(ref.Fingerprint)
		if current.Stats.Chunks > 0 && current.Fp != fp {
			acc.Add(current)
			current = sharding.SizedFP{}
		}
		if current.Stats.Chunks == 0 {
			resp.Statistics.Index.TotalStreams++
		}
		current.Fp = fp
		current.Stats.Chunks++
		current.Stats.Entries += uint64(ref.Entries)
		current.Stats.Bytes += uint64(ref.KB) << 10
		resp.Statistics.Index.TotalChunks++
		if precompute {
			cp := ref.ChunkRef
			precomputed = append(precomputed, &cp)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if current.Stats.Chunks > 0 {
		acc.Add(current)
	}
	resp.Shards = acc.Shards()
	resp.Statistics.Index.PostFilterChunks = resp.Statistics.Index.TotalChunks
	if len(precomputed) > 0 {
		for _, shard := range resp.Shards {
			n := 0
			for n < len(precomputed) && precomputed[n].Fingerprint <= uint64(shard.Bounds.Max) {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				n++
			}
			resp.ChunkGroups = append(resp.ChunkGroups, logproto.ChunkRefGroup{Refs: precomputed[:n:n]})
			precomputed = precomputed[n:]
		}
	}
	return resp, nil
}
