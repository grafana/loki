package openshift

import (
	"slices"

	"github.com/grafana/loki/operator/internal/manifests/internal/config"
)

// DefaultOTLPAttributes provides the required/recommended set of OTLP attributes for OpenShift Logging.
func DefaultOTLPAttributes(disableRecommended bool) config.OTLPAttributeConfig {
	result := config.OTLPAttributeConfig{
		RemoveDefaultLabels: true,
		Global: &config.OTLPTenantAttributeConfig{
			ResourceAttributes: []config.OTLPAttribute{
				{
					Action: config.OTLPAttributeActionStreamLabel,
					Names: []string{
						"k8s.namespace.name",
						"kubernetes.namespace_name",
						"log_source",
						"log_type",
						"openshift.cluster.uid",
						"openshift.log.source",
						"openshift.log.type",
					},
				},
			},
		},
	}

	if disableRecommended {
		return result
	}

	result.Global.ResourceAttributes[0].Names = append(result.Global.ResourceAttributes[0].Names,
		"k8s.container.name",
		"k8s.cronjob.name",
		"k8s.daemonset.name",
		"k8s.deployment.name",
		"k8s.job.name",
		"k8s.node.name",
		"k8s.pod.name",
		"k8s.statefulset.name",
		"kubernetes.container_name",
		"kubernetes.host",
		"kubernetes.pod_name",
		"service.name",
	)
	slices.Sort(result.Global.ResourceAttributes[0].Names)

	return result
}
