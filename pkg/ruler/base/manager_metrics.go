package base

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/util"
)

// ManagerMetrics aggregates metrics exported by the Prometheus
// rules package and returns them as Cortex metrics
type ManagerMetrics struct {
	regs                   *util.UserRegistries
	disableRuleGroupLabel  bool
	metricLabelTransformer util.MetricLabelTransformFunc

	EvalDuration         *prometheus.Desc
	IterationDuration    *prometheus.Desc
	IterationsMissed     *prometheus.Desc
	IterationsScheduled  *prometheus.Desc
	EvalTotal            *prometheus.Desc
	EvalFailures         *prometheus.Desc
	GroupInterval        *prometheus.Desc
	GroupLastEvalTime    *prometheus.Desc
	GroupLastDuration    *prometheus.Desc
	GroupRules           *prometheus.Desc
	GroupLastEvalSamples *prometheus.Desc
}

// RuleGroupLabel is the label added by Prometheus, the value of which comes from the GroupKey function
const RuleGroupLabel = "rule_group"

// NewManagerMetrics returns a ManagerMetrics struct
func NewManagerMetrics(disableRuleGroupLabel bool, tf util.MetricLabelTransformFunc, metricsNamespace string) *ManagerMetrics {
	commonLabels := []string{"user"}
	if !disableRuleGroupLabel {
		commonLabels = append(commonLabels, RuleGroupLabel)
	}
	return &ManagerMetrics{
		regs:                   util.NewUserRegistries(),
		disableRuleGroupLabel:  disableRuleGroupLabel,
		metricLabelTransformer: tf,

		EvalDuration: prometheus.NewDesc(
			metricsNamespace+"_prometheus_rule_evaluation_duration_seconds",
			"The duration for a rule to execute.",
			[]string{"user"},
			nil,
		),
		IterationDuration: prometheus.NewDesc(
			metricsNamespace+"_prometheus_rule_group_duration_seconds",
			"The duration of rule group evaluations.",
			[]string{"user"},
			nil,
		),
		IterationsMissed: prometheus.NewDesc(
			metricsNamespace+"_prometheus_rule_group_iterations_missed_total",
			"The total number of rule group evaluations missed due to slow rule group evaluation.",
			commonLabels,
			nil,
		),
		IterationsScheduled: prometheus.NewDesc(
			metricsNamespace+"_prometheus_rule_group_iterations_total",
			"The total number of scheduled rule group evaluations, whether executed or missed.",
			commonLabels,
			nil,
		),
		EvalTotal: prometheus.NewDesc(
			metricsNamespace+"_prometheus_rule_evaluations_total",
			"The total number of rule evaluations.",
			commonLabels,
			nil,
		),
		EvalFailures: prometheus.NewDesc(
			metricsNamespace+"_prometheus_rule_evaluation_failures_total",
			"The total number of rule evaluation failures.",
			commonLabels,
			nil,
		),
		GroupInterval: prometheus.NewDesc(
			metricsNamespace+"_prometheus_rule_group_interval_seconds",
			"The interval of a rule group.",
			commonLabels,
			nil,
		),
		GroupLastEvalTime: prometheus.NewDesc(
			metricsNamespace+"_prometheus_rule_group_last_evaluation_timestamp_seconds",
			"The timestamp of the last rule group evaluation in seconds.",
			commonLabels,
			nil,
		),
		GroupLastDuration: prometheus.NewDesc(
			metricsNamespace+"_prometheus_rule_group_last_duration_seconds",
			"The duration of the last rule group evaluation.",
			commonLabels,
			nil,
		),
		GroupRules: prometheus.NewDesc(
			metricsNamespace+"_prometheus_rule_group_rules",
			"The number of rules.",
			commonLabels,
			nil,
		),
		GroupLastEvalSamples: prometheus.NewDesc(
			metricsNamespace+"_prometheus_last_evaluation_samples",
			"The number of samples returned during the last rule group evaluation.",
			commonLabels,
			nil,
		),
	}
}

// AddUserRegistry adds a user-specific Prometheus registry.
func (m *ManagerMetrics) AddUserRegistry(user string, reg *prometheus.Registry) {
	m.regs.AddUserRegistry(user, reg)
}

// RemoveUserRegistry removes user-specific Prometheus registry.
func (m *ManagerMetrics) RemoveUserRegistry(user string) {
	m.regs.RemoveUserRegistry(user, true)
}

// Describe implements the Collector interface
func (m *ManagerMetrics) Describe(out chan<- *prometheus.Desc) {
	out <- m.EvalDuration
	out <- m.IterationDuration
	out <- m.IterationsMissed
	out <- m.IterationsScheduled
	out <- m.EvalTotal
	out <- m.EvalFailures
	out <- m.GroupInterval
	out <- m.GroupLastEvalTime
	out <- m.GroupLastDuration
	out <- m.GroupRules
	out <- m.GroupLastEvalSamples
}

// Collect implements the Collector interface
func (m *ManagerMetrics) Collect(out chan<- prometheus.Metric) {
	data := m.regs.BuildMetricFamiliesPerUser(m.metricLabelTransformer)
	labels := []string{}
	if !m.disableRuleGroupLabel {
		labels = append(labels, RuleGroupLabel)
	}
	// WARNING: It is important that all metrics generated in this method are "Per User".
	// Thanks to that we can actually *remove* metrics for given user (see RemoveUserRegistry).
	// If same user is later re-added, all metrics will start from 0, which is fine.

	data.SendSumOfSummariesPerUser(out, m.EvalDuration, "prometheus_rule_evaluation_duration_seconds")
	data.SendSumOfSummariesPerUser(out, m.IterationDuration, "prometheus_rule_group_duration_seconds")

	data.SendSumOfCountersPerUserWithLabels(out, m.IterationsMissed, "prometheus_rule_group_iterations_missed_total", labels...)
	data.SendSumOfCountersPerUserWithLabels(out, m.IterationsScheduled, "prometheus_rule_group_iterations_total", labels...)
	data.SendSumOfCountersPerUserWithLabels(out, m.EvalTotal, "prometheus_rule_evaluations_total", labels...)
	data.SendSumOfCountersPerUserWithLabels(out, m.EvalFailures, "prometheus_rule_evaluation_failures_total", labels...)
	data.SendSumOfGaugesPerUserWithLabels(out, m.GroupInterval, "prometheus_rule_group_interval_seconds", labels...)
	data.SendSumOfGaugesPerUserWithLabels(out, m.GroupLastEvalTime, "prometheus_rule_group_last_evaluation_timestamp_seconds", labels...)
	data.SendSumOfGaugesPerUserWithLabels(out, m.GroupLastDuration, "prometheus_rule_group_last_duration_seconds", labels...)
	data.SendSumOfGaugesPerUserWithLabels(out, m.GroupRules, "prometheus_rule_group_rules", labels...)
	data.SendSumOfGaugesPerUserWithLabels(out, m.GroupLastEvalSamples, "prometheus_rule_group_last_evaluation_samples", labels...)
}
