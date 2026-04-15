package base

import (
	"bytes"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/ruler/rulespb"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

func TestManagerMetricsWithRuleGroupLabel(t *testing.T) {
	mainReg := prometheus.NewPedanticRegistry()

	managerMetrics := NewManagerMetrics(false, nil, constants.Cortex)
	mainReg.MustRegister(managerMetrics)
	managerMetrics.AddUserRegistry("user1", populateManager(1))
	managerMetrics.AddUserRegistry("user2", populateManager(10))
	managerMetrics.AddUserRegistry("user3", populateManager(100))

	managerMetrics.AddUserRegistry("user4", populateManager(1000))
	managerMetrics.RemoveUserRegistry("user4")

	//noinspection ALL
	err := testutil.GatherAndCompare(mainReg, bytes.NewBufferString(`
# HELP cortex_prometheus_last_evaluation_samples The number of samples returned during the last rule group evaluation.
# TYPE cortex_prometheus_last_evaluation_samples gauge
cortex_prometheus_last_evaluation_samples{rule_group="group_one",user="user1"} 1000
cortex_prometheus_last_evaluation_samples{rule_group="group_one",user="user2"} 10000
cortex_prometheus_last_evaluation_samples{rule_group="group_one",user="user3"} 100000
cortex_prometheus_last_evaluation_samples{rule_group="group_two",user="user1"} 1000
cortex_prometheus_last_evaluation_samples{rule_group="group_two",user="user2"} 10000
cortex_prometheus_last_evaluation_samples{rule_group="group_two",user="user3"} 100000
# HELP cortex_prometheus_rule_evaluation_duration_seconds The duration for a rule to execute.
# TYPE cortex_prometheus_rule_evaluation_duration_seconds summary
cortex_prometheus_rule_evaluation_duration_seconds{user="user1",quantile="0.5"} 1
cortex_prometheus_rule_evaluation_duration_seconds{user="user1",quantile="0.9"} 1
cortex_prometheus_rule_evaluation_duration_seconds{user="user1",quantile="0.99"} 1
cortex_prometheus_rule_evaluation_duration_seconds_sum{user="user1"} 1
cortex_prometheus_rule_evaluation_duration_seconds_count{user="user1"} 1
cortex_prometheus_rule_evaluation_duration_seconds{user="user2",quantile="0.5"} 10
cortex_prometheus_rule_evaluation_duration_seconds{user="user2",quantile="0.9"} 10
cortex_prometheus_rule_evaluation_duration_seconds{user="user2",quantile="0.99"} 10
cortex_prometheus_rule_evaluation_duration_seconds_sum{user="user2"} 10
cortex_prometheus_rule_evaluation_duration_seconds_count{user="user2"} 1
cortex_prometheus_rule_evaluation_duration_seconds{user="user3",quantile="0.5"} 100
cortex_prometheus_rule_evaluation_duration_seconds{user="user3",quantile="0.9"} 100
cortex_prometheus_rule_evaluation_duration_seconds{user="user3",quantile="0.99"} 100
cortex_prometheus_rule_evaluation_duration_seconds_sum{user="user3"} 100
cortex_prometheus_rule_evaluation_duration_seconds_count{user="user3"} 1
# HELP cortex_prometheus_rule_evaluation_failures_total The total number of rule evaluation failures.
# TYPE cortex_prometheus_rule_evaluation_failures_total counter
cortex_prometheus_rule_evaluation_failures_total{rule_group="group_one",user="user1"} 1
cortex_prometheus_rule_evaluation_failures_total{rule_group="group_one",user="user2"} 10
cortex_prometheus_rule_evaluation_failures_total{rule_group="group_one",user="user3"} 100
cortex_prometheus_rule_evaluation_failures_total{rule_group="group_two",user="user1"} 1
cortex_prometheus_rule_evaluation_failures_total{rule_group="group_two",user="user2"} 10
cortex_prometheus_rule_evaluation_failures_total{rule_group="group_two",user="user3"} 100
# HELP cortex_prometheus_rule_evaluations_total The total number of rule evaluations.
# TYPE cortex_prometheus_rule_evaluations_total counter
cortex_prometheus_rule_evaluations_total{rule_group="group_one",user="user1"} 1
cortex_prometheus_rule_evaluations_total{rule_group="group_one",user="user2"} 10
cortex_prometheus_rule_evaluations_total{rule_group="group_one",user="user3"} 100
cortex_prometheus_rule_evaluations_total{rule_group="group_two",user="user1"} 1
cortex_prometheus_rule_evaluations_total{rule_group="group_two",user="user2"} 10
cortex_prometheus_rule_evaluations_total{rule_group="group_two",user="user3"} 100
# HELP cortex_prometheus_rule_group_duration_seconds The duration of rule group evaluations.
# TYPE cortex_prometheus_rule_group_duration_seconds summary
cortex_prometheus_rule_group_duration_seconds{user="user1",quantile="0.01"} 1
cortex_prometheus_rule_group_duration_seconds{user="user1",quantile="0.05"} 1
cortex_prometheus_rule_group_duration_seconds{user="user1",quantile="0.5"} 1
cortex_prometheus_rule_group_duration_seconds{user="user1",quantile="0.9"} 1
cortex_prometheus_rule_group_duration_seconds{user="user1",quantile="0.99"} 1
cortex_prometheus_rule_group_duration_seconds_sum{user="user1"} 1
cortex_prometheus_rule_group_duration_seconds_count{user="user1"} 1
cortex_prometheus_rule_group_duration_seconds{user="user2",quantile="0.01"} 10
cortex_prometheus_rule_group_duration_seconds{user="user2",quantile="0.05"} 10
cortex_prometheus_rule_group_duration_seconds{user="user2",quantile="0.5"} 10
cortex_prometheus_rule_group_duration_seconds{user="user2",quantile="0.9"} 10
cortex_prometheus_rule_group_duration_seconds{user="user2",quantile="0.99"} 10
cortex_prometheus_rule_group_duration_seconds_sum{user="user2"} 10
cortex_prometheus_rule_group_duration_seconds_count{user="user2"} 1
cortex_prometheus_rule_group_duration_seconds{user="user3",quantile="0.01"} 100
cortex_prometheus_rule_group_duration_seconds{user="user3",quantile="0.05"} 100
cortex_prometheus_rule_group_duration_seconds{user="user3",quantile="0.5"} 100
cortex_prometheus_rule_group_duration_seconds{user="user3",quantile="0.9"} 100
cortex_prometheus_rule_group_duration_seconds{user="user3",quantile="0.99"} 100
cortex_prometheus_rule_group_duration_seconds_sum{user="user3"} 100
cortex_prometheus_rule_group_duration_seconds_count{user="user3"} 1
# HELP cortex_prometheus_rule_group_iterations_missed_total The total number of rule group evaluations missed due to slow rule group evaluation.
# TYPE cortex_prometheus_rule_group_iterations_missed_total counter
cortex_prometheus_rule_group_iterations_missed_total{rule_group="group_one",user="user1"} 1
cortex_prometheus_rule_group_iterations_missed_total{rule_group="group_one",user="user2"} 10
cortex_prometheus_rule_group_iterations_missed_total{rule_group="group_one",user="user3"} 100
cortex_prometheus_rule_group_iterations_missed_total{rule_group="group_two",user="user1"} 1
cortex_prometheus_rule_group_iterations_missed_total{rule_group="group_two",user="user2"} 10
cortex_prometheus_rule_group_iterations_missed_total{rule_group="group_two",user="user3"} 100
# HELP cortex_prometheus_rule_group_iterations_total The total number of scheduled rule group evaluations, whether executed or missed.
# TYPE cortex_prometheus_rule_group_iterations_total counter
cortex_prometheus_rule_group_iterations_total{rule_group="group_one",user="user1"} 1
cortex_prometheus_rule_group_iterations_total{rule_group="group_one",user="user2"} 10
cortex_prometheus_rule_group_iterations_total{rule_group="group_one",user="user3"} 100
cortex_prometheus_rule_group_iterations_total{rule_group="group_two",user="user1"} 1
cortex_prometheus_rule_group_iterations_total{rule_group="group_two",user="user2"} 10
cortex_prometheus_rule_group_iterations_total{rule_group="group_two",user="user3"} 100
# HELP cortex_prometheus_rule_group_last_duration_seconds The duration of the last rule group evaluation.
# TYPE cortex_prometheus_rule_group_last_duration_seconds gauge
cortex_prometheus_rule_group_last_duration_seconds{rule_group="group_one",user="user1"} 1000
cortex_prometheus_rule_group_last_duration_seconds{rule_group="group_one",user="user2"} 10000
cortex_prometheus_rule_group_last_duration_seconds{rule_group="group_one",user="user3"} 100000
cortex_prometheus_rule_group_last_duration_seconds{rule_group="group_two",user="user1"} 1000
cortex_prometheus_rule_group_last_duration_seconds{rule_group="group_two",user="user2"} 10000
cortex_prometheus_rule_group_last_duration_seconds{rule_group="group_two",user="user3"} 100000
# HELP cortex_prometheus_rule_group_last_evaluation_timestamp_seconds The timestamp of the last rule group evaluation in seconds.
# TYPE cortex_prometheus_rule_group_last_evaluation_timestamp_seconds gauge
cortex_prometheus_rule_group_last_evaluation_timestamp_seconds{rule_group="group_one",user="user1"} 1000
cortex_prometheus_rule_group_last_evaluation_timestamp_seconds{rule_group="group_one",user="user2"} 10000
cortex_prometheus_rule_group_last_evaluation_timestamp_seconds{rule_group="group_one",user="user3"} 100000
cortex_prometheus_rule_group_last_evaluation_timestamp_seconds{rule_group="group_two",user="user1"} 1000
cortex_prometheus_rule_group_last_evaluation_timestamp_seconds{rule_group="group_two",user="user2"} 10000
cortex_prometheus_rule_group_last_evaluation_timestamp_seconds{rule_group="group_two",user="user3"} 100000
# HELP cortex_prometheus_rule_group_rules The number of rules.
# TYPE cortex_prometheus_rule_group_rules gauge
cortex_prometheus_rule_group_rules{rule_group="group_one",user="user1"} 1000
cortex_prometheus_rule_group_rules{rule_group="group_one",user="user2"} 10000
cortex_prometheus_rule_group_rules{rule_group="group_one",user="user3"} 100000
cortex_prometheus_rule_group_rules{rule_group="group_two",user="user1"} 1000
cortex_prometheus_rule_group_rules{rule_group="group_two",user="user2"} 10000
cortex_prometheus_rule_group_rules{rule_group="group_two",user="user3"} 100000
`))
	require.NoError(t, err)
}

func TestManagerMetricsWithoutRuleGroupLabel(t *testing.T) {
	mainReg := prometheus.NewPedanticRegistry()

	managerMetrics := NewManagerMetrics(true, nil, constants.Loki)
	mainReg.MustRegister(managerMetrics)
	managerMetrics.AddUserRegistry("user1", populateManager(1))
	managerMetrics.AddUserRegistry("user2", populateManager(10))
	managerMetrics.AddUserRegistry("user3", populateManager(100))

	managerMetrics.AddUserRegistry("user4", populateManager(1000))
	managerMetrics.RemoveUserRegistry("user4")

	//noinspection ALL
	err := testutil.GatherAndCompare(mainReg, bytes.NewBufferString(`
# HELP loki_prometheus_last_evaluation_samples The number of samples returned during the last rule group evaluation.
# TYPE loki_prometheus_last_evaluation_samples gauge
loki_prometheus_last_evaluation_samples{user="user1"} 2000
loki_prometheus_last_evaluation_samples{user="user2"} 20000
loki_prometheus_last_evaluation_samples{user="user3"} 200000
# HELP loki_prometheus_rule_evaluation_duration_seconds The duration for a rule to execute.
# TYPE loki_prometheus_rule_evaluation_duration_seconds summary
loki_prometheus_rule_evaluation_duration_seconds{user="user1",quantile="0.5"} 1
loki_prometheus_rule_evaluation_duration_seconds{user="user1",quantile="0.9"} 1
loki_prometheus_rule_evaluation_duration_seconds{user="user1",quantile="0.99"} 1
loki_prometheus_rule_evaluation_duration_seconds_sum{user="user1"} 1
loki_prometheus_rule_evaluation_duration_seconds_count{user="user1"} 1
loki_prometheus_rule_evaluation_duration_seconds{user="user2",quantile="0.5"} 10
loki_prometheus_rule_evaluation_duration_seconds{user="user2",quantile="0.9"} 10
loki_prometheus_rule_evaluation_duration_seconds{user="user2",quantile="0.99"} 10
loki_prometheus_rule_evaluation_duration_seconds_sum{user="user2"} 10
loki_prometheus_rule_evaluation_duration_seconds_count{user="user2"} 1
loki_prometheus_rule_evaluation_duration_seconds{user="user3",quantile="0.5"} 100
loki_prometheus_rule_evaluation_duration_seconds{user="user3",quantile="0.9"} 100
loki_prometheus_rule_evaluation_duration_seconds{user="user3",quantile="0.99"} 100
loki_prometheus_rule_evaluation_duration_seconds_sum{user="user3"} 100
loki_prometheus_rule_evaluation_duration_seconds_count{user="user3"} 1
# HELP loki_prometheus_rule_evaluation_failures_total The total number of rule evaluation failures.
# TYPE loki_prometheus_rule_evaluation_failures_total counter
loki_prometheus_rule_evaluation_failures_total{user="user1"} 2
loki_prometheus_rule_evaluation_failures_total{user="user2"} 20
loki_prometheus_rule_evaluation_failures_total{user="user3"} 200
# HELP loki_prometheus_rule_evaluations_total The total number of rule evaluations.
# TYPE loki_prometheus_rule_evaluations_total counter
loki_prometheus_rule_evaluations_total{user="user1"} 2
loki_prometheus_rule_evaluations_total{user="user2"} 20
loki_prometheus_rule_evaluations_total{user="user3"} 200
# HELP loki_prometheus_rule_group_duration_seconds The duration of rule group evaluations.
# TYPE loki_prometheus_rule_group_duration_seconds summary
loki_prometheus_rule_group_duration_seconds{user="user1",quantile="0.01"} 1
loki_prometheus_rule_group_duration_seconds{user="user1",quantile="0.05"} 1
loki_prometheus_rule_group_duration_seconds{user="user1",quantile="0.5"} 1
loki_prometheus_rule_group_duration_seconds{user="user1",quantile="0.9"} 1
loki_prometheus_rule_group_duration_seconds{user="user1",quantile="0.99"} 1
loki_prometheus_rule_group_duration_seconds_sum{user="user1"} 1
loki_prometheus_rule_group_duration_seconds_count{user="user1"} 1
loki_prometheus_rule_group_duration_seconds{user="user2",quantile="0.01"} 10
loki_prometheus_rule_group_duration_seconds{user="user2",quantile="0.05"} 10
loki_prometheus_rule_group_duration_seconds{user="user2",quantile="0.5"} 10
loki_prometheus_rule_group_duration_seconds{user="user2",quantile="0.9"} 10
loki_prometheus_rule_group_duration_seconds{user="user2",quantile="0.99"} 10
loki_prometheus_rule_group_duration_seconds_sum{user="user2"} 10
loki_prometheus_rule_group_duration_seconds_count{user="user2"} 1
loki_prometheus_rule_group_duration_seconds{user="user3",quantile="0.01"} 100
loki_prometheus_rule_group_duration_seconds{user="user3",quantile="0.05"} 100
loki_prometheus_rule_group_duration_seconds{user="user3",quantile="0.5"} 100
loki_prometheus_rule_group_duration_seconds{user="user3",quantile="0.9"} 100
loki_prometheus_rule_group_duration_seconds{user="user3",quantile="0.99"} 100
loki_prometheus_rule_group_duration_seconds_sum{user="user3"} 100
loki_prometheus_rule_group_duration_seconds_count{user="user3"} 1
# HELP loki_prometheus_rule_group_iterations_missed_total The total number of rule group evaluations missed due to slow rule group evaluation.
# TYPE loki_prometheus_rule_group_iterations_missed_total counter
loki_prometheus_rule_group_iterations_missed_total{user="user1"} 2
loki_prometheus_rule_group_iterations_missed_total{user="user2"} 20
loki_prometheus_rule_group_iterations_missed_total{user="user3"} 200
# HELP loki_prometheus_rule_group_iterations_total The total number of scheduled rule group evaluations, whether executed or missed.
# TYPE loki_prometheus_rule_group_iterations_total counter
loki_prometheus_rule_group_iterations_total{user="user1"} 2
loki_prometheus_rule_group_iterations_total{user="user2"} 20
loki_prometheus_rule_group_iterations_total{user="user3"} 200
# HELP loki_prometheus_rule_group_last_duration_seconds The duration of the last rule group evaluation.
# TYPE loki_prometheus_rule_group_last_duration_seconds gauge
loki_prometheus_rule_group_last_duration_seconds{user="user1"} 2000
loki_prometheus_rule_group_last_duration_seconds{user="user2"} 20000
loki_prometheus_rule_group_last_duration_seconds{user="user3"} 200000
# HELP loki_prometheus_rule_group_last_evaluation_timestamp_seconds The timestamp of the last rule group evaluation in seconds.
# TYPE loki_prometheus_rule_group_last_evaluation_timestamp_seconds gauge
loki_prometheus_rule_group_last_evaluation_timestamp_seconds{user="user1"} 2000
loki_prometheus_rule_group_last_evaluation_timestamp_seconds{user="user2"} 20000
loki_prometheus_rule_group_last_evaluation_timestamp_seconds{user="user3"} 200000
# HELP loki_prometheus_rule_group_rules The number of rules.
# TYPE loki_prometheus_rule_group_rules gauge
loki_prometheus_rule_group_rules{user="user1"} 2000
loki_prometheus_rule_group_rules{user="user2"} 20000
loki_prometheus_rule_group_rules{user="user3"} 200000
`))
	require.NoError(t, err)
}

func populateManager(base float64) *prometheus.Registry {
	r := prometheus.NewRegistry()

	metrics := newGroupMetrics(r)

	metrics.evalDuration.Observe(base)
	metrics.iterationDuration.Observe(base)

	metrics.iterationsScheduled.WithLabelValues("group_one").Add(base)
	metrics.iterationsScheduled.WithLabelValues("group_two").Add(base)
	metrics.iterationsMissed.WithLabelValues("group_one").Add(base)
	metrics.iterationsMissed.WithLabelValues("group_two").Add(base)
	metrics.evalTotal.WithLabelValues("group_one").Add(base)
	metrics.evalTotal.WithLabelValues("group_two").Add(base)
	metrics.evalFailures.WithLabelValues("group_one").Add(base)
	metrics.evalFailures.WithLabelValues("group_two").Add(base)

	metrics.groupLastEvalTime.WithLabelValues("group_one").Add(base * 1000)
	metrics.groupLastEvalTime.WithLabelValues("group_two").Add(base * 1000)

	metrics.groupLastDuration.WithLabelValues("group_one").Add(base * 1000)
	metrics.groupLastDuration.WithLabelValues("group_two").Add(base * 1000)

	metrics.groupRules.WithLabelValues("group_one").Add(base * 1000)
	metrics.groupRules.WithLabelValues("group_two").Add(base * 1000)

	metrics.groupLastEvalSamples.WithLabelValues("group_one").Add(base * 1000)
	metrics.groupLastEvalSamples.WithLabelValues("group_two").Add(base * 1000)

	return r
}

// Copied from github.com/prometheus/rules/manager.go
type groupMetrics struct {
	evalDuration         prometheus.Summary
	iterationDuration    prometheus.Summary
	iterationsMissed     *prometheus.CounterVec
	iterationsScheduled  *prometheus.CounterVec
	evalTotal            *prometheus.CounterVec
	evalFailures         *prometheus.CounterVec
	groupInterval        *prometheus.GaugeVec
	groupLastEvalTime    *prometheus.GaugeVec
	groupLastDuration    *prometheus.GaugeVec
	groupRules           *prometheus.GaugeVec
	groupLastEvalSamples *prometheus.GaugeVec
}

func newGroupMetrics(r prometheus.Registerer) *groupMetrics {
	m := &groupMetrics{
		evalDuration: promauto.With(r).NewSummary(
			prometheus.SummaryOpts{
				Name:       "prometheus_rule_evaluation_duration_seconds",
				Help:       "The duration for a rule to execute.",
				Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
			}),
		iterationDuration: promauto.With(r).NewSummary(prometheus.SummaryOpts{
			Name:       "prometheus_rule_group_duration_seconds",
			Help:       "The duration of rule group evaluations.",
			Objectives: map[float64]float64{0.01: 0.001, 0.05: 0.005, 0.5: 0.05, 0.90: 0.01, 0.99: 0.001},
		}),
		iterationsMissed: promauto.With(r).NewCounterVec(
			prometheus.CounterOpts{
				Name: "prometheus_rule_group_iterations_missed_total",
				Help: "The total number of rule group evaluations missed due to slow rule group evaluation.",
			},
			[]string{"rule_group"},
		),
		iterationsScheduled: promauto.With(r).NewCounterVec(
			prometheus.CounterOpts{
				Name: "prometheus_rule_group_iterations_total",
				Help: "The total number of scheduled rule group evaluations, whether executed or missed.",
			},
			[]string{"rule_group"},
		),
		evalTotal: promauto.With(r).NewCounterVec(
			prometheus.CounterOpts{
				Name: "prometheus_rule_evaluations_total",
				Help: "The total number of rule evaluations.",
			},
			[]string{"rule_group"},
		),
		evalFailures: promauto.With(r).NewCounterVec(
			prometheus.CounterOpts{
				Name: "prometheus_rule_evaluation_failures_total",
				Help: "The total number of rule evaluation failures.",
			},
			[]string{"rule_group"},
		),
		groupInterval: promauto.With(r).NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "prometheus_rule_group_interval_seconds",
				Help: "The interval of a rule group.",
			},
			[]string{"rule_group"},
		),
		groupLastEvalTime: promauto.With(r).NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "prometheus_rule_group_last_evaluation_timestamp_seconds",
				Help: "The timestamp of the last rule group evaluation in seconds.",
			},
			[]string{"rule_group"},
		),
		groupLastDuration: promauto.With(r).NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "prometheus_rule_group_last_duration_seconds",
				Help: "The duration of the last rule group evaluation.",
			},
			[]string{"rule_group"},
		),
		groupRules: promauto.With(r).NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "prometheus_rule_group_rules",
				Help: "The number of rules.",
			},
			[]string{"rule_group"},
		),
		groupLastEvalSamples: promauto.With(r).NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "prometheus_rule_group_last_evaluation_samples",
				Help: "The number of samples returned during the last rule group evaluation.",
			},
			[]string{"rule_group"},
		),
	}

	return m
}

