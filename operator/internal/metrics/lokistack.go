package metrics

import (
	"context"
	"slices"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
)

const (
	metricsPrefix = "lokistack_"
)

var (
	metricsCommonLabels = []string{
		"stack_namespace",
		"stack_name",
		"size",
	}

	lokiStackInfoDesc = prometheus.NewDesc(
		metricsPrefix+"info",
		"Information about deployed LokiStack instances. Value is always 1.",
		metricsCommonLabels, nil,
	)

	lokiStackConditionsCountDesc = prometheus.NewDesc(
		metricsPrefix+"status_condition",
		"Counts the current status conditions of the LokiStack.",
		append(metricsCommonLabels, "condition", "reason", "status"), nil,
	)

	// Main conditions should always be present to make things like alerts easier to write.
	conditionInDefault = []lokiv1.LokiStackConditionType{lokiv1.ConditionFailed, lokiv1.ConditionReady, lokiv1.ConditionPending, lokiv1.ConditionDegraded}
)

func RegisterLokiStackCollector(log logr.Logger, k8sClient client.Client, registry prometheus.Registerer) error {
	metrics := &lokiStackCollector{
		log:       log,
		k8sClient: k8sClient,
	}

	return registry.Register(metrics)
}

type lokiStackCollector struct {
	log       logr.Logger
	k8sClient client.Client
}

func (l *lokiStackCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- lokiStackInfoDesc
	ch <- lokiStackConditionsCountDesc
}

func (l *lokiStackCollector) Collect(m chan<- prometheus.Metric) {
	ctx := context.TODO()

	stackList := &lokiv1.LokiStackList{}
	err := l.k8sClient.List(ctx, stackList)
	if err != nil {
		l.log.Error(err, "failed to get list of LokiStacks for metrics")
		return
	}

	for _, stack := range stackList.Items {
		labels := []string{
			stack.Namespace,
			stack.Name,
			string(stack.Spec.Size),
		}

		m <- prometheus.MustNewConstMetric(lokiStackInfoDesc, prometheus.GaugeValue, 1.0, labels...)

		for _, c := range conditionInDefault {
			if !slices.ContainsFunc(stack.Status.Conditions, func(cond metav1.Condition) bool { return cond.Type == string(c) }) {
				m <- prometheus.MustNewConstMetric(lokiStackConditionsCountDesc, prometheus.GaugeValue, 0.0, append(labels, string(c), "", "true")...)
				m <- prometheus.MustNewConstMetric(lokiStackConditionsCountDesc, prometheus.GaugeValue, 1.0, append(labels, string(c), "", "false")...)
			}
		}

		for _, c := range stack.Status.Conditions {
			activeValue := 0.0
			if c.Status == metav1.ConditionTrue {
				activeValue = 1.0
			}

			// This mirrors the behavior of kube_state_metrics, which creates two metrics for each condition,
			// one for each status (true/false).
			m <- prometheus.MustNewConstMetric(
				lokiStackConditionsCountDesc,
				prometheus.GaugeValue, activeValue,
				append(labels, c.Type, c.Reason, "true")...,
			)
			m <- prometheus.MustNewConstMetric(
				lokiStackConditionsCountDesc,
				prometheus.GaugeValue, 1.0-activeValue,
				append(labels, c.Type, c.Reason, "false")...,
			)
		}
	}
}
