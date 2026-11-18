package indexgateway

import (
	"context"
	"fmt"
	"math"
	"math/bits"
	"slices"
	"sync"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logproto"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/sharding"
)

const shardPlanningBatchSize = 128 << 10

// chunkKey identifies a chunk within the request's tenant.
type chunkKey struct {
	Fingerprint model.Fingerprint
	From        model.Time
	Through     model.Time
	Checksum    uint32
}

func keyForChunk(fp model.Fingerprint, c index.ChunkMeta) chunkKey {
	return chunkKey{Fingerprint: fp, From: c.From(), Through: c.Through(), Checksum: c.Checksum}
}

// less uses the same field order as ChunkRef.Less.
func (k chunkKey) less(other chunkKey) bool {
	switch {
	case k.Fingerprint != other.Fingerprint:
		return k.Fingerprint < other.Fingerprint
	case k.From != other.From:
		return k.From < other.From
	case k.Through != other.Through:
		return k.Through < other.Through
	default:
		return k.Checksum < other.Checksum
	}
}

type chunkRange struct{ min, max chunkKey }

// split cuts at the first field where the endpoints differ. Set the fields
// that follow to their limits so there is no gap between the two ranges.
func (r chunkRange) split() (chunkRange, chunkRange) {
	lo, hi := r.min, r.max
	leftMax := chunkKey{From: math.MaxInt64, Through: math.MaxInt64, Checksum: math.MaxUint32}
	rightMin := chunkKey{From: math.MinInt64, Through: math.MinInt64}
	switch {
	case lo.Fingerprint != hi.Fingerprint:
		leftMax.Fingerprint, rightMin.Fingerprint = splitKeyField(lo.Fingerprint, hi.Fingerprint)
	case lo.From != hi.From:
		leftMax.Fingerprint, rightMin.Fingerprint = lo.Fingerprint, lo.Fingerprint
		leftMax.From, rightMin.From = splitKeyField(lo.From, hi.From)
	case lo.Through != hi.Through:
		leftMax.Fingerprint, rightMin.Fingerprint = lo.Fingerprint, lo.Fingerprint
		leftMax.From, rightMin.From = lo.From, lo.From
		leftMax.Through, rightMin.Through = splitKeyField(lo.Through, hi.Through)
	default:
		leftMax, rightMin = lo, lo
		leftMax.Checksum, rightMin.Checksum = splitKeyField(lo.Checksum, hi.Checksum)
	}
	return chunkRange{min: lo, max: leftMax}, chunkRange{min: rightMin, max: hi}
}

// splitKeyField cuts at the highest bit where lo and hi differ. Clearing the
// bits below it in hi gives the first value on the right. For signed times on
// either side of zero, this puts the cut between -1 and 0.
func splitKeyField[T ~uint64 | ~int64 | ~uint32](lo, hi T) (leftMax, rightMin T) {
	bit := uint64(1) << (bits.Len64(uint64(lo^hi)) - 1)
	rightMin = hi &^ T(bit-1)
	return rightMin - 1, rightMin
}

// forEachChunkBatch deduplicates across ALL indexes before emitting each sorted
// batch. Neither file order nor callback order is assumed. The callback borrows
// the batch until it returns. At most batchSize reference identities are retained
// in the map and slice, plus a depth-first stack of at most 225 ranges (224 identity bits).
//
// Overflowing ranges are rescanned, first narrowing the index fingerprint filter
// and then the rest of the identity for skewed streams. The latter can require
// many scans of the same stream: this bounds memory at the cost of CPU/IO. The
// index's own postings and per-series ChunkMeta decode buffers are not bounded
// here.
func forEachChunkBatch(ctx context.Context, series sharding.ForSeries, userID string, req *logproto.ShardsRequest, matchers []*labels.Matcher, batchSize int, consume func([]logproto.ChunkRefWithSizingInfo) error) error {
	if batchSize < 1 {
		return fmt.Errorf("shard planning batch size must be positive")
	}
	var seen map[chunkKey]struct{}
	var refs []logproto.ChunkRefWithSizingInfo
	pending := []chunkRange{{
		min: chunkKey{From: math.MinInt64, Through: math.MinInt64},
		max: chunkKey{Fingerprint: math.MaxUint64, From: math.MaxInt64, Through: math.MaxInt64, Checksum: math.MaxUint32},
	}}
	for len(pending) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		r := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		clear(seen)
		refs = refs[:0]
		var mu sync.Mutex
		var overflow bool
		var lo, hi chunkKey
		err := series.ForSeries(ctx, userID, v1.NewBounds(r.min.Fingerprint, r.max.Fingerprint), req.From, req.Through, func(_ labels.Labels, fp model.Fingerprint, chunks []index.ChunkMeta) bool {
			mu.Lock()
			defer mu.Unlock()
			if ctx.Err() != nil {
				return true
			}
			for _, c := range chunks {
				if ctx.Err() != nil {
					return true
				}
				key := keyForChunk(fp, c)
				if key.less(r.min) || r.max.less(key) {
					continue
				}
				if len(refs) == 0 || key.less(lo) {
					lo = key
				}
				if len(refs) == 0 || hi.less(key) {
					hi = key
				}
				if overflow {
					continue
				}
				if _, ok := seen[key]; ok {
					continue
				}
				if len(refs) == batchSize {
					overflow = true
					continue
				}
				if seen == nil {
					// Small requests can finish well before reaching the cap.
					initialSize := min(batchSize, 1024)
					seen = make(map[chunkKey]struct{}, initialSize)
					refs = make([]logproto.ChunkRefWithSizingInfo, 0, initialSize)
				}
				if len(refs) == cap(refs) {
					grown := make([]logproto.ChunkRefWithSizingInfo, len(refs), min(2*cap(refs), batchSize))
					copy(grown, refs)
					refs = grown
				}
				seen[key] = struct{}{}
				refs = append(refs, logproto.ChunkRefWithSizingInfo{
					ChunkRef: logproto.ChunkRef{UserID: userID, Fingerprint: uint64(fp), From: c.From(), Through: c.Through(), Checksum: c.Checksum},
					KB:       c.KB, Entries: c.Entries,
				})
			}
			return false
		}, matchers...)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if overflow {
			// Even after the batch fills, the scan keeps track of the smallest
			// and largest keys. Split that smaller range so we can skip the
			// empty space on either side.
			left, right := (chunkRange{min: lo, max: hi}).split()
			pending = append(pending, right, left)
			continue
		}
		slices.SortFunc(refs, func(a, b logproto.ChunkRefWithSizingInfo) int {
			if a.Less(b.ChunkRef) {
				return -1
			}
			if b.Less(a.ChunkRef) {
				return 1
			}
			return 0
		})
		if err := consume(refs); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func planShards(ctx context.Context, series sharding.ForSeries, userID string, req *logproto.ShardsRequest, matchers []*labels.Matcher, precompute bool, batchSize int) (*logproto.ShardsResponse, error) {
	resp := &logproto.ShardsResponse{}
	acc := sharding.NewShardAccumulator(req.TargetBytesPerShard)
	var current sharding.SizedFP
	// The batch limit caps how many chunk references we hold while sorting and
	// removing duplicates. The response holds every shard and, with precompute
	// enabled, every unique chunk reference.
	var precomputed []*logproto.ChunkRef
	err := forEachChunkBatch(ctx, series, userID, req, matchers, batchSize, func(refs []logproto.ChunkRefWithSizingInfo) error {
		for _, ref := range refs {
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
