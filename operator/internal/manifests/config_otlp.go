package manifests

import (
	"slices"
	"strings"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests/internal/config"
	"github.com/grafana/loki/operator/internal/manifests/openshift/otlp"
)

func defaultOTLPAttributeConfig(ts *lokiv1.TenantsSpec) config.OTLPAttributeConfig {
	if ts == nil || ts.Mode != lokiv1.OpenshiftLogging {
		return config.OTLPAttributeConfig{}
	}

	disableRecommended := false
	if ts.Openshift != nil && ts.Openshift.OTLP != nil {
		disableRecommended = ts.Openshift.OTLP.DisableRecommendedAttributes
	}

	return config.OTLPAttributeConfig{
		RemoveDefaultLabels: true,
		Global: &config.OTLPTenantAttributeConfig{
			ResourceAttributes: []config.OTLPAttribute{
				{
					Action: config.OTLPAttributeActionStreamLabel,
					Names:  otlp.DefaultOTLPAttributes(disableRecommended),
				},
			},
		},
	}
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

func copyOTLPAttributes(in []config.OTLPAttribute) []config.OTLPAttribute {
	result := make([]config.OTLPAttribute, 0, len(in))
	for _, attr := range in {
		result = append(result, config.OTLPAttribute{
			Action: attr.Action,
			Names:  slices.Clone(attr.Names),
			Regex:  attr.Regex,
		})
	}

	return result
}

func copyTenantAttributeConfig(in *config.OTLPTenantAttributeConfig) *config.OTLPTenantAttributeConfig {
	result := &config.OTLPTenantAttributeConfig{}
	if in == nil {
		return result
	}

	if len(in.ResourceAttributes) > 0 {
		result.ResourceAttributes = copyOTLPAttributes(in.ResourceAttributes)
	}

	if len(in.ScopeAttributes) > 0 {
		result.ScopeAttributes = copyOTLPAttributes(in.ScopeAttributes)
	}

	if len(in.LogAttributes) > 0 {
		result.LogAttributes = copyOTLPAttributes(in.LogAttributes)
	}

	return result
}

func convertTenantAttributeReferences(otlpSpec *lokiv1.OTLPSpec, base *config.OTLPTenantAttributeConfig) *config.OTLPTenantAttributeConfig {
	result := copyTenantAttributeConfig(base)

	if streamLabels := otlpSpec.StreamLabels; streamLabels != nil {
		result.ResourceAttributes = append(result.ResourceAttributes,
			convertAttributeReferences(streamLabels.ResourceAttributes, config.OTLPAttributeActionStreamLabel)...)
	}

	if dropLabels := otlpSpec.Drop; dropLabels != nil {
		if resAttr := dropLabels.ResourceAttributes; len(resAttr) > 0 {
			result.ResourceAttributes = append(result.ResourceAttributes,
				convertAttributeReferences(resAttr, config.OTLPAttributeActionDrop)...)
		}

		if scopeAttr := dropLabels.ScopeAttributes; len(scopeAttr) > 0 {
			result.ScopeAttributes = append(result.ScopeAttributes,
				convertAttributeReferences(scopeAttr, config.OTLPAttributeActionDrop)...)
		}

		if logAttr := dropLabels.LogAttributes; len(logAttr) > 0 {
			result.LogAttributes = append(result.LogAttributes,
				convertAttributeReferences(logAttr, config.OTLPAttributeActionDrop)...)
		}
	}

	return result
}

func sortAndDeduplicateOTLPConfig(cfg config.OTLPAttributeConfig) config.OTLPAttributeConfig {
	if cfg.Global != nil {
		if len(cfg.Global.ResourceAttributes) > 0 {
			cfg.Global.ResourceAttributes = sortAndDeduplicateOTLPAttributes(cfg.Global.ResourceAttributes)
		}

		if len(cfg.Global.ScopeAttributes) > 0 {
			cfg.Global.ScopeAttributes = sortAndDeduplicateOTLPAttributes(cfg.Global.ScopeAttributes)
		}

		if len(cfg.Global.LogAttributes) > 0 {
			cfg.Global.LogAttributes = sortAndDeduplicateOTLPAttributes(cfg.Global.LogAttributes)
		}
	}

	for _, t := range cfg.Tenants {
		if len(t.ResourceAttributes) > 0 {
			t.ResourceAttributes = sortAndDeduplicateOTLPAttributes(t.ResourceAttributes)
		}

		if len(t.ScopeAttributes) > 0 {
			t.ScopeAttributes = sortAndDeduplicateOTLPAttributes(t.ScopeAttributes)
		}

		if len(t.LogAttributes) > 0 {
			t.LogAttributes = sortAndDeduplicateOTLPAttributes(t.LogAttributes)
		}
	}

	return cfg
}

func sortAndDeduplicateOTLPAttributes(attrs []config.OTLPAttribute) []config.OTLPAttribute {
	if len(attrs) == 0 {
		// Skip everything for zero items
		return attrs
	}

	if len(attrs[0].Names) > 1 {
		// If the first OTLPAttribute has names, then sort and de-duplicate them
		slices.Sort(attrs[0].Names)
		attrs[0].Names = slices.Compact(attrs[0].Names)
	}

	if len(attrs) == 1 {
		// If there's only one item, then skip the complex sorting
		return attrs
	}

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
			result.RemoveDefaultLabels = true
			globalOTLP := ls.Limits.Global.OTLP

			if streamLabels := globalOTLP.StreamLabels; streamLabels != nil {
				if result.Global == nil {
					result.Global = &config.OTLPTenantAttributeConfig{}
				}

				if resAttr := streamLabels.ResourceAttributes; len(resAttr) > 0 {
					result.Global.ResourceAttributes = append(result.Global.ResourceAttributes,
						convertAttributeReferences(resAttr, config.OTLPAttributeActionStreamLabel)...)
				}
			}

			if dropLabels := globalOTLP.Drop; dropLabels != nil {
				if result.Global == nil {
					result.Global = &config.OTLPTenantAttributeConfig{}
				}

				if resAttr := dropLabels.ResourceAttributes; len(resAttr) > 0 {
					result.Global.ResourceAttributes = append(result.Global.ResourceAttributes,
						convertAttributeReferences(resAttr, config.OTLPAttributeActionDrop)...)
				}

				if scopeAttr := dropLabels.ScopeAttributes; len(scopeAttr) > 0 {
					result.Global.ScopeAttributes = append(result.Global.ScopeAttributes,
						convertAttributeReferences(scopeAttr, config.OTLPAttributeActionDrop)...)
				}

				if logAttr := dropLabels.LogAttributes; len(logAttr) > 0 {
					result.Global.LogAttributes = append(result.Global.LogAttributes,
						convertAttributeReferences(logAttr, config.OTLPAttributeActionDrop)...)
				}
			}
		}

		for tenant, tenantLimits := range ls.Limits.Tenants {
			if tenantLimits.OTLP != nil {
				result.RemoveDefaultLabels = true

				if result.Tenants == nil {
					result.Tenants = map[string]*config.OTLPTenantAttributeConfig{}
				}

				tenantResult := convertTenantAttributeReferences(tenantLimits.OTLP, result.Global)
				result.Tenants[tenant] = tenantResult
			}
		}
	}

	return sortAndDeduplicateOTLPConfig(result)
}
