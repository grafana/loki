package openshift

import (
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
		"k8s.node.name",
		"k8s.node.uid",
		"k8s.pod.name",
		"k8s.pod.uid",
		"kubernetes.container_name",
		"kubernetes.host",
		"kubernetes.pod_name",
	)

	// TODO decide whether we want to split the default configuration by tenant
	result.Global = &config.OTLPTenantAttributeConfig{
		ResourceAttributes: []config.OTLPAttribute{
			{
				Action: config.OTLPAttributeActionStreamLabel,
				Names: []string{
					"k8s.cronjob.name",
					"k8s.daemonset.name",
					"k8s.deployment.name",
					"k8s.job.name",
					"service.name",
				},
			},
			{
				Action: config.OTLPAttributeActionStreamLabel,
				Regex:  "openshift\\.labels\\..+",
			},
			{
				Action: config.OTLPAttributeActionMetadata,
				Names: []string{
					"k8s.replicaset.name",
					"k8s.statefulset.name",
					"process.command_line",
					"process.executable.name",
					"process.executable.path",
					"process.pid",
				},
			},
			{
				Action: config.OTLPAttributeActionMetadata,
				Regex:  "k8s\\.pod\\.labels\\..+",
			},
		},
		LogAttributes: []config.OTLPAttribute{
			{
				Action: config.OTLPAttributeActionMetadata,
				Names: []string{
					"k8s.event.level",
					"k8s.event.object_ref.api.group",
					"k8s.event.object_ref.api.version",
					"k8s.event.object_ref.name",
					"k8s.event.object_ref.resource",
					"k8s.event.request.uri",
					"k8s.event.response.code",
					"k8s.event.stage",
					"k8s.event.user_agent",
					"k8s.user.groups",
					"k8s.user.username",
					"log.iostream",
				},
			},
			{
				Action: config.OTLPAttributeActionMetadata,
				Regex:  "k8s\\.event\\.annotations\\..+",
			},
			{
				Action: config.OTLPAttributeActionMetadata,
				Regex:  "systemd\\.t\\..+",
			},
			{
				Action: config.OTLPAttributeActionMetadata,
				Regex:  "systemd\\.u\\..+",
			},
		},
	}

	return result
}
