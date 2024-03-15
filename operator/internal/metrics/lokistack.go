package metrics

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
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

	lokistackWarningsCountDesc = prometheus.NewDesc(
		metricsPrefix+"warnings_count",
		"Counts the number of warnings set on a LokiStack.",
		append(metricsCommonLabels, "reason"), nil,
	)
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

func (l lokiStackCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- lokiStackInfoDesc
	ch <- lokistackWarningsCountDesc
}

func (l lokiStackCollector) Collect(m chan<- prometheus.Metric) {
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

		warningsMap := map[string]int{}
		for _, c := range stack.Status.Conditions {
			if c.Type != string(lokiv1.ConditionWarning) {
				continue
			}

			count := warningsMap[c.Reason]
			count += 1
			warningsMap[c.Reason] = count
		}

		for reason, count := range warningsMap {
			m <- prometheus.MustNewConstMetric(lokistackWarningsCountDesc, prometheus.GaugeValue, float64(count), append(labels, reason)...)
		}
	}
}
