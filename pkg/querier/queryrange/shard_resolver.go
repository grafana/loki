package queryrange

import (
	"context"
	"fmt"
	strings "strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/efficientgo/core/errors"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/concurrency"
	"github.com/grafana/dskit/tenant"
	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	logqlstats "github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/stats"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/sharding"
	"github.com/grafana/loki/v3/pkg/storage/types"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/util/spanlogger"
	"github.com/grafana/loki/v3/pkg/util/validation"
)

func shardResolverForConf(
	ctx context.Context,
	conf config.PeriodConfig,
	defaultLookback time.Duration,
	logger log.Logger,
	maxParallelism int,
	maxShards int,
	r queryrangebase.Request,
	statsHandler, next, retryNext queryrangebase.Handler,
	limits Limits,
) (logql.ShardResolver, bool) {
	if conf.IndexType == types.TSDBType {
		return &dynamicShardResolver{
			ctx:              ctx,
			logger:           logger,
			statsHandler:     statsHandler,
			retryNextHandler: retryNext,
			next:             next,
			limits:           limits,
			from:             model.Time(r.GetStart().UnixMilli()),
			through:          model.Time(r.GetEnd().UnixMilli()),
			maxParallelism:   maxParallelism,
			maxShards:        maxShards,
			defaultLookback:  defaultLookback,
		}, true
	}
	if conf.RowShards < 2 {
		return nil, false
	}
	return logql.ConstantShards(conf.RowShards), true
}

type dynamicShardResolver struct {
	ctx context.Context
	// TODO(owen-d): shouldn't have to fork handlers here -- one should just transparently handle the right logic
	// depending on the underlying type?
	statsHandler     queryrangebase.Handler // index stats handler (hooked up to results cache, etc)
	retryNextHandler queryrangebase.Handler // next handler wrapped with retries
	next             queryrangebase.Handler // next handler in the chain (used for non-stats reqs)
	logger           log.Logger
	limits           Limits

	from, through   model.Time
	maxParallelism  int
	maxShards       int
	defaultLookback time.Duration
}

