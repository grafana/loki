package logql

import (
	"fmt"

	"github.com/cortexproject/cortex/pkg/querier/astmapper"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// keys used in metrics
const (
	StreamsKey = "streams"
	MetricsKey = "metrics"
	SuccessKey = "success"
	FailureKey = "failure"
	NoopKey    = "noop"
)

// ShardingMetrics is the metrics wrapper used in shard mapping
type ShardingMetrics struct {
	shards      *prometheus.CounterVec // sharded queries total, partitioned by (streams/metric)
	parsed      *prometheus.CounterVec // parsed ASTs total, partitioned by (success/failure/noop)
	shardFactor prometheus.Histogram   // per request shard factor
}

func NewShardingMetrics(registerer prometheus.Registerer) *ShardingMetrics {

	return &ShardingMetrics{
		shards: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki",
			Name:      "query_frontend_shards_total",
		}, []string{"type"}),
		parsed: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki",
			Name:      "query_frontend_sharding_parsed_queries_total",
		}, []string{"type"}),
		shardFactor: promauto.With(registerer).NewHistogram(prometheus.HistogramOpts{
			Namespace: "loki",
			Name:      "query_frontend_shard_factor",
			Help:      "Number of shards per request",
			Buckets:   prometheus.LinearBuckets(0, 16, 4), // 16 is the default shard factor for later schemas
		}),
	}
}

// shardRecorder constructs a recorder using the underlying metrics.
func (m *ShardingMetrics) shardRecorder() *shardRecorder {
	return &shardRecorder{
		ShardingMetrics: m,
	}
}

// shardRecorder wraps a vector & histogram, providing an easy way to increment sharding counts.
// and unify them into histogram entries.
// NOT SAFE FOR CONCURRENT USE! We avoid introducing mutex locking here
// because AST mapping is single threaded.
type shardRecorder struct {
	done  bool
	total int
	*ShardingMetrics
}

// Add increments both the shard count and tracks it for the eventual histogram entry.
func (r *shardRecorder) Add(x int, key string) {
	r.total += x
	r.shards.WithLabelValues(key).Add(float64(x))
}

// Finish idemptotently records a histogram entry with the total shard factor.
func (r *shardRecorder) Finish() {
	if !r.done {
		r.done = true
		r.shardFactor.Observe(float64(r.total))
	}
}

func badASTMapping(expected string, got Expr) error {
	return fmt.Errorf("Bad AST mapping: expected one type (%s), but got (%T)", expected, got)
}

func NewShardMapper(shards int, metrics *ShardingMetrics) (ShardMapper, error) {
	if shards < 2 {
		return ShardMapper{}, fmt.Errorf("Cannot create ShardMapper with <2 shards. Received %d", shards)
	}
	return ShardMapper{
		shards:  shards,
		metrics: metrics,
	}, nil
}

type ShardMapper struct {
	shards  int
	metrics *ShardingMetrics
}

func (m ShardMapper) Parse(query string) (noop bool, expr Expr, err error) {
	parsed, err := ParseExpr(query)
	if err != nil {
		return false, nil, err
	}

	recorder := m.metrics.shardRecorder()

	mapped, err := m.Map(parsed, recorder)
	if err != nil {
		m.metrics.parsed.WithLabelValues(FailureKey).Inc()
		return false, nil, err
	}

	mappedStr := mapped.String()
	originalStr := parsed.String()
	noop = originalStr == mappedStr
	if noop {
		m.metrics.parsed.WithLabelValues(NoopKey).Inc()
	} else {
		m.metrics.parsed.WithLabelValues(SuccessKey).Inc()
	}

	recorder.Finish() // only record metrics for successful mappings

	return noop, mapped, err
}

