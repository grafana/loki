package tsdb

import (
	"math"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage/stores/index/stats"
	"github.com/prometheus/common/model"
)

type sizedFP struct {
	fp    model.Fingerprint
	stats stats.Stats
}

type sizedFPs []sizedFP

func (xs sizedFPs) Len() int {
	return len(xs)
}

func (xs sizedFPs) Less(i, j int) bool {
	return xs[i].fp < xs[j].fp
}

func (xs sizedFPs) Swap(i, j int) {
	xs[i], xs[j] = xs[j], xs[i]
}

func (xs sizedFPs) newShard(minFP model.Fingerprint) logproto.Shard {
	return logproto.Shard{
		Bounds: logproto.FPBounds{
			Min: minFP,
		},
		Stats: &stats.Stats{},
	}
}

func (xs sizedFPs) ShardsFor(targetShardBytes uint64) (res []logproto.Shard) {
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
		if cur.SpaceFor(&x.stats, targetShardBytes) {
			cur.Stats.Streams++
			cur.Stats.Chunks += x.stats.Chunks
			cur.Stats.Entries += x.stats.Entries
			cur.Stats.Bytes += x.stats.Bytes

			cur.Bounds.Max = x.fp
			continue
		}

		// we've hit a stream larger than the target;
		// create a shard with 1 stream
		if cur.Stats.Streams == 0 {
			cur.Stats = &stats.Stats{
				Streams: 1,
				Chunks:  x.stats.Chunks,
				Bytes:   x.stats.Bytes,
				Entries: x.stats.Entries,
			}
			cur.Bounds.Max = x.fp
			res = append(res, cur)
			cur = xs.newShard(x.fp + 1)
			continue
		}

		// Otherwise we've hit a stream that's too large but the current shard isn't empty; create a new shard
		cur.Bounds.Max = x.fp - 1
		res = append(res, cur)
		cur = xs.newShard(x.fp)
		cur.Stats = &stats.Stats{
			Streams: 1,
			Chunks:  x.stats.Chunks,
			Bytes:   x.stats.Bytes,
			Entries: x.stats.Entries,
		}
	}

	if cur.Stats.Streams > 0 {
		res = append(res, cur)
	}

	res[len(res)-1].Bounds.Max = model.Fingerprint(math.MaxUint64)
	return res
}
