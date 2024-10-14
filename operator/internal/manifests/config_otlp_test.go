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
							Regex:  "resource.metadata\\.other\\..+",
						},
						{
							Action: config.OTLPAttributeActionMetadata,
							Names:  []string{"resource.metadata"},
						},
					},
					ScopeAttributes: []config.OTLPAttribute{
						{
							Action: config.OTLPAttributeActionMetadata,
							Regex:  "scope.metadata\\.other\\..+",
						},
						{
							Action: config.OTLPAttributeActionMetadata,
							Names:  []string{"scope.metadata"},
						},
					},
					LogAttributes: []config.OTLPAttribute{
						{
							Action: config.OTLPAttributeActionMetadata,
							Regex:  "log.metadata\\.other\\..+",
						},
						{
							Action: config.OTLPAttributeActionMetadata,
							Names:  []string{"log.metadata"},
						},
					},
				},
			},
		},
		{
			desc: "tenant stream label",
			spec: lokiv1.LokiStackSpec{
				Limits: &lokiv1.LimitsSpec{
					Tenants: map[string]lokiv1.PerTenantLimitsTemplateSpec{
						"test-tenant": {
							OTLP: &lokiv1.TenantOTLPSpec{
								IgnoreGlobalStreamLabels: true,
								OTLPSpec: lokiv1.OTLPSpec{
									StreamLabels: &lokiv1.OTLPStreamLabelSpec{
										ResourceAttributes: []lokiv1.OTLPAttributeReference{
											{
												Name: "tenant.stream.label",
											},
										},
									},
								},
							},
						},
					},
				},
			},
			wantConfig: config.OTLPAttributeConfig{
				Tenants: map[string]*config.OTLPTenantAttributeConfig{
					"test-tenant": {
						IgnoreGlobalStreamLabels: true,
						ResourceAttributes: []config.OTLPAttribute{
							{
								Action: config.OTLPAttributeActionStreamLabel,
								Names:  []string{"tenant.stream.label"},
							},
						},
					},
				},
			},
		},
		{
			desc: "tenant stream label regex",
			spec: lokiv1.LokiStackSpec{
				Limits: &lokiv1.LimitsSpec{
					Tenants: map[string]lokiv1.PerTenantLimitsTemplateSpec{
						"test-tenant": {
							OTLP: &lokiv1.TenantOTLPSpec{
								OTLPSpec: lokiv1.OTLPSpec{
									StreamLabels: &lokiv1.OTLPStreamLabelSpec{
										ResourceAttributes: []lokiv1.OTLPAttributeReference{
											{
												Name:  "tenant\\.stream\\.label\\.regex\\..+",
												Regex: true,
											},
										},
									},
								},
							},
						},
					},
				},
			},
			wantConfig: config.OTLPAttributeConfig{
				Tenants: map[string]*config.OTLPTenantAttributeConfig{
					"test-tenant": {
						ResourceAttributes: []config.OTLPAttribute{
							{
								Action: config.OTLPAttributeActionStreamLabel,
								Regex:  "tenant\\.stream\\.label\\.regex\\..+",
							},
						},
					},
				},
			},
		},
		{
			desc: "tenant metadata",
			spec: lokiv1.LokiStackSpec{
				Limits: &lokiv1.LimitsSpec{
					Tenants: map[string]lokiv1.PerTenantLimitsTemplateSpec{
						"test-tenant": {
							OTLP: &lokiv1.TenantOTLPSpec{
								OTLPSpec: lokiv1.OTLPSpec{
									StructuredMetadata: &lokiv1.OTLPMetadataSpec{
										ResourceAttributes: []lokiv1.OTLPAttributeReference{
											{
												Name: "tenant.metadata",
											},
										},
									},
								},
							},
						},
					},
				},
			},
			wantConfig: config.OTLPAttributeConfig{
				Tenants: map[string]*config.OTLPTenantAttributeConfig{
					"test-tenant": {
						ResourceAttributes: []config.OTLPAttribute{
							{
								Action: config.OTLPAttributeActionMetadata,
								Names:  []string{"tenant.metadata"},
							},
						},
					},
				},
			},
		},
		{
			desc: "tenant combined",
			spec: lokiv1.LokiStackSpec{
				Limits: &lokiv1.LimitsSpec{
					Tenants: map[string]lokiv1.PerTenantLimitsTemplateSpec{
						"test-tenant": {
							OTLP: &lokiv1.TenantOTLPSpec{
								OTLPSpec: lokiv1.OTLPSpec{
									StreamLabels: &lokiv1.OTLPStreamLabelSpec{
										ResourceAttributes: []lokiv1.OTLPAttributeReference{
											{
												Name: "tenant.stream.label",
											},
											{
												Name:  `tenant\.stream\.label\.regex\..+`,
												Regex: true,
											},
										},
									},
									StructuredMetadata: &lokiv1.OTLPMetadataSpec{
										ResourceAttributes: []lokiv1.OTLPAttributeReference{
											{
												Name: "tenant.resource.metadata",
											},
											{
												Name:  `tenant\.resource.metadata\.other\..+`,
												Regex: true,
											},
										},
										ScopeAttributes: []lokiv1.OTLPAttributeReference{
											{
												Name: "tenant.scope.metadata",
											},
											{
												Name:  `tenant\.scope\.metadata\.other\..+`,
												Regex: true,
											},
										},
										LogAttributes: []lokiv1.OTLPAttributeReference{
											{
												Name: "tenant.log.metadata",
											},
											{
												Name:  `tenant\.log\.metadata\.other\..+`,
												Regex: true,
											},
										},
									},
								},
							},
						},
					},
				},
			},
			wantConfig: config.OTLPAttributeConfig{
				Tenants: map[string]*config.OTLPTenantAttributeConfig{
					"test-tenant": {
						ResourceAttributes: []config.OTLPAttribute{
							{
								Action: config.OTLPAttributeActionStreamLabel,
								Regex:  "tenant\\.stream\\.label\\.regex\\..+",
							},
							{
								Action: config.OTLPAttributeActionStreamLabel,
								Names:  []string{"tenant.stream.label"},
							},
							{
								Action: config.OTLPAttributeActionMetadata,
								Regex:  `tenant\.resource.metadata\.other\..+`,
							},
							{
								Action: config.OTLPAttributeActionMetadata,
								Names:  []string{"tenant.resource.metadata"},
							},
						},
						ScopeAttributes: []config.OTLPAttribute{
							{
								Action: config.OTLPAttributeActionMetadata,
								Regex:  `tenant\.scope\.metadata\.other\..+`,
							},
							{
								Action: config.OTLPAttributeActionMetadata,
								Names:  []string{"tenant.scope.metadata"},
							},
						},
						LogAttributes: []config.OTLPAttribute{
							{
								Action: config.OTLPAttributeActionMetadata,
								Regex:  `tenant\.log\.metadata\.other\..+`,
							},
							{
								Action: config.OTLPAttributeActionMetadata,
								Names:  []string{"tenant.log.metadata"},
							},
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
			wantConfig: sortAndDeduplicateOTLPAttributes(defaultOpenShiftLoggingAttributes(false)),
		},
		{
			desc: "openshift-logging defaults without recommended",
			spec: lokiv1.LokiStackSpec{
				Tenants: &lokiv1.TenantsSpec{
					Mode: lokiv1.OpenshiftLogging,
					Openshift: &lokiv1.OpenshiftTenantSpec{
						OTLP: &lokiv1.OpenshiftOTLPConfig{
							DisableRecommendedAttributes: true,
						},
					},
				},
			},
			wantConfig: sortAndDeduplicateOTLPAttributes(defaultOpenShiftLoggingAttributes(true)),
		},
		{
			desc: "openshift-logging defaults with additional custom attributes",
			spec: lokiv1.LokiStackSpec{
				Limits: &lokiv1.LimitsSpec{
					Global: &lokiv1.LimitsTemplateSpec{
						OTLP: &lokiv1.OTLPSpec{
							StreamLabels: &lokiv1.OTLPStreamLabelSpec{
								ResourceAttributes: []lokiv1.OTLPAttributeReference{
									{
										Name: "custom.stream.label",
									},
								},
							},
							StructuredMetadata: &lokiv1.OTLPMetadataSpec{
								LogAttributes: []lokiv1.OTLPAttributeReference{
									{
										Name: "custom.log.metadata",
									},
								},
							},
						},
					},
				},
				Tenants: &lokiv1.TenantsSpec{
					Mode: lokiv1.OpenshiftLogging,
					Openshift: &lokiv1.OpenshiftTenantSpec{
						OTLP: &lokiv1.OpenshiftOTLPConfig{
							DisableRecommendedAttributes: true,
						},
					},
				},
			},
			wantConfig: config.OTLPAttributeConfig{
				DefaultIndexLabels: []string{
					"custom.stream.label",
					"k8s.namespace.name",
					"kubernetes.namespace_name",
					"log_source",
					"log_type",
					"openshift.cluster.uid",
					"openshift.log.source",
					"openshift.log.type",
				},
				Global: &config.OTLPTenantAttributeConfig{
					LogAttributes: []config.OTLPAttribute{
						{
							Action: config.OTLPAttributeActionMetadata,
							Names:  []string{"custom.log.metadata"},
						},
					},
				},
			},
		},
		{
			desc: "openshift-logging defaults with de-duplication",
			spec: lokiv1.LokiStackSpec{
				Limits: &lokiv1.LimitsSpec{
					Global: &lokiv1.LimitsTemplateSpec{
						OTLP: &lokiv1.OTLPSpec{
							StreamLabels: &lokiv1.OTLPStreamLabelSpec{
								ResourceAttributes: []lokiv1.OTLPAttributeReference{
									{
										Name: "custom.stream.label",
									},
									{
										Name: "k8s.namespace.name",
									},
								},
							},
							StructuredMetadata: &lokiv1.OTLPMetadataSpec{
								LogAttributes: []lokiv1.OTLPAttributeReference{
									{
										Name: "custom.log.metadata",
									},
								},
							},
						},
					},
				},
				Tenants: &lokiv1.TenantsSpec{
					Mode: lokiv1.OpenshiftLogging,
					Openshift: &lokiv1.OpenshiftTenantSpec{
						OTLP: &lokiv1.OpenshiftOTLPConfig{
							DisableRecommendedAttributes: true,
						},
					},
				},
			},
			wantConfig: config.OTLPAttributeConfig{
				DefaultIndexLabels: []string{
					"custom.stream.label",
					"k8s.namespace.name",
					"kubernetes.namespace_name",
					"log_source",
					"log_type",
					"openshift.cluster.uid",
					"openshift.log.source",
					"openshift.log.type",
				},
				Global: &config.OTLPTenantAttributeConfig{
					LogAttributes: []config.OTLPAttribute{
						{
							Action: config.OTLPAttributeActionMetadata,
							Names:  []string{"custom.log.metadata"},
						},
					},
				},
			},
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