func (m ShardMapper) Map(expr Expr, r *shardRecorder) (Expr, error) {
	switch e := expr.(type) {
	case *literalExpr:
		return e, nil
	case *matchersExpr, *pipelineExpr:
		return m.mapLogSelectorExpr(e.(LogSelectorExpr), r), nil
	case *vectorAggregationExpr:
		return m.mapVectorAggregationExpr(e, r)
	case *labelReplaceExpr:
		return m.mapLabelReplaceExpr(e, r)
	case *rangeAggregationExpr:
		return m.mapRangeAggregationExpr(e, r), nil
	case *binOpExpr:
		lhsMapped, err := m.Map(e.SampleExpr, r)
		if err != nil {
			return nil, err
		}
		rhsMapped, err := m.Map(e.RHS, r)
		if err != nil {
			return nil, err
		}
		lhsSampleExpr, ok := lhsMapped.(SampleExpr)
		if !ok {
			return nil, badASTMapping("SampleExpr", lhsMapped)
		}
		rhsSampleExpr, ok := rhsMapped.(SampleExpr)
		if !ok {
			return nil, badASTMapping("SampleExpr", rhsMapped)
		}
		e.SampleExpr = lhsSampleExpr
		e.RHS = rhsSampleExpr
		return e, nil
	default:
		return nil, errors.Errorf("unexpected expr type (%T) for ASTMapper type (%T) ", expr, m)
	}
}

func (m ShardMapper) mapLogSelectorExpr(expr LogSelectorExpr, r *shardRecorder) LogSelectorExpr {
	var head *ConcatLogSelectorExpr
	for i := m.shards - 1; i >= 0; i-- {
		head = &ConcatLogSelectorExpr{
			DownstreamLogSelectorExpr: DownstreamLogSelectorExpr{
				shard: &astmapper.ShardAnnotation{
					Shard: i,
					Of:    m.shards,
				},
				LogSelectorExpr: expr,
			},
			next: head,
		}
	}
	r.Add(m.shards, StreamsKey)

	return head
}

func (m ShardMapper) mapSampleExpr(expr SampleExpr, r *shardRecorder) SampleExpr {
	var head *ConcatSampleExpr
	for i := m.shards - 1; i >= 0; i-- {
		head = &ConcatSampleExpr{
			DownstreamSampleExpr: DownstreamSampleExpr{
				shard: &astmapper.ShardAnnotation{
					Shard: i,
					Of:    m.shards,
				},
				SampleExpr: expr,
			},
			next: head,
		}
	}
	r.Add(m.shards, MetricsKey)

	return head
}

// technically, std{dev,var} are also parallelizable if there is no cross-shard merging
// in descendent nodes in the AST. This optimization is currently avoided for simplicity.
func (m ShardMapper) mapVectorAggregationExpr(expr *vectorAggregationExpr, r *shardRecorder) (SampleExpr, error) {

	// if this AST contains unshardable operations, don't shard this at this level,
	// but attempt to shard a child node.
	if !expr.Shardable() {
		subMapped, err := m.Map(expr.left, r)
		if err != nil {
			return nil, err
		}
		sampleExpr, ok := subMapped.(SampleExpr)
		if !ok {
			return nil, badASTMapping("SampleExpr", subMapped)
		}

		return &vectorAggregationExpr{
			left:      sampleExpr,
			grouping:  expr.grouping,
			params:    expr.params,
			operation: expr.operation,
		}, nil

	}

	switch expr.operation {
	case OpTypeSum:
		// sum(x) -> sum(sum(x, shard=1) ++ sum(x, shard=2)...)
		return &vectorAggregationExpr{
			left:      m.mapSampleExpr(expr, r),
			grouping:  expr.grouping,
			params:    expr.params,
			operation: expr.operation,
		}, nil

	case OpTypeAvg:
		// avg(x) -> sum(x)/count(x)
		lhs, err := m.mapVectorAggregationExpr(&vectorAggregationExpr{
			left:      expr.left,
			grouping:  expr.grouping,
			operation: OpTypeSum,
		}, r)
		if err != nil {
			return nil, err
		}
		rhs, err := m.mapVectorAggregationExpr(&vectorAggregationExpr{
			left:      expr.left,
			grouping:  expr.grouping,
			operation: OpTypeCount,
		}, r)
		if err != nil {
			return nil, err
		}

		return &binOpExpr{
			SampleExpr: lhs,
			RHS:        rhs,
			op:         OpTypeDiv,
		}, nil

	case OpTypeCount:
		// count(x) -> sum(count(x, shard=1) ++ count(x, shard=2)...)
		sharded := m.mapSampleExpr(expr, r)
		return &vectorAggregationExpr{
			left:      sharded,
			grouping:  expr.grouping,
			operation: OpTypeSum,
		}, nil
	default:
		// this should not be reachable. If an operation is shardable it should
		// have an optimization listed.
		level.Warn(util.Logger).Log(
			"msg", "unexpected operation which appears shardable, ignoring",
			"operation", expr.operation,
		)
		return expr, nil
	}
}

