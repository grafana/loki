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

	result.Global.ResourceAttributes = append(result.Global.ResourceAttributes,
		config.OTLPAttribute{
			Action: config.OTLPAttributeActionMetadata,
			Names: []string{
				"k8s.node.uid",
				"k8s.pod.uid",
				"k8s.replicaset.name",
				"process.command_line",
				"process.executable.name",
				"process.executable.path",
				"process.pid",
			},
		},
		config.OTLPAttribute{
			Action: config.OTLPAttributeActionMetadata,
			Regex:  `k8s\.pod\.labels\..+`,
		},
		config.OTLPAttribute{
			Action: config.OTLPAttributeActionMetadata,
			Regex:  `openshift\.labels\..+`,
		},
	)

	result.Global.LogAttributes = []config.OTLPAttribute{
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
				"level",
				"log.iostream",
			},
		},
		{
			Action: config.OTLPAttributeActionMetadata,
			Regex:  `k8s\.event\.annotations\..+`,
		},
		{
			Action: config.OTLPAttributeActionMetadata,
			Regex:  `systemd\.t\..+`,
		},
		{
			Action: config.OTLPAttributeActionMetadata,
			Regex:  `systemd\.u\..+`,
		},
	}

	return result
}
