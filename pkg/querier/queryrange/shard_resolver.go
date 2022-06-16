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

	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/index/stats"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexgateway/indexgatewaypb"
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
	sp, _ := spanlogger.NewWithLogger(r.ctx, r.logger, "dynamicShardResolver.Shards")
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
	if err := concurrency.ForEachJob(r.ctx, len(grps), r.maxParallelism, func(ctx context.Context, i int) error {
		matchers := syntax.MatchersString(grps[i].Matchers)
		diff := grps[i].Interval + grps[i].Offset
		adjustedFrom := r.from.Add(-diff)
		if grps[i].Interval == 0 {
			adjustedFrom = adjustedFrom.Add(-r.defaultLookback)
		}

		adjustedThrough := r.through.Add(-grps[i].Offset)

		start := time.Now()
		resp, err := r.handler.Do(r.ctx, &indexgatewaypb.IndexStatsRequest{
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
			"msg", "queried index",
			"type", "single",
			"matchers", matchers,
			"bytes", strings.Replace(humanize.Bytes(casted.Response.Bytes), " ", "", 1),
			"chunks", casted.Response.Chunks,
			"streams", casted.Response.Streams,
			"entries", casted.Response.Entries,
			"duration", time.Since(start),
			"from", adjustedFrom.Time(),
			"through", adjustedThrough.Time(),
			"length", adjustedThrough.Sub(adjustedFrom),
		)
		return nil
	}); err != nil {
		return 0, err
	}

	combined := stats.MergeStats(results...)
	factor := guessShardFactor(combined, r.maxParallelism)
	level.Debug(sp).Log(
		"msg", "queried index",
		"type", "combined",
		"len", len(results),
		"bytes", strings.Replace(humanize.Bytes(combined.Bytes), " ", "", 1),
		"chunks", combined.Chunks,
		"streams", combined.Streams,
		"entries", combined.Entries,
		"max_parallelism", r.maxParallelism,
		"duration", time.Since(start),
		"factor", factor,
	)
	return factor, nil
}

const (
	// Just some observed values to get us started on better query planning.
	p90BytesPerSecond = 300 << 20 // 300MB/s/core
	// At max, schedule a query for 10s of execution before
	// splitting it into more requests. This is a lot of guesswork.
	maxSeconds          = 10
	maxSchedulableBytes = maxSeconds * p90BytesPerSecond
)

func guessShardFactor(stats stats.Stats, maxParallelism int) int {
	expectedSeconds := float64(stats.Bytes / p90BytesPerSecond)
	if expectedSeconds <= float64(maxParallelism) {
		power := math.Ceil(math.Log2(expectedSeconds)) // round up to nearest power of 2
		// Ideally, parallelize down to 1s queries
		return int(math.Pow(2, power))
	}

	n := stats.Bytes / maxSchedulableBytes
	power := math.Ceil(math.Log2(float64(n)))
	return int(math.Pow(2, power))
}
