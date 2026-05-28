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
		append(metricsCommonLabels, "object_storage_type", "credential_mode", "schema_version"), nil,
	)

	lokiStackConditionsCountDesc = prometheus.NewDesc(
		metricsPrefix+"status_condition",
		"Counts the current status conditions of the LokiStack.",
		append(metricsCommonLabels, "condition", "reason", "status"), nil,
	)

	lokiStackComponentReplicasDesc = prometheus.NewDesc(
		metricsPrefix+"component_replicas",
		"Replica count for components.",
		append(metricsCommonLabels, "component"), nil,
	)

	lokiStackIngestionRateLimitDesc = prometheus.NewDesc(
		metricsPrefix+"global_ingestion_rate_limit_bytes",
		"Global ingestion rate limit in bytes.",
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
	ch <- lokiStackComponentReplicasDesc
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

		infoLabels := append(labels,
			string(stack.Spec.Storage.Secret.Type),
			getStorageCredentialsMode(&stack),
			getCurrentSchemaVersion(&stack),
		)
		m <- prometheus.MustNewConstMetric(lokiStackInfoDesc, prometheus.GaugeValue, 1.0, infoLabels...)

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

		componentReplicas := getComponentReplicas(&stack)
		for component, replicas := range componentReplicas {
			componentLabels := append(labels, component)
			m <- prometheus.MustNewConstMetric(
				lokiStackComponentReplicasDesc,
				prometheus.GaugeValue,
				float64(replicas),
				componentLabels...)
		}

		if ingestionRate := getGlobalIngestionRateLimit(&stack); ingestionRate > 0 {
			m <- prometheus.MustNewConstMetric(
				lokiStackIngestionRateLimitDesc,
				prometheus.GaugeValue,
				float64(ingestionRate*1024*1024),
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

func getCurrentSchemaVersion(stack *lokiv1.LokiStack) string {
	if len(stack.Spec.Storage.Schemas) == 0 {
		return string(lokiv1.ObjectStorageSchemaV11)
	}

	return string(stack.Spec.Storage.Schemas[len(stack.Spec.Storage.Schemas)-1].Version)
}

func getGlobalIngestionRateLimit(stack *lokiv1.LokiStack) int32 {
	if stack.Spec.Limits != nil &&
		stack.Spec.Limits.Global != nil &&
		stack.Spec.Limits.Global.IngestionLimits != nil &&
		stack.Spec.Limits.Global.IngestionLimits.IngestionRate > 0 {
		return stack.Spec.Limits.Global.IngestionLimits.IngestionRate
	}

	defaults := manifests.DefaultLokiStackSpec(stack.Spec.Size)

	return defaults.Limits.Global.IngestionLimits.IngestionRate
}

func getComponentReplicas(stack *lokiv1.LokiStack) map[string]int32 {
	defaults := manifests.DefaultLokiStackSpec(stack.Spec.Size)
	if defaults == nil || defaults.Template == nil {
		return map[string]int32{}
	}

	userTemplate := &lokiv1.LokiTemplateSpec{}
	if stack.Spec.Template != nil {
		userTemplate = stack.Spec.Template
	}

	components := []struct {
		name     string
		userSpec *lokiv1.LokiComponentSpec
		defSpec  *lokiv1.LokiComponentSpec
	}{
		{"distributor", userTemplate.Distributor, defaults.Template.Distributor},
		{"ingester", userTemplate.Ingester, defaults.Template.Ingester},
		{"querier", userTemplate.Querier, defaults.Template.Querier},
		{"query-frontend", userTemplate.QueryFrontend, defaults.Template.QueryFrontend},
		{"compactor", userTemplate.Compactor, defaults.Template.Compactor},
		{"index-gateway", userTemplate.IndexGateway, defaults.Template.IndexGateway},
		{"gateway", userTemplate.Gateway, defaults.Template.Gateway},
		{"ruler", userTemplate.Ruler, defaults.Template.Ruler},
	}
	replicas := make(map[string]int32, len(components))
	for _, component := range components {
		if component.defSpec == nil {
			continue
		}
		replicas[component.name] = component.defSpec.Replicas
		if component.userSpec != nil && component.userSpec.Replicas != 0 {
			replicas[component.name] = component.userSpec.Replicas
		}
	}
	return replicas
}
