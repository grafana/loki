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

func (xs SizedFPs) newShard(minFP model.Fingerprint) logproto.Shard {
	return logproto.Shard{
		Bounds: logproto.FPBounds{
			Min: minFP,
		},
		Stats: &stats.Stats{},
	}
}

func (xs SizedFPs) ShardsFor(targetShardBytes uint64) (res []logproto.Shard) {
	if len(xs) == 0 {
		full := xs.newShard(0)
		full.Bounds.Max = model.Fingerprint(math.MaxUint64)
		return []logproto.Shard{full}
	}

	var (
		cur = xs.newShard(0)
	)

	for _, x := range xs {

		// easy path, there's space -- continue
		if cur.SpaceFor(&x.Stats, targetShardBytes) {
			cur.Stats.Streams++
			cur.Stats.Chunks += x.Stats.Chunks
			cur.Stats.Entries += x.Stats.Entries
			cur.Stats.Bytes += x.Stats.Bytes

			cur.Bounds.Max = x.Fp
			continue
		}

		// we've hit a stream larger than the target;
		// create a shard with 1 stream
		if cur.Stats.Streams == 0 {
			cur.Stats = &stats.Stats{
				Streams: 1,
				Chunks:  x.Stats.Chunks,
				Bytes:   x.Stats.Bytes,
				Entries: x.Stats.Entries,
			}
			cur.Bounds.Max = x.Fp
			res = append(res, cur)
			cur = xs.newShard(x.Fp + 1)
			continue
		}

		// Otherwise we've hit a stream that's too large but the current shard isn't empty; create a new shard
		cur.Bounds.Max = x.Fp - 1
		res = append(res, cur)
		cur = xs.newShard(x.Fp)
		cur.Stats = &stats.Stats{
			Streams: 1,
			Chunks:  x.Stats.Chunks,
			Bytes:   x.Stats.Bytes,
			Entries: x.Stats.Entries,
		}
	}

	if cur.Stats.Streams > 0 {
		res = append(res, cur)
	}

	res[len(res)-1].Bounds.Max = model.Fingerprint(math.MaxUint64)
	return res
}