func (m ShardMapper) mapLabelReplaceExpr(expr *labelReplaceExpr, r *shardRecorder) (SampleExpr, error) {
	subMapped, err := m.Map(expr.left, r)
	if err != nil {
		return nil, err
	}
	cpy := *expr
	cpy.left = subMapped.(SampleExpr)
	return &cpy, nil
}

func (m ShardMapper) mapRangeAggregationExpr(expr *rangeAggregationExpr, r *shardRecorder) SampleExpr {
	if hasLabelModifier(expr) {
		// if an expr can modify labels this means multiple shards can returns the same labelset.
		// When this happens the merge strategy needs to be different than a simple concatenation.
		// For instance for rates we need to sum data from different shards but same series.
		// Since we currently support only concatenation as merge strategy, we skip those queries.
		return expr
	}
	switch expr.operation {
	case OpRangeTypeCount, OpRangeTypeRate, OpRangeTypeBytesRate, OpRangeTypeBytes:
		// count_over_time(x) -> count_over_time(x, shard=1) ++ count_over_time(x, shard=2)...
		// rate(x) -> rate(x, shard=1) ++ rate(x, shard=2)...
		// same goes for bytes_rate and bytes_over_time
		return m.mapSampleExpr(expr, r)
	default:
		return expr
	}
}

// hasLabelModifier tells if an expression contains pipelines that can modify stream labels
// parsers introduce new labels but does not alter original one for instance.
func hasLabelModifier(expr *rangeAggregationExpr) bool {
	switch ex := expr.left.left.(type) {
	case *matchersExpr:
		return false
	case *pipelineExpr:
		for _, p := range ex.pipeline {
			if _, ok := p.(*labelFmtExpr); ok {
				return true
			}
		}
	}
	return false
}

// shardableOps lists the operations which may be sharded.
// topk, botk, max, & min all must be concatenated and then evaluated in order to avoid
// potential data loss due to series distribution across shards.
// For example, grouping by `cluster` for a `max` operation may yield
// 2 results on the first shard and 10 results on the second. If we prematurely
// calculated `max`s on each shard, the shard/label combination with `2` may be
// discarded and some other combination with `11` may be reported falsely as the max.
//
// Explanation: this is my (owen-d) best understanding.
//
// For an operation to be shardable, first the sample-operation itself must be associative like (+, *) but not (%, /, ^).
// Secondly, if the operation is part of a vector aggregation expression or utilizes logical/set binary ops,
// the vector operation must be distributive over the sample-operation.
// This ensures that the vector merging operation can be applied repeatedly to data in different shards.
// references:
// https://en.wikipedia.org/wiki/Associative_property
// https://en.wikipedia.org/wiki/Distributive_property
var shardableOps = map[string]bool{
	// vector ops
	OpTypeSum: true,
	// avg is only marked as shardable because we remap it into sum/count.
	OpTypeAvg:   true,
	OpTypeCount: true,

	// range vector ops
	OpRangeTypeCount:     true,
	OpRangeTypeRate:      true,
	OpRangeTypeBytes:     true,
	OpRangeTypeBytesRate: true,
	OpRangeTypeSum:       true,
	OpRangeTypeMax:       true,
	OpRangeTypeMin:       true,

	// binops - arith
	OpTypeAdd: true,
	OpTypeMul: true,
}
