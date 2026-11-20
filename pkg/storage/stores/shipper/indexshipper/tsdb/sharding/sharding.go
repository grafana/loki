package sharding

import (
	"math"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/queue"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/stats"
)

var (
	SizedFPsPool = queue.NewSlicePool[SizedFP](1<<8, 1<<16, 4) // 256->65536
)

type SizedFP struct {
	Fp    model.Fingerprint
	Stats stats.Stats
}

type SizedFPs []SizedFP

func (xs SizedFPs) Len() int {
	return len(xs)
}

func (xs SizedFPs) Less(i, j int) bool {
	return xs[i].Fp < xs[j].Fp
}

func (xs SizedFPs) Swap(i, j int) {
	xs[i], xs[j] = xs[j], xs[i]
}

func newShard(minFP model.Fingerprint) logproto.Shard {
	return logproto.Shard{
		Bounds: logproto.FPBounds{
			Min: minFP,
		},
		Stats: &stats.Stats{},
	}
}

// ShardAccumulator builds shards from fingerprint-ordered stream totals without
// retaining those totals. Only the resulting shards are retained.
type ShardAccumulator struct {
	target uint64
	cur    logproto.Shard
	shards []logproto.Shard
}

// NewShardAccumulator creates an incremental shard planner.
func NewShardAccumulator(targetShardBytes uint64) *ShardAccumulator {
	return &ShardAccumulator{target: targetShardBytes, cur: newShard(0)}
}

// Add consumes the complete totals for one fingerprint. Fingerprints must be
// strictly increasing; a stream must not be split across calls.
func (a *ShardAccumulator) Add(x SizedFP) {
	if a.cur.SpaceFor(&x.Stats, a.target) {
		a.cur.Stats.Streams++
		a.cur.Stats.Chunks += x.Stats.Chunks
		a.cur.Stats.Entries += x.Stats.Entries
		a.cur.Stats.Bytes += x.Stats.Bytes
		a.cur.Bounds.Max = x.Fp
		return
	}
	// An oversized first stream gets a shard of its own.
	if a.cur.Stats.Streams == 0 {
		a.cur.Stats = &stats.Stats{Streams: 1, Chunks: x.Stats.Chunks, Bytes: x.Stats.Bytes, Entries: x.Stats.Entries}
		a.cur.Bounds.Max = x.Fp
		a.shards = append(a.shards, a.cur)
		a.cur = newShard(x.Fp + 1)
		return
	}
	// Keep the existing nearest-target sizing rule and put the new stream
	// in the next shard when adding it would move farther from the target.
	a.cur.Bounds.Max = x.Fp - 1
	a.shards = append(a.shards, a.cur)
	a.cur = newShard(x.Fp)
	a.cur.Stats = &stats.Stats{Streams: 1, Chunks: x.Stats.Chunks, Bytes: x.Stats.Bytes, Entries: x.Stats.Entries}
}

// Shards finishes the plan, covering the whole fingerprint space even for empty
// input. The accumulator must not be used after this call.
func (a *ShardAccumulator) Shards() []logproto.Shard {
	if a.cur.Stats.Streams > 0 || len(a.shards) == 0 {
		a.shards = append(a.shards, a.cur)
	}
	a.shards[len(a.shards)-1].Bounds.Max = model.Fingerprint(math.MaxUint64)
	return a.shards
}

func (xs SizedFPs) ShardsFor(targetShardBytes uint64) []logproto.Shard {
	a := NewShardAccumulator(targetShardBytes)
	for _, x := range xs {
		a.Add(x)
	}
	return a.Shards()
}
