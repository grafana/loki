package logql

import (
	"fmt"

	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/util/math"

	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/querier/astmapper"
	"github.com/grafana/loki/pkg/storage/stores/index/stats"
	util_log "github.com/grafana/loki/pkg/util/log"
)

type ShardResolver interface {
	Shards(expr syntax.Expr) (int, uint64, error)
	GetStats(e syntax.Expr) (stats.Stats, error)
}

type ConstantShards int

func (s ConstantShards) Shards(_ syntax.Expr) (int, uint64, error)   { return int(s), 0, nil }
func (s ConstantShards) GetStats(_ syntax.Expr) (stats.Stats, error) { return stats.Stats{}, nil }

type ShardMapper struct {
	shards  ShardResolver
	metrics *MapperMetrics

	probabilisticQueries bool
}

func NewShardMapper(resolver ShardResolver, probabilistic bool, metrics *MapperMetrics) ShardMapper {
	return ShardMapper{
		shards:               resolver,
		metrics:              metrics,
		probabilisticQueries: probabilistic,
	}
}

func NewShardMapperMetrics(registerer prometheus.Registerer) *MapperMetrics {
	return newMapperMetrics(registerer, "shard")
}

func (m ShardMapper) Parse(query string) (noop bool, bytesPerShard uint64, expr syntax.Expr, err error) {
	parsed, err := syntax.ParseExpr(query)
	if err != nil {
		return false, 0, nil, err
	}

	recorder := m.metrics.downstreamRecorder()

	mapped, bytesPerShard, err := m.Map(parsed, recorder)
	if err != nil {
		m.metrics.ParsedQueries.WithLabelValues(FailureKey).Inc()
		return false, 0, nil, err
	}

	originalStr := parsed.String()
	mappedStr := mapped.String()
	noop = originalStr == mappedStr
	if noop {
		m.metrics.ParsedQueries.WithLabelValues(NoopKey).Inc()
	} else {
		m.metrics.ParsedQueries.WithLabelValues(SuccessKey).Inc()
	}

	recorder.Finish() // only record metrics for successful mappings

	return noop, bytesPerShard, mapped, err
}

func (m ShardMapper) Map(expr syntax.Expr, r *downstreamRecorder) (syntax.Expr, uint64, error) {
	// immediately clone the passed expr to avoid mutating the original
	expr, err := syntax.Clone(expr)
	if err != nil {
		return nil, 0, err
	}

	switch e := expr.(type) {
	case *syntax.LiteralExpr:
		return e, 0, nil
	case *syntax.VectorExpr:
		return e, 0, nil
	case *syntax.MatchersExpr, *syntax.PipelineExpr:
		return m.mapLogSelectorExpr(e.(syntax.LogSelectorExpr), r)
	case *syntax.VectorAggregationExpr:
		return m.mapVectorAggregationExpr(e, r)
	case *syntax.LabelReplaceExpr:
		return m.mapLabelReplaceExpr(e, r)
	case *syntax.RangeAggregationExpr:
		return m.mapRangeAggregationExpr(e, r)
	case *syntax.BinOpExpr:
		lhsMapped, lhsBytesPerShard, err := m.Map(e.SampleExpr, r)
		if err != nil {
			return nil, 0, err
		}
		rhsMapped, rhsBytesPerShard, err := m.Map(e.RHS, r)
		if err != nil {
			return nil, 0, err
		}
		lhsSampleExpr, ok := lhsMapped.(syntax.SampleExpr)
		if !ok {
			return nil, 0, badASTMapping(lhsMapped)
		}
		rhsSampleExpr, ok := rhsMapped.(syntax.SampleExpr)
		if !ok {
			return nil, 0, badASTMapping(rhsMapped)
		}
		e.SampleExpr = lhsSampleExpr
		e.RHS = rhsSampleExpr

		// We take the maximum bytes per shard of both sides of the operation
		bytesPerShard := uint64(math.Max(int(lhsBytesPerShard), int(rhsBytesPerShard)))

		return e, bytesPerShard, nil
	default:
		return nil, 0, errors.Errorf("unexpected expr type (%T) for ASTMapper type (%T) ", expr, m)
	}
}

