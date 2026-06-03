package engine

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/logical"
)

var (
	status               = "status"
	statusSuccess        = "success"
	statusFailure        = "failure"
	statusNotImplemented = "notimplemented"

	queryTypeLabel  = "query_type"
	hasRegexLabel   = "has_regex"
	passNameLabel   = "pass_name"
	ruleNameLabel   = "rule_name"
	applicableLabel = "applicable"
	succeededLabel  = "succeeded"
	errorClassLabel = "error_class"
	stageLabel      = "stage"
	reasonLabel     = "reason"

	stageLogicalPlanning  = "logical_planning"
	stagePhysicalPlanning = "physical_planning"
	stagePrepare          = "prepare"
	stageExecution        = "execution"
)

// metrics groups the v2 engine's query-grain metrics by query lifecycle phase.
// The core struct holds one sub-struct per phase; each sub-struct owns the
// metrics observed during that phase.
//
// NOTE: Metrics are subject to rapid change!
type metrics struct {
	query     queryMetrics
	planning  planningMetrics
	execution executionMetrics
}

// queryMetrics covers whole-query outcome and duration.

type queryMetrics struct {
	subqueries    *prometheus.CounterVec   // {status, query_type}
	stageFailures *prometheus.CounterVec   // {stage, reason, query_type}
	other         *prometheus.HistogramVec // {query_type}
}

// planningMetrics covers logical, physical, and prepare planning.

type planningMetrics struct {
	logical  *prometheus.HistogramVec // {query_type}
	physical *prometheus.HistogramVec // {query_type}
	prepare  *prometheus.HistogramVec // {query_type}

	// LogQL query-shape stats, observed once per query.

	logqlLengthChars    *prometheus.HistogramVec // {query_type}
	logqlLabelMatchers  *prometheus.HistogramVec // {query_type}
	logqlPipelineStages *prometheus.HistogramVec // {query_type}
	queriesWithRegex    *prometheus.CounterVec   // {query_type, has_regex}

	// Physical plan-shape stats, observed once per query after physical planning.

	physicalPlanDepth                 prometheus.Histogram
	physicalPlanNodeCount             prometheus.Histogram
	physicalPlanMaxFanout             prometheus.Histogram
	physicalPlanPartitionableSubtrees prometheus.Histogram

	// Optimizer pass/rule firing counters.

	logicalPassTotal  *prometheus.CounterVec // {pass_name, applicable, succeeded}
	logicalPassErrors *prometheus.CounterVec // {pass_name, error_class}
	physicalRuleTotal *prometheus.CounterVec // {rule_name, applicable}
}

// executionMetrics covers query execution and result collection.

type executionMetrics struct {
	duration       *prometheus.HistogramVec // {query_type}
	tasksPerQuery  *prometheus.HistogramVec // {query_type}
	tasksGenerated *prometheus.HistogramVec // {query_type}
}

