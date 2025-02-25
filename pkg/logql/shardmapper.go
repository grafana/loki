package logql

import (
	"fmt"

	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

const (
	ShardLastOverTime     = "last_over_time"
	ShardFirstOverTime    = "first_over_time"
	ShardQuantileOverTime = "quantile_over_time"
	SupportApproxTopk     = "approx_topk"
)

type ShardMapper struct {
	shards                   ShardingStrategy
	metrics                  *MapperMetrics
	quantileOverTimeSharding bool
	lastOverTimeSharding     bool
	firstOverTimeSharding    bool
	approxTopkSupport        bool
}

func NewShardMapper(strategy ShardingStrategy, metrics *MapperMetrics, shardAggregation []string) ShardMapper {
	mapper := ShardMapper{
		shards:                   strategy,
		metrics:                  metrics,
		quantileOverTimeSharding: false,
		lastOverTimeSharding:     false,
		firstOverTimeSharding:    false,
		approxTopkSupport:        false,
	}
	for _, a := range shardAggregation {
		switch a {
		case ShardQuantileOverTime:
			mapper.quantileOverTimeSharding = true
		case ShardLastOverTime:
			mapper.lastOverTimeSharding = true
		case ShardFirstOverTime:
			mapper.firstOverTimeSharding = true
		case SupportApproxTopk:
			mapper.approxTopkSupport = true
		}
	}

	return mapper
}

func NewShardMapperMetrics(registerer prometheus.Registerer) *MapperMetrics {
	return newMapperMetrics(registerer, "shard")
}

func (m ShardMapper) Parse(parsed syntax.Expr) (noop bool, bytesPerShard uint64, expr syntax.Expr, err error) {
	recorder := m.metrics.downstreamRecorder()

	mapped, bytesPerShard, err := m.Map(parsed, recorder, true)
	if err != nil {
		m.metrics.ParsedQueries.WithLabelValues(FailureKey).Inc()
		return false, 0, nil, err
	}

	noop = isNoOp(parsed, mapped)
	if noop {
		m.metrics.ParsedQueries.WithLabelValues(NoopKey).Inc()
	} else {
		m.metrics.ParsedQueries.WithLabelValues(SuccessKey).Inc()
	}

	recorder.Finish() // only record metrics for successful mappings

	return noop, bytesPerShard, mapped, err
}

func (m ShardMapper) Map(expr syntax.Expr, r *downstreamRecorder, topLevel bool) (syntax.Expr, uint64, error) {
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
	case *syntax.MultiVariantExpr:
		// TODO(twhitney): this should be possible to support but hasn't been implemented yet
		return e, 0, nil
	case *syntax.MatchersExpr, *syntax.PipelineExpr:
		return m.mapLogSelectorExpr(e.(syntax.LogSelectorExpr), r)
	case *syntax.VectorAggregationExpr:
		return m.mapVectorAggregationExpr(e, r, topLevel)
	case *syntax.LabelReplaceExpr:
		return m.mapLabelReplaceExpr(e, r, topLevel)
	case *syntax.RangeAggregationExpr:
		return m.mapRangeAggregationExpr(e, r, topLevel)
	case *syntax.BinOpExpr:
		return m.mapBinOpExpr(e, r, topLevel)
	default:
		return nil, 0, errors.Errorf("unexpected expr type (%T) for ASTMapper type (%T) ", expr, m)
	}
}