// getStatsForMatchers returns the index stats for all the groups in matcherGroups.
func getStatsForMatchers(
	ctx context.Context,
	logger log.Logger,
	statsHandler queryrangebase.Handler,
	start, end model.Time,
	matcherGroups []syntax.MatcherRange,
	parallelism int,
	defaultLookback time.Duration,
) ([]*stats.Stats, error) {
	startTime := time.Now()

	results := make([]*stats.Stats, len(matcherGroups))
	if err := concurrency.ForEachJob(ctx, len(matcherGroups), parallelism, func(ctx context.Context, i int) error {
		matchers := syntax.MatchersString(matcherGroups[i].Matchers)
		diff := matcherGroups[i].Interval + matcherGroups[i].Offset
		adjustedFrom := start.Add(-diff)
		if matcherGroups[i].Interval == 0 {
			// For limited instant queries, when start == end, the queries would return
			// zero results. Prometheus has a concept of "look back amount of time for instant queries"
			// since metric data is sampled at some configurable scrape_interval (commonly 15s, 30s, or 1m).
			// We copy that idea and say "find me logs from the past when start=end".
			adjustedFrom = adjustedFrom.Add(-defaultLookback)
		}

		adjustedThrough := end.Add(-matcherGroups[i].Offset)

		resp, err := statsHandler.Do(ctx, &logproto.IndexStatsRequest{
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

		results[i] = casted.Response

		level.Debug(logger).Log(
			append(
				casted.Response.LoggingKeyValues(),
				"msg", "queried index",
				"type", "single",
				"matchers", matchers,
				"duration", time.Since(startTime),
				"from", adjustedFrom.Time(),
				"through", adjustedThrough.Time(),
				"length", adjustedThrough.Sub(adjustedFrom),
			)...,
		)

		return nil
	}); err != nil {
		return nil, err
	}

	return results, nil
}

func (r *dynamicShardResolver) GetStats(e syntax.Expr) (stats.Stats, error) {
	sp, ctx := opentracing.StartSpanFromContext(r.ctx, "dynamicShardResolver.GetStats")
	defer sp.Finish()

	start := time.Now()

	// We try to shard subtrees in the AST independently if possible, although
	// nested binary expressions can make this difficult. In this case,
	// we query the index stats for all matcher groups then sum the results.
	grps, err := syntax.MatcherGroups(e)
	if err != nil {
		return stats.Stats{}, err
	}

	// If there are zero matchers groups, we'll inject one to query everything
	if len(grps) == 0 {
		grps = append(grps, syntax.MatcherRange{})
	}

	log := util_log.WithContext(ctx, util_log.Logger)
	results, err := getStatsForMatchers(ctx, log, r.statsHandler, r.from, r.through, grps, r.maxParallelism, r.defaultLookback)
	if err != nil {
		return stats.Stats{}, err
	}

	combined := stats.MergeStats(results...)

	level.Debug(log).Log(
		append(
			combined.LoggingKeyValues(),
			"msg", "queried index",
			"type", "combined",
			"len", len(results),
			"max_parallelism", r.maxParallelism,
			"duration", time.Since(start),
		)...,
	)

	return combined, nil
}

func (r *dynamicShardResolver) Shards(e syntax.Expr) (int, uint64, error) {
	sp, ctx := opentracing.StartSpanFromContext(r.ctx, "dynamicShardResolver.Shards")
	defer sp.Finish()
	log := spanlogger.FromContext(ctx)
	defer log.Finish()

	combined, err := r.GetStats(e)
	if err != nil {
		return 0, 0, err
	}

	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return 0, 0, err
	}

	maxBytesPerShard := validation.SmallestPositiveIntPerTenant(tenantIDs, r.limits.TSDBMaxBytesPerShard)
	factor := sharding.GuessShardFactor(combined.Bytes, uint64(maxBytesPerShard), r.maxShards)

	bytesPerShard := combined.Bytes
	if factor > 0 {
		bytesPerShard = combined.Bytes / uint64(factor)
	}

	level.Debug(log).Log(
		append(
			combined.LoggingKeyValues(),
			"msg", "got shard factor",
			"factor", factor,
			"total_bytes", strings.Replace(humanize.Bytes(combined.Bytes), " ", "", 1),
			"bytes_per_shard", strings.Replace(humanize.Bytes(bytesPerShard), " ", "", 1),
		)...,
	)
	return factor, bytesPerShard, nil
}

func (r *dynamicShardResolver) ShardingRanges(expr syntax.Expr, targetBytesPerShard uint64) (
	[]logproto.Shard,
	[]logproto.ChunkRefGroup,
	error,
) {
	log := spanlogger.FromContext(r.ctx)

	var (
		adjustedFrom    = r.from
		adjustedThrough model.Time
	)

	// NB(owen-d): there should only ever be 1 matcher group passed
	// to this call as we call it separately for different legs
	// of binary ops, but I'm putting in the loop for completion
	grps, err := syntax.MatcherGroups(expr)
	if err != nil {
		return nil, nil, err
	}

	for _, grp := range grps {
		diff := grp.Interval

		// For instant queries, when start == end,
		// we have a default lookback which we add here
		if diff == 0 {
			diff = r.defaultLookback
		}

		diff += grp.Offset

		// use the oldest adjustedFrom
		if r.from.Add(-diff).Before(adjustedFrom) {
			adjustedFrom = r.from.Add(-diff)
		}

		// use the latest adjustedThrough
		if r.through.Add(-grp.Offset).After(adjustedThrough) {
			adjustedThrough = r.through.Add(-grp.Offset)
		}
	}

	// handle the case where there are no matchers
	if adjustedThrough == 0 {
		adjustedThrough = r.through
	}

	exprStr := expr.String()
	// try to get shards for the given expression
	// if it fails, fallback to linearshards based on stats
	// use the retry handler here to retry transient errors
	resp, err := r.retryNextHandler.Do(r.ctx, &logproto.ShardsRequest{
		From:                adjustedFrom,
		Through:             adjustedThrough,
		Query:               expr.String(),
		TargetBytesPerShard: targetBytesPerShard,
	})
	if err != nil {
		return nil, nil, errors.Wrapf(err, "failed to get shards for expression, got %T: %+v", err, err)
	}

	casted, ok := resp.(*ShardsResponse)
	if !ok {
		return nil, nil, fmt.Errorf("expected *ShardsResponse while querying index, got %T", resp)
	}

	// accumulate stats
	logqlstats.JoinResults(r.ctx, casted.Response.Statistics)

	var refs int
	for _, x := range casted.Response.ChunkGroups {
		refs += len(x.Refs)
	}

	level.Debug(log).Log(
		"msg", "retrieved sharding ranges",
		"target_bytes_per_shard", targetBytesPerShard,
		"shards", len(casted.Response.Shards),
		"query", exprStr,
		"total_chunks", casted.Response.Statistics.Index.TotalChunks,
		"post_filter_chunks", casted.Response.Statistics.Index.PostFilterChunks,
		"precomputed_refs", refs,
	)

	return casted.Response.Shards, casted.Response.ChunkGroups, err
}
