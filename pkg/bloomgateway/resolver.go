package bloomgateway

import (
	"context"
	"sort"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/loki/v3/pkg/logproto"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
	"github.com/prometheus/common/model"
)

type BlockResolver interface {
	Resolve(context.Context, string, bloomshipper.Interval, []*logproto.GroupedChunkRefs) ([]blockWithSeries, error)
}

type blockWithSeries struct {
	block  bloomshipper.BlockRef
	series []*logproto.GroupedChunkRefs
}

type defaultBlockResolver struct {
	store  bloomshipper.Store
	logger log.Logger
}

func (r *defaultBlockResolver) Resolve(ctx context.Context, tenant string, interval bloomshipper.Interval, series []*logproto.GroupedChunkRefs) ([]blockWithSeries, error) {
	minFp, maxFp := getFirstLast(series)
	metaSearch := bloomshipper.MetaSearchParams{
		TenantID: tenant,
		Interval: interval,
		Keyspace: v1.NewBounds(model.Fingerprint(minFp.Fingerprint), model.Fingerprint(maxFp.Fingerprint)),
	}

	startMetas := time.Now()
	metas, err := r.store.FetchMetas(ctx, metaSearch)
	duration := time.Since(startMetas)
	level.Debug(r.logger).Log("msg", "fetched metas", "count", len(metas), "duration", duration, "err", err)

	if err != nil {
		return nil, err
	}

	return blocksMatchingSeries(metas, interval, series), nil
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
			result = append(result, blockWithSeries{
				block:  block,
				series: series[min:max],
			})
		}
	}

	return result
}

func NewBlockResolver(store bloomshipper.Store, logger log.Logger) BlockResolver {
	return &defaultBlockResolver{
		store:  store,
		logger: logger,
	}
}
