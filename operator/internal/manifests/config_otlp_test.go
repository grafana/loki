package manifests

import (
	"testing"

	"github.com/stretchr/testify/assert"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests/internal/config"
)

func TestOtlpAttributeConfig(t *testing.T) {
	tt := []struct {
		desc       string
		spec       lokiv1.LokiStackSpec
		wantConfig config.OTLPAttributeConfig
	}{
		{
			desc: "empty",
		},
		{
			desc: "global stream label",
			spec: lokiv1.LokiStackSpec{
				Limits: &lokiv1.LimitsSpec{
					Global: &lokiv1.LimitsTemplateSpec{
						OTLP: &lokiv1.OTLPSpec{
							StreamLabels: &lokiv1.OTLPStreamLabelSpec{
								ResourceAttributes: []lokiv1.OTLPAttributeReference{
									{
										Name: "stream.label",
									},
								},
							},
						},
					},
				},
			},
			wantConfig: config.OTLPAttributeConfig{
				DefaultIndexLabels: []string{"stream.label"},
			},
		},
		{
			desc: "global stream label regex",
			spec: lokiv1.LokiStackSpec{
				Limits: &lokiv1.LimitsSpec{
					Global: &lokiv1.LimitsTemplateSpec{
						OTLP: &lokiv1.OTLPSpec{
							StreamLabels: &lokiv1.OTLPStreamLabelSpec{
								ResourceAttributes: []lokiv1.OTLPAttributeReference{
									{
										Name:  "stream\\.label\\.regex\\..+",
										Regex: true,
									},
								},
							},
						},
					},
				},
			},
			wantConfig: config.OTLPAttributeConfig{
				Global: &config.OTLPTenantAttributeConfig{
					ResourceAttributes: []config.OTLPAttribute{
						{
							Action: config.OTLPAttributeActionStreamLabel,
							Regex:  "stream\\.label\\.regex\\..+",
						},
					},
				},
			},
		},
		{
			desc: "global metadata",
			spec: lokiv1.LokiStackSpec{
				Limits: &lokiv1.LimitsSpec{
					Global: &lokiv1.LimitsTemplateSpec{
						OTLP: &lokiv1.OTLPSpec{
							StructuredMetadata: &lokiv1.OTLPMetadataSpec{
								ResourceAttributes: []lokiv1.OTLPAttributeReference{
									{
										Name: "metadata",
									},
								},
							},
						},
					},
				},
			},
			wantConfig: config.OTLPAttributeConfig{
				Global: &config.OTLPTenantAttributeConfig{
					ResourceAttributes: []config.OTLPAttribute{
						{
							Action: config.OTLPAttributeActionMetadata,
							Names:  []string{"metadata"},
						},
					},
				},
			},
		},
		{
			desc: "global combined",
			spec: lokiv1.LokiStackSpec{
				Limits: &lokiv1.LimitsSpec{
					Global: &lokiv1.LimitsTemplateSpec{
						OTLP: &lokiv1.OTLPSpec{
							StreamLabels: &lokiv1.OTLPStreamLabelSpec{
								ResourceAttributes: []lokiv1.OTLPAttributeReference{
									{
										Name: "stream.label",
									},
									{
										Name:  "stream\\.label\\.regex\\..+",
										Regex: true,
									},
								},
							},
							StructuredMetadata: &lokiv1.OTLPMetadataSpec{
								ResourceAttributes: []lokiv1.OTLPAttributeReference{
									{
										Name: "resource.metadata",
									},
									{
										Name:  "resource.metadata\\.other\\..+",
										Regex: true,
									},
								},
								ScopeAttributes: []lokiv1.OTLPAttributeReference{
									{
										Name: "scope.metadata",
									},
									{
										Name:  "scope.metadata\\.other\\..+",
										Regex: true,
									},
								},
								LogAttributes: []lokiv1.OTLPAttributeReference{
									{
										Name: "log.metadata",
									},
									{
										Name:  "log.metadata\\.other\\..+",
										Regex: true,
									},
								},
							},
						},
					},
				},
			},
			wantConfig: config.OTLPAttributeConfig{
				DefaultIndexLabels: []string{"stream.label"},
				Global: &config.OTLPTenantAttributeConfig{
					ResourceAttributes: []config.OTLPAttribute{
						{
							Action: config.OTLPAttributeActionStreamLabel,
							Regex:  "stream\\.label\\.regex\\..+",
						},
						{
							Action: config.OTLPAttributeActionMetadata,
							Names:  []string{"resource.metadata"},
						},
						{
							Action: config.OTLPAttributeActionMetadata,
							Regex:  "resource.metadata\\.other\\..+",
						},
					},
					ScopeAttributes: []config.OTLPAttribute{
						{
							Action: config.OTLPAttributeActionMetadata,
							Names:  []string{"scope.metadata"},
						},
						{
							Action: config.OTLPAttributeActionMetadata,
							Regex:  "scope.metadata\\.other\\..+",
						},
					},
					LogAttributes: []config.OTLPAttribute{
						{
							Action: config.OTLPAttributeActionMetadata,
							Names:  []string{"log.metadata"},
						},
						{
							Action: config.OTLPAttributeActionMetadata,
							Regex:  "log.metadata\\.other\\..+",
						},
					},
				},
			},
		},
		{
			desc: "openshift-logging defaults",
			spec: lokiv1.LokiStackSpec{
				Tenants: &lokiv1.TenantsSpec{
					Mode: lokiv1.OpenshiftLogging,
				},
			},
			wantConfig: defaultOpenShiftLoggingAttributes(),
		},
	}

	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			cfg := otlpAttributeConfig(&tc.spec)

			assert.Equal(t, tc.wantConfig, cfg)
		})
	}
}
