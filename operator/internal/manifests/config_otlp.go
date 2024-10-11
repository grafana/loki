package manifests

import (
	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests/internal/config"
)

func defaultOpenShiftLoggingAttributes(disableRecommended bool) config.OTLPAttributeConfig {
	result := config.OTLPAttributeConfig{
		DefaultIndexLabels: []string{
			"openshift.cluster.uid",
			"openshift.log.source",
			"log_source",
			"openshift.log.type",
			"log_type",
			"k8s.namespace.name",
			"kubernetes.namespace_name",
		},
	}

	if !disableRecommended {
		result.DefaultIndexLabels = append(result.DefaultIndexLabels,
			"k8s.node.name",
			"k8s.node.uid",
			"k8s.container.name",
			"kubernetes.container_name",
			"k8s.pod.name",
			"k8s.pod.uid",
			"kubernetes.pod_name",
		)

		result.Global = &config.OTLPTenantAttributeConfig{
			ResourceAttributes: []config.OTLPAttribute{
				{
					Action: config.OTLPAttributeActionStreamLabel,
					Regex:  "openshift\\.labels\\..+",
				},
				{
					Action: config.OTLPAttributeActionMetadata,
					Regex:  "k8s\\.pod\\.labels\\..+",
				},
				{
					Action: config.OTLPAttributeActionMetadata,
					Names: []string{
						"k8s.cronjob.name",
						"k8s.daemonset.name",
						"k8s.deployment.name",
						"k8s.job.name",
						"k8s.replicaset.name",
						"k8s.statefulset.name",
						"process.executable.name",
						"process.executable.path",
						"process.command_line",
						"process.pid",
						"service.name",
					},
				},
			},
			LogAttributes: []config.OTLPAttribute{
				{
					Action: config.OTLPAttributeActionMetadata,
					Names: []string{
						"log.iostream",
						"k8s.event.level",
						"k8s.event.stage",
						"k8s.event.user_agent",
						"k8s.event.request.uri",
						"k8s.event.response.code",
						"k8s.event.object_ref.resource",
						"k8s.event.object_ref.name",
						"k8s.event.object_ref.api.group",
						"k8s.event.object_ref.api.version",
						"k8s.user.username",
						"k8s.user.groups",
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
	}

	return result
}

func defaultOTLPAttributeConfig(ts *lokiv1.TenantsSpec) config.OTLPAttributeConfig {
	if ts == nil || ts.Mode != lokiv1.OpenshiftLogging {
		return config.OTLPAttributeConfig{}
	}

	// TODO decide which of these can be disabled by using "disableRecommendedAttributes"
	// TODO decide whether we want to split the default configuration by tenant
	disableRecommended := false
	if ts.Openshift != nil && ts.Openshift.OTLP != nil {
		disableRecommended = ts.Openshift.OTLP.DisableRecommendedAttributes
	}

	return defaultOpenShiftLoggingAttributes(disableRecommended)
}

func convertAttributeReferences(refs []lokiv1.OTLPAttributeReference, action config.OTLPAttributeAction) []config.OTLPAttribute {
	var (
		names  []string
		result []config.OTLPAttribute
	)

	for _, attr := range refs {
		if attr.Regex {
			result = append(result, config.OTLPAttribute{
				Action: action,
				Regex:  attr.Name,
			})
			continue
		}

		names = append(names, attr.Name)
	}

	if len(names) > 0 {
		result = append(result, config.OTLPAttribute{
			Action: action,
			Names:  names,
		})
	}

	return result
}

func otlpAttributeConfig(ls *lokiv1.LokiStackSpec) config.OTLPAttributeConfig {
	result := defaultOTLPAttributeConfig(ls.Tenants)

	if ls.Limits != nil {
		if ls.Limits.Global != nil && ls.Limits.Global.OTLP != nil {
			globalOTLP := ls.Limits.Global.OTLP

			if globalOTLP.StreamLabels != nil {
				regularExpressions := []string{}
				for _, attr := range globalOTLP.StreamLabels.ResourceAttributes {
					if attr.Regex {
						regularExpressions = append(regularExpressions, attr.Name)
						continue
					}

					result.DefaultIndexLabels = append(result.DefaultIndexLabels, attr.Name)
				}

				if len(regularExpressions) > 0 {
					result.Global = &config.OTLPTenantAttributeConfig{}

					for _, re := range regularExpressions {
						result.Global.ResourceAttributes = append(result.Global.ResourceAttributes, config.OTLPAttribute{
							Action: config.OTLPAttributeActionStreamLabel,
							Regex:  re,
						})
					}
				}
			}

			if structuredMetadata := globalOTLP.StructuredMetadata; structuredMetadata != nil {
				if result.Global == nil {
					result.Global = &config.OTLPTenantAttributeConfig{}
				}

				if resAttr := structuredMetadata.ResourceAttributes; len(resAttr) > 0 {
					result.Global.ResourceAttributes = append(result.Global.ResourceAttributes,
						convertAttributeReferences(resAttr, config.OTLPAttributeActionMetadata)...)
				}

				if scopeAttr := structuredMetadata.ScopeAttributes; len(scopeAttr) > 0 {
					result.Global.ScopeAttributes = append(result.Global.ScopeAttributes,
						convertAttributeReferences(scopeAttr, config.OTLPAttributeActionMetadata)...)
				}

				if logAttr := structuredMetadata.LogAttributes; len(logAttr) > 0 {
					result.Global.LogAttributes = append(result.Global.LogAttributes,
						convertAttributeReferences(logAttr, config.OTLPAttributeActionMetadata)...)
				}
			}
		}

		for tenant, tenantLimits := range ls.Limits.Tenants {
			if tenantLimits.OTLP != nil {
				tenantOTLP := tenantLimits.OTLP
				tenantResult := &config.OTLPTenantAttributeConfig{
					IgnoreGlobalStreamLabels: tenantOTLP.IgnoreGlobalStreamLabels,
				}

				if streamLabels := tenantOTLP.StreamLabels; streamLabels != nil {
					tenantResult.ResourceAttributes = append(tenantResult.ResourceAttributes,
						convertAttributeReferences(streamLabels.ResourceAttributes, config.OTLPAttributeActionStreamLabel)...)
				}

				if structuredMetadata := tenantOTLP.StructuredMetadata; structuredMetadata != nil {
					if resAttr := structuredMetadata.ResourceAttributes; len(resAttr) > 0 {
						tenantResult.ResourceAttributes = append(tenantResult.ResourceAttributes,
							convertAttributeReferences(resAttr, config.OTLPAttributeActionMetadata)...)
					}

					if scopeAttr := structuredMetadata.ScopeAttributes; len(scopeAttr) > 0 {
						tenantResult.ScopeAttributes = append(tenantResult.ScopeAttributes,
							convertAttributeReferences(scopeAttr, config.OTLPAttributeActionMetadata)...)
					}

					if logAttr := structuredMetadata.LogAttributes; len(logAttr) > 0 {
						tenantResult.LogAttributes = append(tenantResult.LogAttributes,
							convertAttributeReferences(logAttr, config.OTLPAttributeActionMetadata)...)
					}
				}

				if result.Tenants == nil {
					result.Tenants = map[string]*config.OTLPTenantAttributeConfig{}
				}
				result.Tenants[tenant] = tenantResult
			}
		}
	}

	return result
}
