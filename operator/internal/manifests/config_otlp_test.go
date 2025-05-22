package manifests

import (
	"testing"

	"github.com/stretchr/testify/assert"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests/internal/config"
	"github.com/grafana/loki/operator/internal/manifests/openshift/otlp"
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
				RemoveDefaultLabels: true,
				Global: &config.OTLPTenantAttributeConfig{
					ResourceAttributes: []config.OTLPAttribute{
						{
							Action: config.OTLPAttributeActionStreamLabel,
							Names: []string{
								"stream.label",
							},
						},
					},
				},
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
				RemoveDefaultLabels: true,
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
			desc: "global with drop",
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
							Drop: &lokiv1.OTLPMetadataSpec{
								ResourceAttributes: []lokiv1.OTLPAttributeReference{
									{
										Name: "drop.attribute",
									},
								},
							},
						},
					},
				},
			},
			wantConfig: config.OTLPAttributeConfig{
				RemoveDefaultLabels: true,
				Global: &config.OTLPTenantAttributeConfig{
					ResourceAttributes: []config.OTLPAttribute{
						{
							Action: config.OTLPAttributeActionDrop,
							Names: []string{
								"drop.attribute",
							},
						},
						{
							Action: config.OTLPAttributeActionStreamLabel,
							Names: []string{
								"stream.label",
							},
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
							OTLP: &lokiv1.OTLPSpec{
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
			wantConfig: config.OTLPAttributeConfig{
				RemoveDefaultLabels: true,
				Tenants: map[string]*config.OTLPTenantAttributeConfig{
					"test-tenant": {
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
							OTLP: &lokiv1.OTLPSpec{
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
			wantConfig: config.OTLPAttributeConfig{
				RemoveDefaultLabels: true,
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
			desc: "tenant with drop",
			spec: lokiv1.LokiStackSpec{
				Limits: &lokiv1.LimitsSpec{
					Tenants: map[string]lokiv1.PerTenantLimitsTemplateSpec{
						"test-tenant": {
							OTLP: &lokiv1.OTLPSpec{
								StreamLabels: &lokiv1.OTLPStreamLabelSpec{
									ResourceAttributes: []lokiv1.OTLPAttributeReference{
										{
											Name: "stream.label",
										},
									},
								},
								Drop: &lokiv1.OTLPMetadataSpec{
									ResourceAttributes: []lokiv1.OTLPAttributeReference{
										{
											Name: "drop.attribute",
										},
									},
								},
							},
						},
					},
				},
			},
			wantConfig: config.OTLPAttributeConfig{
				RemoveDefaultLabels: true,
				Tenants: map[string]*config.OTLPTenantAttributeConfig{
					"test-tenant": {
						ResourceAttributes: []config.OTLPAttribute{
							{
								Action: config.OTLPAttributeActionDrop,
								Names: []string{
									"drop.attribute",
								},
							},
							{
								Action: config.OTLPAttributeActionStreamLabel,
								Names: []string{
									"stream.label",
								},
							},
						},
					},
				},
			},
		},
		{
			desc: "global and tenant configuration with de-duplication",
			spec: lokiv1.LokiStackSpec{
				Limits: &lokiv1.LimitsSpec{
					Global: &lokiv1.LimitsTemplateSpec{
						OTLP: &lokiv1.OTLPSpec{
							StreamLabels: &lokiv1.OTLPStreamLabelSpec{
								ResourceAttributes: []lokiv1.OTLPAttributeReference{
									{
										Name: "global.stream.label",
									},
									{
										Name: "another.stream.label",
									},
								},
							},
						},
					},
					Tenants: map[string]lokiv1.PerTenantLimitsTemplateSpec{
						"test-tenant": {
							OTLP: &lokiv1.OTLPSpec{
								StreamLabels: &lokiv1.OTLPStreamLabelSpec{
									ResourceAttributes: []lokiv1.OTLPAttributeReference{
										{
											Name: "tenant.stream.label",
										},
										{
											Name: "another.stream.label",
										},
									},
								},
							},
						},
					},
				},
			},
			wantConfig: config.OTLPAttributeConfig{
				RemoveDefaultLabels: true,
				Global: &config.OTLPTenantAttributeConfig{
					ResourceAttributes: []config.OTLPAttribute{
						{
							Action: config.OTLPAttributeActionStreamLabel,
							Names: []string{
								"another.stream.label",
								"global.stream.label",
							},
						},
					},
				},
				Tenants: map[string]*config.OTLPTenantAttributeConfig{
					"test-tenant": {
						ResourceAttributes: []config.OTLPAttribute{
							{
								Action: config.OTLPAttributeActionStreamLabel,
								Names: []string{
									"another.stream.label",
									"global.stream.label",
									"tenant.stream.label",
								},
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
			wantConfig: config.OTLPAttributeConfig{
				RemoveDefaultLabels: true,
				Global: &config.OTLPTenantAttributeConfig{
					ResourceAttributes: []config.OTLPAttribute{
						{
							Action: config.OTLPAttributeActionStreamLabel,
							Names:  otlp.DefaultOTLPAttributes(false),
						},
					},
				},
			},
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
			wantConfig: config.OTLPAttributeConfig{
				RemoveDefaultLabels: true,
				Global: &config.OTLPTenantAttributeConfig{
					ResourceAttributes: []config.OTLPAttribute{
						{
							Action: config.OTLPAttributeActionStreamLabel,
							Names:  otlp.DefaultOTLPAttributes(true),
						},
					},
				},
			},
		},
		{
			desc: "openshift-logging defaults with drop",
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
							Drop: &lokiv1.OTLPMetadataSpec{
								LogAttributes: []lokiv1.OTLPAttributeReference{
									{
										Name: "custom.log.metadata",
									},
								},
							},
						},
					},
					Tenants: map[string]lokiv1.PerTenantLimitsTemplateSpec{
						"application": {
							OTLP: &lokiv1.OTLPSpec{
								StreamLabels: &lokiv1.OTLPStreamLabelSpec{
									ResourceAttributes: []lokiv1.OTLPAttributeReference{
										{
											Name: "custom.application.label",
										},
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
				RemoveDefaultLabels: true,
				Global: &config.OTLPTenantAttributeConfig{
					ResourceAttributes: []config.OTLPAttribute{
						{
							Action: config.OTLPAttributeActionStreamLabel,
							Names: []string{
								"custom.stream.label",
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
					LogAttributes: []config.OTLPAttribute{
						{
							Action: config.OTLPAttributeActionDrop,
							Names:  []string{"custom.log.metadata"},
						},
					},
				},
				Tenants: map[string]*config.OTLPTenantAttributeConfig{
					"application": {
						ResourceAttributes: []config.OTLPAttribute{
							{
								Action: config.OTLPAttributeActionStreamLabel,
								Names: []string{
									"custom.application.label",
									"custom.stream.label",
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
						LogAttributes: []config.OTLPAttribute{
							{
								Action: config.OTLPAttributeActionDrop,
								Names:  []string{"custom.log.metadata"},
							},
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
							Drop: &lokiv1.OTLPMetadataSpec{
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
				RemoveDefaultLabels: true,
				Global: &config.OTLPTenantAttributeConfig{
					ResourceAttributes: []config.OTLPAttribute{
						{
							Action: config.OTLPAttributeActionStreamLabel,
							Names: []string{
								"custom.stream.label",
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
					LogAttributes: []config.OTLPAttribute{
						{
							Action: config.OTLPAttributeActionDrop,
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

func TestSortOTLPAttributes(t *testing.T) {
	tt := []struct {
		desc      string
		attrs     []config.OTLPAttribute
		wantAttrs []config.OTLPAttribute
	}{
		{
			desc: "sort",
			attrs: []config.OTLPAttribute{
				{
					Action: config.OTLPAttributeActionStreamLabel,
					Names:  []string{"test.b"},
				},
				{
					Action: config.OTLPAttributeActionStreamLabel,
					Regex:  "test.regex.b",
				},
			},
			wantAttrs: []config.OTLPAttribute{
				{
					Action: config.OTLPAttributeActionStreamLabel,
					Names:  []string{"test.b"},
				},
				{
					Action: config.OTLPAttributeActionStreamLabel,
					Regex:  "test.regex.b",
				},
			},
		},
		{
			desc: "simple combine",
			attrs: []config.OTLPAttribute{
				{
					Action: config.OTLPAttributeActionStreamLabel,
					Names:  []string{"test.a"},
				},
				{
					Action: config.OTLPAttributeActionStreamLabel,
					Names:  []string{"test.b"},
				},
			},
			wantAttrs: []config.OTLPAttribute{
				{
					Action: config.OTLPAttributeActionStreamLabel,
					Names:  []string{"test.a", "test.b"},
				},
			},
		},
		{
			desc: "complex combine",
			attrs: []config.OTLPAttribute{
				{
					Action: config.OTLPAttributeActionStreamLabel,
					Names:  []string{"test.a"},
				},
				{
					Action: config.OTLPAttributeActionStreamLabel,
					Names:  []string{"test.c"},
				},
				{
					Action: config.OTLPAttributeActionStreamLabel,
					Names:  []string{"test.b"},
				},
				{
					Action: config.OTLPAttributeActionStreamLabel,
					Regex:  "test.regex.b",
				},
			},
			wantAttrs: []config.OTLPAttribute{
				{
					Action: config.OTLPAttributeActionStreamLabel,
					Names:  []string{"test.a", "test.b", "test.c"},
				},
				{
					Action: config.OTLPAttributeActionStreamLabel,
					Regex:  "test.regex.b",
				},
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			attrs := sortAndDeduplicateOTLPAttributes(tc.attrs)

			assert.Equal(t, tc.wantAttrs, attrs)
		})
	}
}
