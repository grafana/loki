package openshift

import (
	"slices"

	"github.com/grafana/loki/operator/internal/manifests/internal/config"
	"github.com/grafana/loki/operator/internal/manifests/openshift/otlp"
)

// DefaultOTLPAttributes provides the required/recommended set of OTLP attributes for OpenShift Logging.
func DefaultOTLPAttributes(disableRecommended bool) config.OTLPAttributeConfig {
	result := config.OTLPAttributeConfig{
		RemoveDefaultLabels: true,
		Global: &config.OTLPTenantAttributeConfig{
			ResourceAttributes: []config.OTLPAttribute{
				{
					Action: config.OTLPAttributeActionStreamLabel,
					Names:  otlp.RequiredAttributes,
				},
			},
		},
	}

	if disableRecommended {
		return result
	}

	result.Global.ResourceAttributes[0].Names = append(result.Global.ResourceAttributes[0].Names,
		otlp.RecommendedAttributes...,
	)
	slices.Sort(result.Global.ResourceAttributes[0].Names)

	return result
}
