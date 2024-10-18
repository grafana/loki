package manifests

import (
	"slices"
	"strings"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests/internal/config"
)

func defaultOpenShiftLoggingAttributes(disableRecommended bool) config.OTLPAttributeConfig {
	result := config.OTLPAttributeConfig{
		DefaultIndexLabels: []string{
			"k8s.namespace.name",
			"kubernetes.namespace_name",
			"log_source",
			"log_type",
			"openshift.cluster.uid",
			"openshift.log.source",
			"openshift.log.type",
		},
	}

	if disableRecommended {
		return result
	}

	result.DefaultIndexLabels = append(result.DefaultIndexLabels,
		"k8s.container.name",
		"k8s.node.name",
		"k8s.node.uid",
		"k8s.pod.name",
		"k8s.pod.uid",
		"kubernetes.container_name",
		"kubernetes.host",
		"kubernetes.pod_name",
	)
	slices.Sort(result.DefaultIndexLabels)

	// TODO decide whether we want to split the default configuration by tenant
	result.Global = &config.OTLPTenantAttributeConfig{
		ResourceAttributes: []config.OTLPAttribute{
			{
				Action: config.OTLPAttributeActionStreamLabel,
				Regex:  "openshift\\.labels\\..+",
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
					"process.command_line",
					"process.executable.name",
					"process.executable.path",
					"process.pid",
					"service.name",
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

func defaultOTLPAttributeConfig(ts *lokiv1.TenantsSpec) config.OTLPAttributeConfig {
	if ts == nil || ts.Mode != lokiv1.OpenshiftLogging {
		return config.OTLPAttributeConfig{}
	}

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

func sortAndDeduplicateOTLPConfig(cfg config.OTLPAttributeConfig) config.OTLPAttributeConfig {
	if len(cfg.DefaultIndexLabels) > 1 {
		slices.Sort(cfg.DefaultIndexLabels)
		cfg.DefaultIndexLabels = slices.Compact(cfg.DefaultIndexLabels)
	}

	if cfg.Global != nil {
		if len(cfg.Global.ResourceAttributes) > 1 {
			cfg.Global.ResourceAttributes = sortAndDeduplicateOTLPAttributes(cfg.Global.ResourceAttributes)
		}

		if len(cfg.Global.ScopeAttributes) > 1 {
			cfg.Global.ScopeAttributes = sortAndDeduplicateOTLPAttributes(cfg.Global.ScopeAttributes)
		}

		if len(cfg.Global.LogAttributes) > 1 {
			cfg.Global.LogAttributes = sortAndDeduplicateOTLPAttributes(cfg.Global.LogAttributes)
		}
	}

	for _, t := range cfg.Tenants {
		if len(t.ResourceAttributes) > 1 {
			t.ResourceAttributes = sortAndDeduplicateOTLPAttributes(t.ResourceAttributes)
		}

		if len(t.ScopeAttributes) > 1 {
			t.ScopeAttributes = sortAndDeduplicateOTLPAttributes(t.ScopeAttributes)
		}

		if len(t.LogAttributes) > 1 {
			t.LogAttributes = sortAndDeduplicateOTLPAttributes(t.LogAttributes)
		}
	}

	return cfg
}

func sortAndDeduplicateOTLPAttributes(attrs []config.OTLPAttribute) []config.OTLPAttribute {
	slices.SortFunc(attrs, func(a, b config.OTLPAttribute) int {
		action := strings.Compare(string(a.Action), string(b.Action))
		if action != 0 {
			return action
		}

		if a.Regex != "" && b.Regex != "" {
			return strings.Compare(a.Regex, b.Regex)
		}

		if a.Regex != "" && b.Regex == "" {
			return 1
		}

		if a.Regex == "" && b.Regex != "" {
			return -1
		}

		return 0
	})

	for i := 0; i < len(attrs)-1; i++ {
		a := attrs[i]
		if a.Regex != "" {
			continue
		}

		slices.Sort(a.Names)
		attrs[i] = a

		next := attrs[i+1]
		if next.Regex != "" {
			continue
		}

		if a.Action != next.Action {
			continue
		}

		// Combine attribute definitions if they have the same action and just contain names
		a.Names = append(a.Names, next.Names...)
		slices.Sort(a.Names)
		a.Names = slices.Compact(a.Names)

		// Remove the "next" attribute definition
		attrs[i] = a
		attrs = append(attrs[:i+1], attrs[i+2:]...)
		i--
	}

	return attrs
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
				tenantResult := &config.OTLPTenantAttributeConfig{}

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

	return sortAndDeduplicateOTLPConfig(result)
}
