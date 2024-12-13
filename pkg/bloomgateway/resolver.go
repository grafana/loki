package bloomgateway

import (
	"context"
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
	result := make([]blockWithSeries, 0, len(metas))

	for _, meta := range metas {
		for _, block := range meta.Blocks {

			// skip blocks that are not within time interval
			if !interval.Overlaps(block.Interval()) {
				continue
			}

			min := sort.Search(len(series), func(i int) bool {
				return block.Cmp(series[i].Fingerprint) > v1.Before
			})

			max := sort.Search(len(series), func(i int) bool {
				return block.Cmp(series[i].Fingerprint) == v1.After
			})

			// All fingerprints fall outside of the consumer's range
			if min == len(series) || max == 0 || min == max {
				continue
			}

			// At least one fingerprint is within bounds of the blocks
			// so append to results
			dst := make([]*logproto.GroupedChunkRefs, max-min)
			_ = copy(dst, series[min:max])
			result = append(result, blockWithSeries{
				block:  block,
				series: dst,
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
