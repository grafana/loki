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
}

func NewShardMapper(resolver ShardResolver, metrics *MapperMetrics) ShardMapper {
	return ShardMapper{
		shards:  resolver,
		metrics: metrics,
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

// turn a vector aggr into a wrapped+sharded variant,
// used as a subroutine in mapping
func (m ShardMapper) wrappedShardedVectorAggr(expr *syntax.VectorAggregationExpr, r *downstreamRecorder) (*syntax.VectorAggregationExpr, uint64, error) {
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
}

// technically, std{dev,var} are also parallelizable if there is no cross-shard merging
// in descendent nodes in the AST. This optimization is currently avoided for simplicity.
func (m ShardMapper) mapVectorAggregationExpr(expr *syntax.VectorAggregationExpr, r *downstreamRecorder) (syntax.SampleExpr, uint64, error) {
	if expr.Shardable() {

		switch expr.Operation {

		case syntax.OpTypeSum:
			// sum(x) -> sum(sum(x, shard=1) ++ sum(x, shard=2)...)
			return m.wrappedShardedVectorAggr(expr, r)

		case syntax.OpTypeMin, syntax.OpTypeMax:
			if syntax.ReducesLabels(expr) {
				// skip sharding optimizations at this level. If labels are reduced,
				// the same series may exist on multiple shards and must be aggregated
				// together before a max|min is applied
				break
			}
			// max(x) -> max(max(x, shard=1) ++ max(x, shard=2)...)
			// min(x) -> min(min(x, shard=1) ++ min(x, shard=2)...)
			return m.wrappedShardedVectorAggr(expr, r)

		case syntax.OpTypeAvg:
			// avg(x) -> sum(x)/count(x), which is parallelizable
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
			if syntax.ReducesLabels(expr) {
				// skip sharding optimizations at this level. If labels are reduced,
				// the same series may exist on multiple shards and must be aggregated
				// together before a count is applied
				break
			}

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
		default:
			// this should not be reachable. If an operation is shardable it should
			// have an optimization listed. Nonetheless, we log this as a warning
			// and return the original expression unsharded.
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

	// if this AST contains unshardable operations, don't shard this at this level,
	// but attempt to shard a child node.
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
	if !expr.Shardable() {
		exprStats, err := m.shards.GetStats(expr)
		if err != nil {
			return nil, 0, err
		}
		return expr, exprStats.Bytes, nil
	}

	switch expr.Operation {

	case syntax.OpRangeTypeCount, syntax.OpRangeTypeRate, syntax.OpRangeTypeBytes, syntax.OpRangeTypeBytesRate, syntax.OpRangeTypeSum, syntax.OpRangeTypeMax, syntax.OpRangeTypeMin:
		// if the expr can reduce labels, it can cause the same labelset to
		// exist on separate shards and we'll need to merge the results
		// accordingly. If it does not reduce labels and has no special grouping
		// aggregation, we can shard it as normal via concatenation.
		potentialConflict := syntax.ReducesLabels(expr)
		if !potentialConflict && (expr.Grouping == nil || expr.Grouping.Noop()) {
			return m.mapSampleExpr(expr, r)
		}

		// These functions require a different merge strategy than the default
		// concatenation.
		// This is because the same label sets may exist on multiple shards when label-reducing parsing is applied or when
		// grouping by some subset of the labels. In this case, the resulting vector may have multiple values for the same
		// series and we need to combine them appropriately given a particular operation.
		mergeMap := map[string]string{
			// all these may be summed
			syntax.OpRangeTypeCount:     syntax.OpTypeSum,
			syntax.OpRangeTypeRate:      syntax.OpTypeSum,
			syntax.OpRangeTypeBytes:     syntax.OpTypeSum,
			syntax.OpRangeTypeBytesRate: syntax.OpTypeSum,
			syntax.OpRangeTypeSum:       syntax.OpTypeSum,

			// min & max require taking the min|max of the shards
			syntax.OpRangeTypeMin: syntax.OpTypeMin,
			syntax.OpRangeTypeMax: syntax.OpTypeMax,
		}

		// range aggregation groupings default to `without ()` behavior
		// so we explicitly set the wrapping vector aggregation to this
		// for parity when it's not explicitly set
		grouping := expr.Grouping
		if grouping == nil {
			grouping = &syntax.Grouping{Without: true}
		}

		mapped, bytes, err := m.mapSampleExpr(expr, r)
		// max_over_time(_) -> max without() (max_over_time(_) ++ max_over_time(_)...)
		// max_over_time(_) by (foo) -> max by (foo) (max_over_time(_) by (foo) ++ max_over_time(_) by (foo)...)
		merger, ok := mergeMap[expr.Operation]
		if !ok {
			return nil, 0, fmt.Errorf(
				"error while finding merge operation for %s", expr.Operation,
			)
		}
		return &syntax.VectorAggregationExpr{
			Left:      mapped,
			Grouping:  grouping,
			Operation: merger,
		}, bytes, err

	default:
		// don't shard if there's not an appropriate optimization
		exprStats, err := m.shards.GetStats(expr)
		if err != nil {
			return nil, 0, err
		}
		return expr, exprStats.Bytes, nil
	}
}

func badASTMapping(got syntax.Expr) error {
	return fmt.Errorf("bad AST mapping: expected SampleExpr, but got (%T)", got)
}
