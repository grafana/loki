package bloomgateway

import (
	"context"
	"slices"
	"sort"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/logproto"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
)

type BlockResolver interface {
	Resolve(ctx context.Context, tenant string, interval bloomshipper.Interval, series []*logproto.GroupedChunkRefs) (blocks []blockWithSeries, skipped []*logproto.GroupedChunkRefs, err error)
}

type blockWithSeries struct {
	block  bloomshipper.BlockRef
	series []*logproto.GroupedChunkRefs
}

type defaultBlockResolver struct {
	store  bloomshipper.StoreBase
	logger log.Logger
}

func (r *defaultBlockResolver) Resolve(ctx context.Context, tenant string, interval bloomshipper.Interval, series []*logproto.GroupedChunkRefs) ([]blockWithSeries, []*logproto.GroupedChunkRefs, error) {
	minFp, maxFp := getFirstLast(series)
	metaSearch := bloomshipper.MetaSearchParams{
		TenantID: tenant,
		Interval: interval,
		Keyspace: v1.NewBounds(model.Fingerprint(minFp.Fingerprint), model.Fingerprint(maxFp.Fingerprint)),
	}

	startMetas := time.Now()
	metas, err := r.store.FetchMetas(ctx, metaSearch)
	duration := time.Since(startMetas)
	level.Debug(r.logger).Log(
		"msg", "fetched metas",
		"tenant", tenant,
		"from", interval.Start.Time(),
		"through", interval.End.Time(),
		"minFp", minFp.Fingerprint,
		"maxFp", maxFp.Fingerprint,
		"count", len(metas),
		"duration", duration,
		"err", err,
	)

	if err != nil {
		return nil, series, err
	}

	mapped := blocksMatchingSeries(metas, interval, series)
	skipped := unassignedSeries(mapped, series)
	return mapped, skipped, nil
}

func blocksMatchingSeries(metas []bloomshipper.Meta, interval bloomshipper.Interval, series []*logproto.GroupedChunkRefs) []blockWithSeries {
	slices.SortFunc(series, func(a, b *logproto.GroupedChunkRefs) int { return int(a.Fingerprint - b.Fingerprint) })

	result := make([]blockWithSeries, 0, len(metas))
	cache := make(map[bloomshipper.BlockRef]int)

	// find the newest block for each series
	for _, s := range series {
		var b *bloomshipper.BlockRef
		var newestTs time.Time

		for i := range metas {
			for j := range metas[i].Blocks {
				block := metas[i].Blocks[j]
				// To keep backwards compatibility, we can only look at the source at index 0
				// because in the past the slice had always length 1, see
				// https://github.com/grafana/loki/blob/b4060154d198e17bef8ba0fbb1c99bb5c93a412d/pkg/bloombuild/builder/builder.go#L418
				sourceTs := metas[i].Sources[0].TS
				// Newer metas have len(Sources) == len(Blocks)
				if len(metas[i].Sources) > j {
					sourceTs = metas[i].Sources[j].TS
				}
				// skip blocks that are not within time interval
				if !interval.Overlaps(block.Interval()) {
					continue
				}
				// skip blocks that do not contain the series
				if block.Cmp(s.Fingerprint) != v1.Overlap {
					continue
				}
				// only use the block if it is newer than the previous
				if sourceTs.After(newestTs) {
					b = &block
					newestTs = sourceTs
				}
			}
		}

		if b == nil {
			continue
		}

		idx, ok := cache[*b]
		if ok {
			result[idx].series = append(result[idx].series, s)
		} else {
			cache[*b] = len(result)
			result = append(result, blockWithSeries{
				block:  *b,
				series: []*logproto.GroupedChunkRefs{s},
			})
		}
	}

	return result
}

func unassignedSeries(mapped []blockWithSeries, series []*logproto.GroupedChunkRefs) []*logproto.GroupedChunkRefs {
	skipped := make([]*logproto.GroupedChunkRefs, len(series))
	_ = copy(skipped, series)

	for _, block := range mapped {
		minFp, maxFp := getFirstLast(block.series)

		minIdx := sort.Search(len(skipped), func(i int) bool {
			return skipped[i].Fingerprint >= minFp.Fingerprint
		})

		maxIdx := sort.Search(len(skipped), func(i int) bool {
			return skipped[i].Fingerprint > maxFp.Fingerprint
		})

		if minIdx == len(skipped) || maxIdx == 0 || minIdx == maxIdx {
			continue
		}

		skipped = append(skipped[0:minIdx], skipped[maxIdx:]...)
	}

	return skipped
}

func NewBlockResolver(store bloomshipper.StoreBase, logger log.Logger) BlockResolver {
	return &defaultBlockResolver{
		store:  store,
		logger: logger,
	}
}