func (m ShardMapper) mapBinOpExpr(e *syntax.BinOpExpr, r *downstreamRecorder, topLevel bool) (*syntax.BinOpExpr, uint64, error) {
	// In a BinOp expression both sides need to be either executed locally or wrapped
	// into a downstream expression to be executed on the querier, since the default
	// evaluator on the query frontend cannot select logs or samples.
	// However, it can evaluate literals and vectors.

	// check if LHS is shardable by mapping the tree
	// only wrap in downstream expression if the mapping is a no-op and the
	// expression is a vector or literal
	lhsMapped, lhsBytesPerShard, err := m.Map(e.SampleExpr, r, topLevel)
	if err != nil {
		return nil, 0, err
	}
	if isNoOp(e.SampleExpr, lhsMapped) && !isLiteralOrVector(lhsMapped) {
		lhsMapped = DownstreamSampleExpr{
			shard:      nil,
			SampleExpr: e.SampleExpr,
		}
	}

	// check if RHS is shardable by mapping the tree
	// only wrap in downstream expression if the mapping is a no-op and the
	// expression is a vector or literal
	rhsMapped, rhsBytesPerShard, err := m.Map(e.RHS, r, topLevel)
	if err != nil {
		return nil, 0, err
	}
	if isNoOp(e.RHS, rhsMapped) && !isLiteralOrVector(rhsMapped) {
		// TODO: check if literal or vector
		rhsMapped = DownstreamSampleExpr{
			shard:      nil,
			SampleExpr: e.RHS,
		}
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
	bytesPerShard := uint64(max(int(lhsBytesPerShard), int(rhsBytesPerShard)))

	return e, bytesPerShard, nil
}

func (m ShardMapper) mapLogSelectorExpr(expr syntax.LogSelectorExpr, r *downstreamRecorder) (syntax.LogSelectorExpr, uint64, error) {
	var head *ConcatLogSelectorExpr
	shards, maxBytesPerShard, err := m.shards.Shards(expr)
	if err != nil {
		return nil, 0, err
	}
	if len(shards) == 0 {
		return &ConcatLogSelectorExpr{
			DownstreamLogSelectorExpr: DownstreamLogSelectorExpr{
				shard:           nil,
				LogSelectorExpr: expr,
			},
		}, maxBytesPerShard, nil
	}

	for i := len(shards) - 1; i >= 0; i-- {
		head = &ConcatLogSelectorExpr{
			DownstreamLogSelectorExpr: DownstreamLogSelectorExpr{
				shard:           &shards[i],
				LogSelectorExpr: expr,
			},
			next: head,
		}
	}

	r.Add(len(shards), StreamsKey)
	return head, maxBytesPerShard, nil
}

func (m ShardMapper) mapSampleExpr(expr syntax.SampleExpr, r *downstreamRecorder) (syntax.SampleExpr, uint64, error) {
	var head *ConcatSampleExpr
	shards, maxBytesPerShard, err := m.shards.Shards(expr)
	if err != nil {
		return nil, 0, err
	}

	if len(shards) == 0 {
		return &ConcatSampleExpr{
			DownstreamSampleExpr: DownstreamSampleExpr{
				shard:      nil,
				SampleExpr: expr,
			},
		}, maxBytesPerShard, nil
	}

	for i := len(shards) - 1; i >= 0; i-- {
		head = &ConcatSampleExpr{
			DownstreamSampleExpr: DownstreamSampleExpr{
				shard:      &shards[i],
				SampleExpr: expr,
			},
			next: head,
		}
	}

	r.Add(len(shards), MetricsKey)

	return head, maxBytesPerShard, nil
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
func (m ShardMapper) mapVectorAggregationExpr(expr *syntax.VectorAggregationExpr, r *downstreamRecorder, topLevel bool) (syntax.SampleExpr, uint64, error) {
	if expr.Shardable(topLevel) {
		switch expr.Operation {

		case syntax.OpTypeSum:
			// sum(x) -> sum(sum(x, shard=1) ++ sum(x, shard=2)...)
			return m.wrappedShardedVectorAggr(expr, r)

		case syntax.OpTypeMin, syntax.OpTypeMax:
			if syntax.ReducesLabels(expr.Left) {
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
			binOp := &syntax.BinOpExpr{
				SampleExpr: &syntax.VectorAggregationExpr{
					Left:      expr.Left,
					Grouping:  expr.Grouping,
					Operation: syntax.OpTypeSum,
				},
				RHS: &syntax.VectorAggregationExpr{
					Left:      expr.Left,
					Grouping:  expr.Grouping,
					Operation: syntax.OpTypeCount,
				},
				Op: syntax.OpTypeDiv,
			}

			return m.mapBinOpExpr(binOp, r, topLevel)
		case syntax.OpTypeCount:
			if syntax.ReducesLabels(expr.Left) {
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
		case syntax.OpTypeApproxTopK:
			if !m.approxTopkSupport {
				return nil, 0, fmt.Errorf("approx_topk is not enabled. See -limits.shard_aggregations")
			}

			return m.mapApproxTopk(expr, false)
		default:
			// this should not be reachable. If an operation is shardable it should
			// have an optimization listed. Nonetheless, we log this as a warning
			// and return the original expression unsharded.
			level.Warn(util_log.Logger).Log(
				"msg", "unexpected operation which appears shardable, ignoring",
				"operation", expr.Operation,
			)
			exprStats, err := m.shards.Resolver().GetStats(expr)
			if err != nil {
				return nil, 0, err
			}
			return expr, exprStats.Bytes, nil
		}
	} else {
		// if this AST contains unshardable operations, we still need to rewrite some operations (e.g. approx_topk) as if it had 0 shards as they are not supported on the querier
		switch expr.Operation {
		case syntax.OpTypeApproxTopK:
			level.Error(util_log.Logger).Log(
				"msg", "encountered unshardable approx_topk operation",
				"operation", expr.Operation,
			)
			return m.mapApproxTopk(expr, true)
		}
	}

	// if this AST contains unshardable operations, don't shard this at this level,
	// but attempt to shard a child node.
	subMapped, bytesPerShard, err := m.Map(expr.Left, r, false)
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

func (m ShardMapper) mapApproxTopk(expr *syntax.VectorAggregationExpr, forceNoShard bool) (*syntax.VectorAggregationExpr, uint64, error) {
	// TODO(owen-d): integrate bounded sharding with approx_topk
	// I'm not doing this now because it uses a separate code path and may not handle
	// bounded shards in the same way
	shards, bytesPerShard, err := m.shards.Resolver().Shards(expr)
	if err != nil {
		return nil, 0, err
	}

	// approx_topk(k, inner) ->
	// topk(
	//   k,
	//   eval_cms(
	//     __count_min_sketch__(inner, shard=1) ++ __count_min_sketch__(inner, shard=2)...
	//   )
	// )

	countMinSketchExpr := syntax.MustClone(expr)
	countMinSketchExpr.Operation = syntax.OpTypeCountMinSketch
	countMinSketchExpr.Params = 0

	// Even if this query is not sharded the user wants an approximation. This is helpful if some
	// inferred label has a very high cardinality. Note that the querier does not support CountMinSketchEvalExpr
	// which is why it's evaluated on the front end.
	if shards == 0 || forceNoShard {
		return &syntax.VectorAggregationExpr{
			Left: &CountMinSketchEvalExpr{
				downstreams: []DownstreamSampleExpr{{
					SampleExpr: countMinSketchExpr,
				}},
			},
			Grouping:  expr.Grouping,
			Operation: syntax.OpTypeTopK,
			Params:    expr.Params,
		}, bytesPerShard, nil
	}

	downstreams := make([]DownstreamSampleExpr, 0, shards)
	for shard := 0; shard < shards; shard++ {
		s := NewPowerOfTwoShard(index.ShardAnnotation{
			Shard: uint32(shard),
			Of:    uint32(shards),
		})
		downstreams = append(downstreams, DownstreamSampleExpr{
			shard: &ShardWithChunkRefs{
				Shard: s,
			},
			SampleExpr: countMinSketchExpr,
		})
	}

	sharded := &CountMinSketchEvalExpr{
		downstreams: downstreams,
	}

	return &syntax.VectorAggregationExpr{
		Left:      sharded,
		Grouping:  expr.Grouping,
		Operation: syntax.OpTypeTopK,
		Params:    expr.Params,
	}, bytesPerShard, nil
}

func (m ShardMapper) mapLabelReplaceExpr(expr *syntax.LabelReplaceExpr, r *downstreamRecorder, topLevel bool) (syntax.SampleExpr, uint64, error) {
	subMapped, bytesPerShard, err := m.Map(expr.Left, r, topLevel)
	if err != nil {
		return nil, 0, err
	}
	cpy := *expr
	cpy.Left = subMapped.(syntax.SampleExpr)
	return &cpy, bytesPerShard, nil
}

// These functions require a different merge strategy than the default
// concatenation.
// This is because the same label sets may exist on multiple shards when label-reducing parsing is applied or when
// grouping by some subset of the labels. In this case, the resulting vector may have multiple values for the same
// series and we need to combine them appropriately given a particular operation.
var rangeMergeMap = map[string]string{
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

func (m ShardMapper) mapRangeAggregationExpr(expr *syntax.RangeAggregationExpr, r *downstreamRecorder, topLevel bool) (syntax.SampleExpr, uint64, error) {
	if !expr.Shardable(topLevel) {
		return noOp(expr, m.shards.Resolver())
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
		merger, ok := rangeMergeMap[expr.Operation]
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

	case syntax.OpRangeTypeAvg:
		potentialConflict := syntax.ReducesLabels(expr)
		if !potentialConflict && (expr.Grouping == nil || expr.Grouping.Noop()) {
			return m.mapSampleExpr(expr, r)
		}

		grouping := expr.Grouping
		if grouping == nil {
			grouping = &syntax.Grouping{Without: true}
		}

		// avg_over_time() by (foo) -> sum by (foo) (sum_over_time()) / sum by (foo) (count_over_time())
		lhs, lhsBytesPerShard, err := m.mapVectorAggregationExpr(&syntax.VectorAggregationExpr{
			Left: &syntax.RangeAggregationExpr{
				Left:      expr.Left,
				Operation: syntax.OpRangeTypeSum,
			},
			Grouping:  grouping,
			Operation: syntax.OpTypeSum,
		}, r, false)
		if err != nil {
			return nil, 0, err
		}

		// Strip unwrap from log range
		countOverTimeSelector, err := expr.Left.WithoutUnwrap()
		if err != nil {
			return nil, 0, err
		}

		// labelSampleExtractor includes the unwrap identifier in without() list if no grouping is specified
		// similar change is required for the RHS here to ensure the resulting label sets match
		rhsGrouping := *grouping
		if rhsGrouping.Without {
			if expr.Left.Unwrap != nil {
				rhsGrouping.Groups = append(rhsGrouping.Groups, expr.Left.Unwrap.Identifier)
			}
		}

		rhs, rhsBytesPerShard, err := m.mapVectorAggregationExpr(&syntax.VectorAggregationExpr{
			Left: &syntax.RangeAggregationExpr{
				Left:      countOverTimeSelector,
				Operation: syntax.OpRangeTypeCount,
			},
			Grouping:  &rhsGrouping,
			Operation: syntax.OpTypeSum,
		}, r, false)
		if err != nil {
			return nil, 0, err
		}

		// We take the maximum bytes per shard of both sides of the operation
		bytesPerShard := uint64(max(int(lhsBytesPerShard), int(rhsBytesPerShard)))

		return &syntax.BinOpExpr{
			SampleExpr: lhs,
			RHS:        rhs,
			Op:         syntax.OpTypeDiv,
		}, bytesPerShard, nil

	case syntax.OpRangeTypeQuantile:
		if !m.quantileOverTimeSharding {
			return noOp(expr, m.shards.Resolver())
		}

		potentialConflict := syntax.ReducesLabels(expr)
		if !potentialConflict && (expr.Grouping == nil || expr.Grouping.Noop()) {
			return m.mapSampleExpr(expr, r)
		}

		// TODO(owen-d): integrate bounded sharding with quantile over time
		// I'm not doing this now because it uses a separate code path and may not handle
		// bounded shards in the same way
		shards, bytesPerShard, err := m.shards.Resolver().Shards(expr)
		if err != nil {
			return nil, 0, err
		}
		if shards == 0 {
			return noOp(expr, m.shards.Resolver())
		}

		// quantile_over_time() by (foo) ->
		// quantile_sketch_eval(quantile_merge by (foo)
		// (__quantile_sketch_over_time__() by (foo)))

		downstreams := make([]DownstreamSampleExpr, 0, shards)
		expr.Operation = syntax.OpRangeTypeQuantileSketch
		for shard := shards - 1; shard >= 0; shard-- {
			s := NewPowerOfTwoShard(index.ShardAnnotation{
				Shard: uint32(shard),
				Of:    uint32(shards),
			})
			downstreams = append(downstreams, DownstreamSampleExpr{
				shard: &ShardWithChunkRefs{
					Shard: s,
				},
				SampleExpr: expr,
			})
		}

		return &QuantileSketchEvalExpr{
			quantileMergeExpr: &QuantileSketchMergeExpr{
				downstreams: downstreams,
			},
			quantile: expr.Params,
		}, bytesPerShard, nil

	case syntax.OpRangeTypeFirst:
		if !m.firstOverTimeSharding {
			return noOp(expr, m.shards.Resolver())
		}

		potentialConflict := syntax.ReducesLabels(expr)
		if !potentialConflict && (expr.Grouping == nil || expr.Grouping.Noop()) {
			return m.mapSampleExpr(expr, r)
		}

		shards, bytesPerShard, err := m.shards.Shards(expr)
		if err != nil {
			return nil, 0, err
		}
		if len(shards) == 0 {
			return noOp(expr, m.shards.Resolver())
		}

		downstreams := make([]DownstreamSampleExpr, 0, len(shards))
		// This is the magic. We send a custom operation
		expr.Operation = syntax.OpRangeTypeFirstWithTimestamp
		for i := len(shards) - 1; i >= 0; i-- {
			downstreams = append(downstreams, DownstreamSampleExpr{
				shard:      &shards[i],
				SampleExpr: expr,
			})
		}

		return &MergeFirstOverTimeExpr{
			downstreams: downstreams,
			offset:      expr.Left.Offset,
		}, bytesPerShard, nil
	case syntax.OpRangeTypeLast:
		if !m.lastOverTimeSharding {
			return noOp(expr, m.shards.Resolver())
		}

		potentialConflict := syntax.ReducesLabels(expr)
		if !potentialConflict && (expr.Grouping == nil || expr.Grouping.Noop()) {
			return m.mapSampleExpr(expr, r)
		}

		shards, bytesPerShard, err := m.shards.Shards(expr)
		if err != nil {
			return nil, 0, err
		}
		if len(shards) == 0 {
			return noOp(expr, m.shards.Resolver())
		}

		downstreams := make([]DownstreamSampleExpr, 0, len(shards))
		expr.Operation = syntax.OpRangeTypeLastWithTimestamp
		for i := len(shards) - 1; i >= 0; i-- {
			downstreams = append(downstreams, DownstreamSampleExpr{
				shard:      &shards[i],
				SampleExpr: expr,
			})
		}

		return &MergeLastOverTimeExpr{
			downstreams: downstreams,
			offset:      expr.Left.Offset,
		}, bytesPerShard, nil
	default:
		// don't shard if there's not an appropriate optimization
		return noOp(expr, m.shards.Resolver())
	}
}

func noOp[E syntax.Expr](expr E, shards ShardResolver) (E, uint64, error) {
	exprStats, err := shards.GetStats(expr)
	if err != nil {
		var empty E
		return empty, 0, err
	}
	return expr, exprStats.Bytes, nil
}

func isNoOp(left syntax.Expr, right syntax.Expr) bool {
	return left.String() == right.String()
}

func isLiteralOrVector(e syntax.Expr) bool {
	switch e.(type) {
	case *syntax.VectorExpr, *syntax.LiteralExpr:
		return true
	default:
		return false
	}
}

func badASTMapping(got syntax.Expr) error {
	return fmt.Errorf("bad AST mapping: expected SampleExpr, but got (%T)", got)
}
