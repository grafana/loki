package queryrange

import (
	"context"
	"fmt"
	math "math"
	strings "strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/concurrency"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/index/stats"
	"github.com/grafana/loki/pkg/util/spanlogger"
)

func shardResolverForConf(
	ctx context.Context,
	conf config.PeriodConfig,
	defaultLookback time.Duration,
	logger log.Logger,
	maxParallelism int,
	r queryrangebase.Request,
	handler queryrangebase.Handler,
) (logql.ShardResolver, bool) {
	if conf.IndexType == config.TSDBType {
		return &dynamicShardResolver{
			ctx:             ctx,
			logger:          logger,
			handler:         handler,
			from:            model.Time(r.GetStart()),
			through:         model.Time(r.GetEnd()),
			maxParallelism:  maxParallelism,
			defaultLookback: defaultLookback,
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
	logger  log.Logger

	from, through   model.Time
	maxParallelism  int
	defaultLookback time.Duration
}

func (r *dynamicShardResolver) Shards(e syntax.Expr) (int, error) {
	sp, ctx := spanlogger.NewWithLogger(r.ctx, r.logger, "dynamicShardResolver.Shards")
	defer sp.Finish()
	// We try to shard subtrees in the AST independently if possible, although
	// nested binary expressions can make this difficult. In this case,
	// we query the index stats for all matcher groups then sum the results.
	grps := syntax.MatcherGroups(e)

	// If there are zero matchers groups, we'll inject one to query everything
	if len(grps) == 0 {
		grps = append(grps, syntax.MatcherRange{})
	}

	results := make([]*stats.Stats, 0, len(grps))

	start := time.Now()
	if err := concurrency.ForEachJob(ctx, len(grps), r.maxParallelism, func(ctx context.Context, i int) error {
		matchers := syntax.MatchersString(grps[i].Matchers)
		diff := grps[i].Interval + grps[i].Offset
		adjustedFrom := r.from.Add(-diff)
		if grps[i].Interval == 0 {
			adjustedFrom = adjustedFrom.Add(-r.defaultLookback)
		}

		adjustedThrough := r.through.Add(-grps[i].Offset)

		start := time.Now()
		resp, err := r.handler.Do(r.ctx, &logproto.IndexStatsRequest{
			From:     adjustedFrom,
			Through:  adjustedThrough,
			Matchers: matchers,
		})
		if err != nil {
			return err
		}

		casted, ok := resp.(*IndexStatsResponse)
		if !ok {
			return fmt.Errorf("expected *IndexStatsResponse while querying index, got %T", resp)
		}

		results = append(results, casted.Response)
		level.Debug(sp).Log(
			append(
				casted.Response.LoggingKeyValues(),
				"msg", "queried index",
				"type", "single",
				"matchers", matchers,
				"duration", time.Since(start),
				"from", adjustedFrom.Time(),
				"through", adjustedThrough.Time(),
				"length", adjustedThrough.Sub(adjustedFrom),
			)...,
		)
		return nil
	}); err != nil {
		return 0, err
	}

	combined := stats.MergeStats(results...)
	factor := guessShardFactor(combined)
	var bytesPerShard = combined.Bytes
	if factor > 0 {
		bytesPerShard = combined.Bytes / uint64(factor)
	}
	level.Debug(sp).Log(
		append(
			combined.LoggingKeyValues(),
			"msg", "queried index",
			"type", "combined",
			"len", len(results),
			"max_parallelism", r.maxParallelism,
			"duration", time.Since(start),
			"factor", factor,
			"bytes_per_shard", strings.Replace(humanize.Bytes(bytesPerShard), " ", "", 1),
		)...,
	)
	return factor, nil
}

const (
	// Just some observed values to get us started on better query planning.
	maxBytesPerShard = 600 << 20
)

// Since we shard by powers of two and we increase shard factor
// once each shard surpasses maxBytesPerShard, if the shard factor
// is at least two, the range of data per shard is (maxBytesPerShard/2, maxBytesPerShard]
// For instance, for a maxBytesPerShard of 500MB and a query touching 1000MB, we split into two shards of 500MB.
// If there are 1004MB, we split into four shards of 251MB.
func guessShardFactor(stats stats.Stats) int {
	minShards := float64(stats.Bytes) / float64(maxBytesPerShard)

	// round up to nearest power of 2
	power := math.Ceil(math.Log2(minShards))

	// Since x^0 == 1 and we only support factors of 2
	// reset this edge case manually
	factor := int(math.Pow(2, power))
	if factor == 1 {
		factor = 0
	}
	return factor
}
