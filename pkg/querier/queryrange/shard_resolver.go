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
	"github.com/opentracing/opentracing-go"
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
	maxShards int,
	r queryrangebase.Request,
	handler queryrangebase.Handler,
	limits Limits,
) (logql.ShardResolver, bool) {
	if conf.IndexType == config.TSDBType {
		return &dynamicShardResolver{
			ctx:             ctx,
			logger:          logger,
			handler:         handler,
			limits:          limits,
			from:            model.Time(r.GetStart()),
			through:         model.Time(r.GetEnd()),
			maxParallelism:  maxParallelism,
			maxShards:       maxShards,
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
	limits  Limits

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
	log := spanlogger.FromContext(r.ctx)
	defer log.Finish()

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

	results, err := getStatsForMatchers(ctx, log, r.handler, r.from, r.through, grps, r.maxParallelism, r.defaultLookback)
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
	r.ctx = ctx

	defer sp.Finish()
	log := spanlogger.FromContext(ctx)
	defer log.Finish()

	combined, err := r.GetStats(e)
	if err != nil {
		return 0, 0, err
	}

	factor := guessShardFactor(combined, r.maxShards)

	var bytesPerShard = combined.Bytes
	if factor > 0 {
		bytesPerShard = combined.Bytes / uint64(factor)
	}

	level.Debug(log).Log(
		append(
			combined.LoggingKeyValues(),
			"msg", "Got shard factor",
			"factor", factor,
			"bytes_per_shard", strings.Replace(humanize.Bytes(bytesPerShard), " ", "", 1),
		)...,
	)
	return factor, bytesPerShard, nil
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
func guessShardFactor(stats stats.Stats, maxShards int) int {
	minShards := float64(stats.Bytes) / float64(maxBytesPerShard)

	// round up to nearest power of 2
	power := math.Ceil(math.Log2(minShards))

	// Since x^0 == 1 and we only support factors of 2
	// reset this edge case manually
	factor := int(math.Pow(2, power))
	if maxShards > 0 {
		factor = min(factor, maxShards)
	}

	// shortcut: no need to run any sharding logic when factor=1
	// as it's the same as no sharding
	if factor == 1 {
		factor = 0
	}
	return factor
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
