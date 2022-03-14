package logql

import (
	"fmt"

	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/querier/astmapper"
	util_log "github.com/grafana/loki/pkg/util/log"
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
	Shards      *prometheus.CounterVec // sharded queries total, partitioned by (streams/metric)
	ShardFactor prometheus.Histogram   // per request shard factor
	parsed      *prometheus.CounterVec // parsed ASTs total, partitioned by (success/failure/noop)
}

func NewShardingMetrics(registerer prometheus.Registerer) *ShardingMetrics {
	return &ShardingMetrics{
		Shards: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki",
			Name:      "query_frontend_shards_total",
		}, []string{"type"}),
		parsed: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki",
			Name:      "query_frontend_sharding_parsed_queries_total",
		}, []string{"type"}),
		ShardFactor: promauto.With(registerer).NewHistogram(prometheus.HistogramOpts{
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
	r.Shards.WithLabelValues(key).Add(float64(x))
}

// Finish idemptotently records a histogram entry with the total shard factor.
func (r *shardRecorder) Finish() {
	if !r.done {
		r.done = true
		r.ShardFactor.Observe(float64(r.total))
	}
}

func badASTMapping(got syntax.Expr) error {
	return fmt.Errorf("bad AST mapping: expected SampleExpr, but got (%T)", got)
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

func (m ShardMapper) Parse(query string) (noop bool, expr syntax.Expr, err error) {
	parsed, err := syntax.ParseExpr(query)
	if err != nil {
		return false, nil, err
	}

	recorder := m.metrics.shardRecorder()

	mapped, err := m.Map(parsed, recorder)
	if err != nil {
		m.metrics.parsed.WithLabelValues(FailureKey).Inc()
		return false, nil, err
	}

	originalStr := parsed.String()
	mappedStr := mapped.String()
	noop = originalStr == mappedStr
	if noop {
		m.metrics.parsed.WithLabelValues(NoopKey).Inc()
	} else {
		m.metrics.parsed.WithLabelValues(SuccessKey).Inc()
	}

	recorder.Finish() // only record metrics for successful mappings

	return noop, mapped, err
}

func (m ShardMapper) Map(expr syntax.Expr, r *shardRecorder) (syntax.Expr, error) {
	// immediately clone the passed expr to avoid mutating the original
	expr, err := syntax.Clone(expr)
	if err != nil {
		return nil, err
	}

	switch e := expr.(type) {
	case *syntax.LiteralExpr:
		return e, nil
	case *syntax.MatchersExpr, *syntax.PipelineExpr:
		return m.mapLogSelectorExpr(e.(syntax.LogSelectorExpr), r), nil
	case *syntax.VectorAggregationExpr:
		return m.mapVectorAggregationExpr(e, r)
	case *syntax.LabelReplaceExpr:
		return m.mapLabelReplaceExpr(e, r)
	case *syntax.RangeAggregationExpr:
		return m.mapRangeAggregationExpr(e, r), nil
	case *syntax.BinOpExpr:
		lhsMapped, err := m.Map(e.SampleExpr, r)
		if err != nil {
			return nil, err
		}
		rhsMapped, err := m.Map(e.RHS, r)
		if err != nil {
			return nil, err
		}
		lhsSampleExpr, ok := lhsMapped.(syntax.SampleExpr)
		if !ok {
			return nil, badASTMapping(lhsMapped)
		}
		rhsSampleExpr, ok := rhsMapped.(syntax.SampleExpr)
		if !ok {
			return nil, badASTMapping(rhsMapped)
		}
		e.SampleExpr = lhsSampleExpr
		e.RHS = rhsSampleExpr
		return e, nil
	default:
		return nil, errors.Errorf("unexpected expr type (%T) for ASTMapper type (%T) ", expr, m)
	}
}

func (m ShardMapper) mapLogSelectorExpr(expr syntax.LogSelectorExpr, r *shardRecorder) syntax.LogSelectorExpr {
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

func (m ShardMapper) mapSampleExpr(expr syntax.SampleExpr, r *shardRecorder) syntax.SampleExpr {
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
func (m ShardMapper) mapVectorAggregationExpr(expr *syntax.VectorAggregationExpr, r *shardRecorder) (syntax.SampleExpr, error) {
	// if this AST contains unshardable operations, don't shard this at this level,
	// but attempt to shard a child node.
	if !expr.Shardable() {
		subMapped, err := m.Map(expr.Left, r)
		if err != nil {
			return nil, err
		}
		sampleExpr, ok := subMapped.(syntax.SampleExpr)
		if !ok {
			return nil, badASTMapping(subMapped)
		}

		return &syntax.VectorAggregationExpr{
			Left:      sampleExpr,
			Grouping:  expr.Grouping,
			Params:    expr.Params,
			Operation: expr.Operation,
		}, nil

	}

	switch expr.Operation {
	case syntax.OpTypeSum:
		// sum(x) -> sum(sum(x, shard=1) ++ sum(x, shard=2)...)
		return &syntax.VectorAggregationExpr{
			Left:      m.mapSampleExpr(expr, r),
			Grouping:  expr.Grouping,
			Params:    expr.Params,
			Operation: expr.Operation,
		}, nil

	case syntax.OpTypeAvg:
		// avg(x) -> sum(x)/count(x)
		lhs, err := m.mapVectorAggregationExpr(&syntax.VectorAggregationExpr{
			Left:      expr.Left,
			Grouping:  expr.Grouping,
			Operation: syntax.OpTypeSum,
		}, r)
		if err != nil {
			return nil, err
		}
		rhs, err := m.mapVectorAggregationExpr(&syntax.VectorAggregationExpr{
			Left:      expr.Left,
			Grouping:  expr.Grouping,
			Operation: syntax.OpTypeCount,
		}, r)
		if err != nil {
			return nil, err
		}

		return &syntax.BinOpExpr{
			SampleExpr: lhs,
			RHS:        rhs,
			Op:         syntax.OpTypeDiv,
		}, nil

	case syntax.OpTypeCount:
		// count(x) -> sum(count(x, shard=1) ++ count(x, shard=2)...)
		sharded := m.mapSampleExpr(expr, r)
		return &syntax.VectorAggregationExpr{
			Left:      sharded,
			Grouping:  expr.Grouping,
			Operation: syntax.OpTypeSum,
		}, nil
	default:
		// this should not be reachable. If an operation is shardable it should
		// have an optimization listed.
		level.Warn(util_log.Logger).Log(
			"msg", "unexpected operation which appears shardable, ignoring",
			"operation", expr.Operation,
		)
		return expr, nil
	}
}

func (m ShardMapper) mapLabelReplaceExpr(expr *syntax.LabelReplaceExpr, r *shardRecorder) (syntax.SampleExpr, error) {
	subMapped, err := m.Map(expr.Left, r)
	if err != nil {
		return nil, err
	}
	cpy := *expr
	cpy.Left = subMapped.(syntax.SampleExpr)
	return &cpy, nil
}

func (m ShardMapper) mapRangeAggregationExpr(expr *syntax.RangeAggregationExpr, r *shardRecorder) syntax.SampleExpr {
	if hasLabelModifier(expr) {
		// if an expr can modify labels this means multiple shards can returns the same labelset.
		// When this happens the merge strategy needs to be different than a simple concatenation.
		// For instance for rates we need to sum data from different shards but same series.
		// Since we currently support only concatenation as merge strategy, we skip those queries.
		return expr
	}
	switch expr.Operation {
	case syntax.OpRangeTypeCount, syntax.OpRangeTypeRate, syntax.OpRangeTypeBytesRate, syntax.OpRangeTypeBytes:
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