func newMetrics(r prometheus.Registerer) *metrics {
	return &metrics{
		query: queryMetrics{
			subqueries: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
				Name: "loki_engine_v2_subqueries_total",
				Help: "Total number of subqueries executed with the new engine",
			}, []string{status, queryTypeLabel}),
			stageFailures: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
				Name: "loki_engine_v2_stage_failures_total",
				Help: "Total number of query failures by engine stage and reason.",
			}, []string{stageLabel, reasonLabel, queryTypeLabel}),
			other: newNativeHistogramVec(r, prometheus.HistogramOpts{
				Name: "loki_engine_v2_other_duration_seconds",
				Help: "Per-query residual duration in seconds: wall-clock time not attributed to any tracked phase (mirrors the duration_other_ms summary field)",
			}, []string{queryTypeLabel}),
		},

		planning: planningMetrics{
			logical: newNativeHistogramVec(r, prometheus.HistogramOpts{
				Name: "loki_engine_v2_logical_planning_duration_seconds",
				Help: "Duration of logical query planning in seconds",
			}, []string{queryTypeLabel}),
			physical: newNativeHistogramVec(r, prometheus.HistogramOpts{
				Name: "loki_engine_v2_physical_planning_duration_seconds",
				Help: "Duration of physical query planning in seconds",
				Buckets: append(
					prometheus.DefBuckets,                    // 0.005s -> 10s
					prometheus.LinearBuckets(15, 5.0, 10)..., // 15s -> 60s
				),
			}, []string{queryTypeLabel}),
			prepare: newNativeHistogramVec(r, prometheus.HistogramOpts{
				Name: "loki_engine_v2_prepare_duration_seconds",
				Help: "Duration of query preparation in seconds",
				Buckets: append(
					prometheus.DefBuckets,                    // 0.005s -> 10s
					prometheus.LinearBuckets(15, 5.0, 10)..., // 15s -> 60s
				),
			}, []string{queryTypeLabel}),

			logqlLengthChars: newNativeHistogramVec(r, prometheus.HistogramOpts{
				Name: "loki_engine_v2_logql_length_chars",
				Help: "Length of the incoming LogQL query string in characters",
			}, []string{queryTypeLabel}),
			logqlLabelMatchers: newNativeHistogramVec(r, prometheus.HistogramOpts{
				Name: "loki_engine_v2_logql_label_matchers",
				Help: "Number of stream-selector label matchers in the incoming LogQL query",
			}, []string{queryTypeLabel}),
			logqlPipelineStages: newNativeHistogramVec(r, prometheus.HistogramOpts{
				Name: "loki_engine_v2_logql_pipeline_stages",
				Help: "Number of pipeline stages in the incoming LogQL query",
			}, []string{queryTypeLabel}),
			queriesWithRegex: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
				Name: "loki_engine_v2_queries_with_regex_total",
				Help: "Total number of queries split by whether they use a regex matcher or filter",
			}, []string{queryTypeLabel, hasRegexLabel}),

			physicalPlanDepth: newNativeHistogram(r, prometheus.HistogramOpts{
				Name: "loki_engine_v2_physical_plan_depth",
				Help: "Depth (longest root-to-leaf path, in nodes) of the physical plan",
			}),
			physicalPlanNodeCount: newNativeHistogram(r, prometheus.HistogramOpts{
				Name: "loki_engine_v2_physical_plan_node_count",
				Help: "Number of nodes in the physical plan",
			}),
			physicalPlanMaxFanout: newNativeHistogram(r, prometheus.HistogramOpts{
				Name: "loki_engine_v2_physical_plan_max_fanout",
				Help: "Maximum number of children of any node in the physical plan",
			}),
			physicalPlanPartitionableSubtrees: newNativeHistogram(r, prometheus.HistogramOpts{
				Name: "loki_engine_v2_physical_plan_partitionable_subtrees",
				Help: "Number of partitionable (Parallelize) subtrees in the physical plan",
			}),

			logicalPassTotal: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
				Name: "loki_engine_v2_logical_pass_total",
				Help: "Total number of logical optimizer pass runs, split by whether the pass applied and succeeded",
			}, []string{passNameLabel, applicableLabel, succeededLabel}),
			logicalPassErrors: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
				Name: "loki_engine_v2_logical_pass_errors_total",
				Help: "Total number of logical optimizer pass failures, split by error class",
			}, []string{passNameLabel, errorClassLabel}),
			physicalRuleTotal: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
				Name: "loki_engine_v2_physical_rule_total",
				Help: "Total number of physical optimizer rule runs, split by whether the rule applied",
			}, []string{ruleNameLabel, applicableLabel}),
		},

		execution: executionMetrics{
			duration: newNativeHistogramVec(r, prometheus.HistogramOpts{
				Name: "loki_engine_v2_execution_duration_seconds",
				Help: "Duration of query execution in seconds",
				Buckets: append(
					prometheus.DefBuckets,                    // 0.005s -> 10s
					prometheus.LinearBuckets(15, 5.0, 10)..., // 15s -> 60s
				),
			}, []string{queryTypeLabel}),
			tasksPerQuery: newNativeHistogramVec(r, prometheus.HistogramOpts{
				Name: "loki_engine_v2_tasks_per_query",
				Help: "Number of tasks per query after pruning",
			}, []string{queryTypeLabel}),
			tasksGenerated: newNativeHistogramVec(r, prometheus.HistogramOpts{
				Name: "loki_engine_v2_tasks_generated_per_query",
				Help: "Number of tasks generated per query before pruning",
			}, []string{queryTypeLabel}),
		},
	}
}

// observeLogQLShape records the LogQL query-shape metrics for the given query
// type.
func (m *metrics) observeLogQLShape(queryType string, shape logqlShape) {
	m.planning.logqlLengthChars.WithLabelValues(queryType).Observe(float64(shape.lengthChars))
	m.planning.logqlLabelMatchers.WithLabelValues(queryType).Observe(float64(shape.labelMatchers))
	m.planning.logqlPipelineStages.WithLabelValues(queryType).Observe(float64(shape.pipelineStages))
	m.planning.queriesWithRegex.WithLabelValues(queryType, strconv.FormatBool(shape.hasRegex)).Inc()
}

// observePhysicalShape records the physical plan-shape metrics.
func (m *metrics) observePhysicalShape(shape physicalShape) {
	m.planning.physicalPlanDepth.Observe(float64(shape.depth))
	m.planning.physicalPlanNodeCount.Observe(float64(shape.nodeCount))
	m.planning.physicalPlanMaxFanout.Observe(float64(shape.maxFanout))
	m.planning.physicalPlanPartitionableSubtrees.Observe(float64(shape.partitionableSubtrees))
}

// recordLogicalPasses records optimizer firing counters from the logical pass
// firings produced during logical optimization.
func (m *metrics) recordLogicalPasses(firings []logical.PassFiring) {
	for _, f := range firings {
		m.planning.logicalPassTotal.WithLabelValues(
			f.Name,
			strconv.FormatBool(f.Applicable),
			strconv.FormatBool(f.Succeeded),
		).Inc()
		if !f.Succeeded {
			m.planning.logicalPassErrors.WithLabelValues(f.Name, errorClass(f.Err)).Inc()
		}
	}
}

// recordPhysicalRules records optimizer firing counters from the physical rule
// firings produced during physical optimization. Physical rules have no error
// path, so there is no success/error dimension to record.
func (m *metrics) recordPhysicalRules(rules map[string]bool) {
	for name, applied := range rules {
		m.planning.physicalRuleTotal.WithLabelValues(
			name,
			strconv.FormatBool(applied),
		).Inc()
	}
}

func newNativeHistogramVec(r prometheus.Registerer, opts prometheus.HistogramOpts, labels []string) *prometheus.HistogramVec {
	applyNativeHistogramOpts(&opts)
	return promauto.With(r).NewHistogramVec(opts, labels)
}

func newNativeHistogram(r prometheus.Registerer, opts prometheus.HistogramOpts) prometheus.Histogram {
	applyNativeHistogramOpts(&opts)
	return promauto.With(r).NewHistogram(opts)
}

func applyNativeHistogramOpts(opts *prometheus.HistogramOpts) {
	opts.NativeHistogramBucketFactor = 1.1
	opts.NativeHistogramMaxBucketNumber = 100
	opts.NativeHistogramMinResetDuration = time.Hour
}