func (m ShardMapper) mapLogSelectorExpr(expr syntax.LogSelectorExpr, r *downstreamRecorder) (syntax.LogSelectorExpr, uint64, error) {
	var head *ConcatLogSelectorExpr
	shards, bytesPerShard, err := m.shards.Shards(expr)
	if err != nil {
		return nil, 0, err
	}
	if shards == 0 {
		return &ConcatLogSelectorExpr{
			DownstreamLogSelectorExpr: DownstreamLogSelectorExpr{
				shard:           nil,
				LogSelectorExpr: expr,
			},
		}, bytesPerShard, nil
	}
	for i := shards - 1; i >= 0; i-- {
		head = &ConcatLogSelectorExpr{
			DownstreamLogSelectorExpr: DownstreamLogSelectorExpr{
				shard: &astmapper.ShardAnnotation{
					Shard: i,
					Of:    shards,
				},
				LogSelectorExpr: expr,
			},
			next: head,
		}
	}
	r.Add(shards, StreamsKey)

	return head, bytesPerShard, nil
}

func (m ShardMapper) mapSampleExpr(expr syntax.SampleExpr, r *downstreamRecorder) (syntax.SampleExpr, uint64, error) {
	var head *ConcatSampleExpr
	shards, bytesPerShard, err := m.shards.Shards(expr)
	if err != nil {
		return nil, 0, err
	}
	if shards == 0 {
		return &ConcatSampleExpr{
			DownstreamSampleExpr: DownstreamSampleExpr{
				shard:      nil,
				SampleExpr: expr,
			},
		}, bytesPerShard, nil
	}
	for i := shards - 1; i >= 0; i-- {
		head = &ConcatSampleExpr{
			DownstreamSampleExpr: DownstreamSampleExpr{
				shard: &astmapper.ShardAnnotation{
					Shard: i,
					Of:    shards,
				},
				SampleExpr: expr,
			},
			next: head,
		}
	}
	r.Add(shards, MetricsKey)

	return head, bytesPerShard, nil
}

func (m ShardMapper) mapTopKSampleExpr(expr syntax.TopkSampleExpr, r *downstreamRecorder) (syntax.TopkSampleExpr, uint64, error) {
	var head *TopkMergeSampleExpr
	shards, bytesPerShard, err := m.shards.Shards(expr)
	if err != nil {
		return nil, 0, err
	}
	if shards == 0 {
		return &TopkMergeSampleExpr{
			DownstreamTopkSampleExpr: DownstreamTopkSampleExpr{
				shard:          nil,
				TopkSampleExpr: expr,
			},
		}, bytesPerShard, nil
	}
	for i := shards - 1; i >= 0; i-- {
		head = &TopkMergeSampleExpr{
			DownstreamTopkSampleExpr: DownstreamTopkSampleExpr{
				shard: &astmapper.ShardAnnotation{
					Shard: i,
					Of:    shards,
				},
				TopkSampleExpr: expr,
			},
			next: head,
		}
	}
	r.Add(shards, MetricsKey)

	return head, bytesPerShard, nil
}