func TestMetricsArePerUser(t *testing.T) {
	mainReg := prometheus.NewPedanticRegistry()

	managerMetrics := NewManagerMetrics(true, nil, constants.Loki)
	mainReg.MustRegister(managerMetrics)
	managerMetrics.AddUserRegistry("user1", populateManager(1))
	managerMetrics.AddUserRegistry("user2", populateManager(10))
	managerMetrics.AddUserRegistry("user3", populateManager(100))

	ch := make(chan prometheus.Metric)

	defer func() {
		// drain the channel, so that collecting gouroutine can stop.
		// This is useful if test fails.
		//nolint:revive
		for range ch {
		}
	}()

	go func() {
		managerMetrics.Collect(ch)
		close(ch)
	}()

	for m := range ch {
		desc := m.Desc()

		dtoM := &dto.Metric{}
		err := m.Write(dtoM)

		require.NoError(t, err)

		foundUserLabel := false
		for _, l := range dtoM.Label {
			if l.GetName() == "user" {
				foundUserLabel = true
				break
			}
		}

		assert.True(t, foundUserLabel, "user label not found for metric %s", desc.String())
	}
}

func TestMetricLabelTransformer(t *testing.T) {
	mainReg := prometheus.NewPedanticRegistry()

	managerMetrics := NewManagerMetrics(false, func(k, v string) string {
		if k == RuleGroupLabel {
			return RemoveRuleTokenFromGroupName(v)
		}

		return v
	}, constants.Loki)
	mainReg.MustRegister(managerMetrics)

	reg := prometheus.NewRegistry()
	metrics := newGroupMetrics(reg)

	r := rulespb.RuleDesc{
		Alert:  "MyAlert",
		Expr:   "count({foo=\"bar\"}) > 0",
		Labels: logproto.FromLabelsToLabelAdapters(labels.FromStrings("foo", "bar")),
	}
	const ruleGroupName = "my_rule_group"
	gr := rulespb.RuleGroupDesc{
		Name:      ruleGroupName,
		Namespace: "namespace",
		Rules:     []*rulespb.RuleDesc{&r},
	}

	metrics.iterationsScheduled.WithLabelValues(AddRuleTokenToGroupName(&gr, &r)).Add(1)
	managerMetrics.AddUserRegistry("user1", reg)

	ch := make(chan prometheus.Metric)

	defer func() {
		// drain the channel, so that collecting gouroutine can stop.
		// This is useful if test fails.
		//nolint:revive
		for range ch {
		}
	}()

	go func() {
		managerMetrics.Collect(ch)
		close(ch)
	}()

	for m := range ch {
		dtoM := &dto.Metric{}
		err := m.Write(dtoM)

		require.NoError(t, err)

		for _, l := range dtoM.Label {
			if l.GetName() == RuleGroupLabel {
				// if the value has been capitalised, we know it was processed by the label transformer
				assert.Equal(t, l.GetValue(), ruleGroupName)
			}
		}
	}
}
