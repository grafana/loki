package queryrange

import (
	"context"
	"fmt"
	math "math"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/dskit/concurrency"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/index/stats"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexgateway/indexgatewaypb"
	"github.com/grafana/loki/pkg/util"
)

func shardResolverForConf(ctx context.Context, conf config.PeriodConfig, maxParallelism int, r queryrangebase.Request, handler queryrangebase.Handler) (logql.ShardResolver, bool) {
	if conf.IndexType == config.TSDBType {
		from, through := util.RoundToMilliseconds(
			time.Unix(0, r.GetStart()),
			time.Unix(0, r.GetEnd()),
		)
		return &dynamicShardResolver{
			ctx:            ctx,
			handler:        handler,
			from:           from,
			through:        through,
			maxParallelism: maxParallelism,
		}, true
	}
	if conf.RowShards < 2 {
		return nil, false
	}
	return logql.ConstantShards(conf.RowShards), true
}

type dynamicShardResolver struct {
	ctx     context.Context
	handler queryrangebase.Handler

	from, through  model.Time
	maxParallelism int
}

// from, through, max concurrency to run
func (r *dynamicShardResolver) Shards(e syntax.Expr) (int, error) {
	// We try to shard subtrees in the AST independently if possible, although
	// nested binary expressions can make this difficult. In this case,
	// we query the index stats for all matcher groups then sum the results.
	grps := syntax.MatcherGroups(e)

	// If there are zero matchers groups, we'll inject one to query everything
	if len(grps) == 0 {
		grps = append(grps, []*labels.Matcher{})
	}

	results := make([]*stats.Stats, 0, len(grps))

	if err := concurrency.ForEachJob(r.ctx, len(grps), r.maxParallelism, func(ctx context.Context, i int) error {
		resp, err := r.handler.Do(r.ctx, &indexgatewaypb.IndexStatsRequest{
			From:     r.from,
			Through:  r.through,
			Matchers: syntax.MatchersString(grps[i]),
		})
		if err != nil {
			return err
		}

		casted, ok := resp.(*IndexStatsResponse)
		if !ok {
			return fmt.Errorf("expected *IndexStatsResponse while querying index, got %T", resp)
		}

		results = append(results, casted.Response)
		return nil
	}); err != nil {
		return 0, err
	}

	combined := stats.MergeStats(results...)
	return guessShardFactor(combined), nil
}

const (
	p90BytesPerSecond = 300 << 20 // 300MB/s/core
	// At max, schedule a query for 15s of execution before
	// splitting it into more requests. This is a lot of guesswork.
	maxSchedulableBytes = 15 * p90BytesPerSecond
)

func guessShardFactor(stats stats.Stats) int {
	partitions := float64(stats.Bytes / p90BytesPerSecond)
	p := math.Ceil(math.Log2(partitions)) // round up to nearest power of 2
	return int(math.Pow(2, p))
}
