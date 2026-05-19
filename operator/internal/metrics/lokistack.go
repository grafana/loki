package metrics

import (
	"context"
	"slices"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests"
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

	lokiStackStorageInfoDesc = prometheus.NewDesc(
		metricsPrefix+"storage_info",
		"Information about LokiStack storage backend configuration.",
		append(metricsCommonLabels, "backend_type", "credential_mode"), nil,
	)

	lokiStackStorageSchemaVersionDesc = prometheus.NewDesc(
		metricsPrefix+"storage_schema_version",
		"Storage schema versions configured for the LokiStack.",
		append(metricsCommonLabels, "version"), nil,
	)

	lokiStackComponentCustomReplicasDesc = prometheus.NewDesc(
		metricsPrefix+"component_custom_replicas",
		"User configured replica count for components",
		append(metricsCommonLabels, "component"), nil,
	)

	lokiStackIngestionRateLimitDesc = prometheus.NewDesc(
		metricsPrefix+"global_ingestion_rate_limit_mb",
		"Global ingestion rate limit in MB/s.",
		metricsCommonLabels, nil,
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
	ch <- lokiStackStorageInfoDesc
	ch <- lokiStackStorageSchemaVersionDesc
	ch <- lokiStackComponentCustomReplicasDesc
	ch <- lokiStackIngestionRateLimitDesc
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

		storageLabels := append(labels, string(stack.Spec.Storage.Secret.Type), getStorageCredentialsMode(&stack))
		m <- prometheus.MustNewConstMetric(
			lokiStackStorageInfoDesc,
			prometheus.GaugeValue, 1.0,
			storageLabels...)

		for _, schema := range stack.Spec.Storage.Schemas {
			schemaLabels := append(labels, string(schema.Version))
			m <- prometheus.MustNewConstMetric(lokiStackStorageSchemaVersionDesc,
				prometheus.GaugeValue, 1.0,
				schemaLabels...)
		}

		componentCustomReplicas := getComponentCustomReplicas(&stack)
		for component, replicas := range componentCustomReplicas {
			componentLabels := append(labels, component)
			m <- prometheus.MustNewConstMetric(
				lokiStackComponentCustomReplicasDesc,
				prometheus.GaugeValue,
				float64(replicas),
				componentLabels...)
		}

		if ingestionRate := getGlobalIngestionRateLimit(&stack); ingestionRate > 0 {
			m <- prometheus.MustNewConstMetric(
				lokiStackIngestionRateLimitDesc,
				prometheus.GaugeValue,
				float64(ingestionRate),
				labels...)
		}
	}
}

func getStorageCredentialsMode(stack *lokiv1.LokiStack) string {
	if stack.Status.Storage.CredentialMode != "" {
		return string(stack.Status.Storage.CredentialMode)
	}
	if stack.Spec.Storage.Secret.CredentialMode != "" {
		return string(stack.Spec.Storage.Secret.CredentialMode)
	}
	return string(lokiv1.CredentialModeStatic)
}

func getGlobalIngestionRateLimit(stack *lokiv1.LokiStack) int32 {
	if stack.Spec.Limits == nil ||
		stack.Spec.Limits.Global == nil ||
		stack.Spec.Limits.Global.IngestionLimits == nil {
		return 0
	}
	return stack.Spec.Limits.Global.IngestionLimits.IngestionRate
}

func getComponentCustomReplicas(stack *lokiv1.LokiStack) map[string]int32 {
	customReplicas := make(map[string]int32)

	defaults := manifests.DefaultLokiStackSpec(stack.Spec.Size)
	if defaults == nil || defaults.Template == nil {
		return customReplicas
	}

	if stack.Spec.Template != nil {
		checkComponent := func(name string, userSpec *lokiv1.LokiComponentSpec, defaultSpec *lokiv1.LokiComponentSpec) {
			if userSpec != nil && userSpec.Replicas != 0 && defaultSpec != nil {
				if userSpec.Replicas != defaultSpec.Replicas {
					customReplicas[name] = userSpec.Replicas
				}
			}
		}

		checkComponent("distributor", stack.Spec.Template.Distributor, defaults.Template.Distributor)
		checkComponent("ingester", stack.Spec.Template.Ingester, defaults.Template.Ingester)
		checkComponent("querier", stack.Spec.Template.Querier, defaults.Template.Querier)
		checkComponent("query-frontend", stack.Spec.Template.QueryFrontend, defaults.Template.QueryFrontend)
		checkComponent("compactor", stack.Spec.Template.Compactor, defaults.Template.Compactor)
		checkComponent("index-gateway", stack.Spec.Template.IndexGateway, defaults.Template.IndexGateway)
		checkComponent("gateway", stack.Spec.Template.Gateway, defaults.Template.Gateway)
		checkComponent("ruler", stack.Spec.Template.Ruler, defaults.Template.Ruler)
	}
	return customReplicas
}