// technically, std{dev,var} are also parallelizable if there is no cross-shard merging
// in descendent nodes in the AST. This optimization is currently avoided for simplicity.
func (m ShardMapper) mapVectorAggregationExpr(expr *syntax.VectorAggregationExpr, r *downstreamRecorder) (syntax.SampleExpr, uint64, error) {
	// if this AST contains unshardable operations, don't shard this at this level,
	// but attempt to shard a child node.
	if !expr.Shardable() || expr.Operation == syntax.OpTypeTopK && !m.probabilisticQueries {
		subMapped, bytesPerShard, err := m.Map(expr.Left, r)
		if err != nil {
			return nil, 0, err
		}
		sampleExpr, ok := subMapped.(syntax.SampleExpr)
		if !ok {
			return nil, 0, badASTMapping(subMapped)
		}

		return &syntax.VectorAggregationExpr{
			Left:      sampleExpr,
			Grouping:  expr.Grouping,
			Params:    expr.Params,
			Operation: expr.Operation,
		}, bytesPerShard, nil

	}

	switch expr.Operation {
	case syntax.OpTypeSum:
		// sum(x) -> sum(sum(x, shard=1) ++ sum(x, shard=2)...)
		sharded, bytesPerShard, err := m.mapSampleExpr(expr, r)
		if err != nil {
			return nil, 0, err
		}
		return &syntax.VectorAggregationExpr{
			Left:      sharded,
			Grouping:  expr.Grouping,
			Params:    expr.Params,
			Operation: expr.Operation,
		}, bytesPerShard, nil

	case syntax.OpTypeAvg:
		// avg(x) -> sum(x)/count(x)
		lhs, lhsBytesPerShard, err := m.mapVectorAggregationExpr(&syntax.VectorAggregationExpr{
			Left:      expr.Left,
			Grouping:  expr.Grouping,
			Operation: syntax.OpTypeSum,
		}, r)
		if err != nil {
			return nil, 0, err
		}
		rhs, rhsBytesPerShard, err := m.mapVectorAggregationExpr(&syntax.VectorAggregationExpr{
			Left:      expr.Left,
			Grouping:  expr.Grouping,
			Operation: syntax.OpTypeCount,
		}, r)
		if err != nil {
			return nil, 0, err
		}

		// We take the maximum bytes per shard of both sides of the operation
		bytesPerShard := uint64(math.Max(int(lhsBytesPerShard), int(rhsBytesPerShard)))

		return &syntax.BinOpExpr{
			SampleExpr: lhs,
			RHS:        rhs,
			Op:         syntax.OpTypeDiv,
		}, bytesPerShard, nil

	case syntax.OpTypeCount:
		// count(x) -> sum(count(x, shard=1) ++ count(x, shard=2)...)
		sharded, bytesPerShard, err := m.mapSampleExpr(expr, r)
		if err != nil {
			return nil, 0, err
		}
		return &syntax.VectorAggregationExpr{
			Left:      sharded,
			Grouping:  expr.Grouping,
			Operation: syntax.OpTypeSum,
		}, bytesPerShard, nil

	case syntax.OpTypeTopK:
		var g *syntax.Grouping
		// this smells, not sure why we need to do this but somehow
		// if a query doesn't have a grouping the expr.Grouping field
		// contains an empty string rather than a nil pointer
		if expr.Grouping != nil && expr.Grouping.String() != "" {
			g = expr.Grouping
		}
		expr.Grouping = g
		// each step of a sharded topk is a set of topk sketch structs whose count-min sketches can be merged
		sharded, bytesPerShard, err := m.mapTopKSampleExpr(expr, r)
		if err != nil {
			return nil, 0, err
		}

		return &syntax.VectorAggregationExpr{
			Left:      sharded,
			Grouping:  g,
			Operation: syntax.OpTypeTopKMerge,
		}, bytesPerShard, nil
	default:
		// this should not be reachable. If an operation is shardable it should
		// have an optimization listed.
		level.Warn(util_log.Logger).Log(
			"msg", "unexpected operation which appears shardable, ignoring",
			"operation", expr.Operation,
		)
		exprStats, err := m.shards.GetStats(expr)
		if err != nil {
			return nil, 0, err
		}
		return expr, exprStats.Bytes, nil
	}
}

func (m ShardMapper) mapLabelReplaceExpr(expr *syntax.LabelReplaceExpr, r *downstreamRecorder) (syntax.SampleExpr, uint64, error) {
	subMapped, bytesPerShard, err := m.Map(expr.Left, r)
	if err != nil {
		return nil, 0, err
	}
	cpy := *expr
	cpy.Left = subMapped.(syntax.SampleExpr)
	return &cpy, bytesPerShard, nil
}

func (m ShardMapper) mapRangeAggregationExpr(expr *syntax.RangeAggregationExpr, r *downstreamRecorder) (syntax.SampleExpr, uint64, error) {
	if hasLabelModifier(expr) {
		// if an expr can modify labels this means multiple shards can return the same labelset.
		// When this happens the merge strategy needs to be different from a simple concatenation.
		// For instance for rates we need to sum data from different shards but same series.
		// Since we currently support only concatenation as merge strategy, we skip those queries.
		exprStats, err := m.shards.GetStats(expr)
		if err != nil {
			return nil, 0, err
		}

		return expr, exprStats.Bytes, nil
	}

	switch expr.Operation {
	case syntax.OpRangeTypeCount, syntax.OpRangeTypeRate, syntax.OpRangeTypeBytesRate, syntax.OpRangeTypeBytes:
		// count_over_time(x) -> count_over_time(x, shard=1) ++ count_over_time(x, shard=2)...
		// rate(x) -> rate(x, shard=1) ++ rate(x, shard=2)...
		// same goes for bytes_rate and bytes_over_time
		return m.mapSampleExpr(expr, r)
	default:
		// This part of the query is not shardable, so the bytesPerShard is the bytes for all the log matchers in expr
		exprStats, err := m.shards.GetStats(expr)
		if err != nil {
			return nil, 0, err
		}

		return expr, exprStats.Bytes, nil
	}
}

// hasLabelModifier tells if an expression contains pipelines that can modify stream labels
// parsers introduce new labels but does not alter original one for instance.
func hasLabelModifier(expr *syntax.RangeAggregationExpr) bool {
	switch ex := expr.Left.Left.(type) {
	case *syntax.MatchersExpr:
		return false
	case *syntax.PipelineExpr:
		for _, p := range ex.MultiStages {
			if _, ok := p.(*syntax.LabelFmtExpr); ok {
				return true
			}
		}
	}
	return false
}

func badASTMapping(got syntax.Expr) error {
	return fmt.Errorf("bad AST mapping: expected SampleExpr, but got (%T)", got)
}
