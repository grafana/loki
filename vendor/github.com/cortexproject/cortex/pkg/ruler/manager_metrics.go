package ruler

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/cortexproject/cortex/pkg/util"
)

// ManagerMetrics aggregates metrics exported by the Prometheus
// rules package and returns them as Cortex metrics
type ManagerMetrics struct {
	regs *util.UserRegistries

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

// NewManagerMetrics returns a ManagerMetrics struct
func NewManagerMetrics() *ManagerMetrics {
	return &ManagerMetrics{
		regs: util.NewUserRegistries(),

		EvalDuration: prometheus.NewDesc(
			"cortex_prometheus_rule_evaluation_duration_seconds",
			"The duration for a rule to execute.",
			[]string{"user"},
			nil,
		),
		IterationDuration: prometheus.NewDesc(
			"cortex_prometheus_rule_group_duration_seconds",
			"The duration of rule group evaluations.",
			[]string{"user"},
			nil,
		),
		IterationsMissed: prometheus.NewDesc(
			"cortex_prometheus_rule_group_iterations_missed_total",
			"The total number of rule group evaluations missed due to slow rule group evaluation.",
			[]string{"user", "rule_group"},
			nil,
		),
		IterationsScheduled: prometheus.NewDesc(
			"cortex_prometheus_rule_group_iterations_total",
			"The total number of scheduled rule group evaluations, whether executed or missed.",
			[]string{"user", "rule_group"},
			nil,
		),
		EvalTotal: prometheus.NewDesc(
			"cortex_prometheus_rule_evaluations_total",
			"The total number of rule evaluations.",
			[]string{"user", "rule_group"},
			nil,
		),
		EvalFailures: prometheus.NewDesc(
			"cortex_prometheus_rule_evaluation_failures_total",
			"The total number of rule evaluation failures.",
			[]string{"user", "rule_group"},
			nil,
		),
		GroupInterval: prometheus.NewDesc(
			"cortex_prometheus_rule_group_interval_seconds",
			"The interval of a rule group.",
			[]string{"user", "rule_group"},
			nil,
		),
		GroupLastEvalTime: prometheus.NewDesc(
			"cortex_prometheus_rule_group_last_evaluation_timestamp_seconds",
			"The timestamp of the last rule group evaluation in seconds.",
			[]string{"user", "rule_group"},
			nil,
		),
		GroupLastDuration: prometheus.NewDesc(
			"cortex_prometheus_rule_group_last_duration_seconds",
			"The duration of the last rule group evaluation.",
			[]string{"user", "rule_group"},
			nil,
		),
		GroupRules: prometheus.NewDesc(
			"cortex_prometheus_rule_group_rules",
			"The number of rules.",
			[]string{"user", "rule_group"},
			nil,
		),
		GroupLastEvalSamples: prometheus.NewDesc(
			"cortex_prometheus_last_evaluation_samples",
			"The number of samples returned during the last rule group evaluation.",
			[]string{"user", "rule_group"},
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
	data := m.regs.BuildMetricFamiliesPerUser()

	// WARNING: It is important that all metrics generated in this method are "Per User".
	// Thanks to that we can actually *remove* metrics for given user (see RemoveUserRegistry).
	// If same user is later re-added, all metrics will start from 0, which is fine.

	data.SendSumOfSummariesPerUser(out, m.EvalDuration, "prometheus_rule_evaluation_duration_seconds")
	data.SendSumOfSummariesPerUser(out, m.IterationDuration, "prometheus_rule_group_duration_seconds")

	data.SendSumOfCountersPerUserWithLabels(out, m.IterationsMissed, "prometheus_rule_group_iterations_missed_total", "rule_group")
	data.SendSumOfCountersPerUserWithLabels(out, m.IterationsScheduled, "prometheus_rule_group_iterations_total", "rule_group")
	data.SendSumOfCountersPerUserWithLabels(out, m.EvalTotal, "prometheus_rule_evaluations_total", "rule_group")
	data.SendSumOfCountersPerUserWithLabels(out, m.EvalFailures, "prometheus_rule_evaluation_failures_total", "rule_group")
	data.SendSumOfGaugesPerUserWithLabels(out, m.GroupInterval, "prometheus_rule_group_interval_seconds", "rule_group")
	data.SendSumOfGaugesPerUserWithLabels(out, m.GroupLastEvalTime, "prometheus_rule_group_last_evaluation_timestamp_seconds", "rule_group")
	data.SendSumOfGaugesPerUserWithLabels(out, m.GroupLastDuration, "prometheus_rule_group_last_duration_seconds", "rule_group")
	data.SendSumOfGaugesPerUserWithLabels(out, m.GroupRules, "prometheus_rule_group_rules", "rule_group")
	data.SendSumOfGaugesPerUserWithLabels(out, m.GroupLastEvalSamples, "prometheus_rule_group_last_evaluation_samples", "rule_group")
}
