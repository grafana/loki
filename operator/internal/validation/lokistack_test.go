package validation_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/validation"
)

var ltt = []struct {
	desc string
	spec lokiv1.LokiStack
	err  *apierrors.StatusError
}{
	{
		desc: "valid spec - no status",
		spec: lokiv1.LokiStack{
			Spec: lokiv1.LokiStackSpec{
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV11,
							EffectiveDate: "2020-10-11",
						},
						{
							Version:       lokiv1.ObjectStorageSchemaV12,
							EffectiveDate: "2020-10-13",
						},
					},
				},
			},
		},
	},
	{
		desc: "valid spec - with status",
		spec: lokiv1.LokiStack{
			Spec: lokiv1.LokiStackSpec{
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV11,
							EffectiveDate: "2020-10-11",
						},
						{
							Version:       lokiv1.ObjectStorageSchemaV12,
							EffectiveDate: "2020-10-13",
						},
					},
				},
			},
			Status: lokiv1.LokiStackStatus{
				Storage: lokiv1.LokiStackStorageStatus{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV11,
							EffectiveDate: "2020-10-11",
						},
						{
							Version:       lokiv1.ObjectStorageSchemaV12,
							EffectiveDate: "2020-10-13",
						},
					},
				},
			},
		},
	},
	{
		desc: "not unique schema effective dates",
		spec: lokiv1.LokiStack{
			Spec: lokiv1.LokiStackSpec{
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV11,
							EffectiveDate: "2020-10-11",
						},
						{
							Version:       lokiv1.ObjectStorageSchemaV12,
							EffectiveDate: "2020-10-11",
						},
					},
				},
			},
		},
		err: apierrors.NewInvalid(
			schema.GroupKind{Group: "loki.grafana.com", Kind: "LokiStack"},
			"testing-stack",
			field.ErrorList{
				field.Invalid(
					field.NewPath("spec").Child("storage").Child("schemas").Index(1).Child("effectiveDate"),
					"2020-10-11",
					lokiv1.ErrEffectiveDatesNotUnique.Error(),
				),
			},
		),
	},
	{
		desc: "schema effective dates bad format",
		spec: lokiv1.LokiStack{
			Spec: lokiv1.LokiStackSpec{
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV11,
							EffectiveDate: "2020/10/11",
						},
					},
				},
			},
		},
		err: apierrors.NewInvalid(
			schema.GroupKind{Group: "loki.grafana.com", Kind: "LokiStack"},
			"testing-stack",
			field.ErrorList{
				field.Invalid(
					field.NewPath("spec").Child("storage").Child("schemas").Index(0).Child("effectiveDate"),
					"2020/10/11",
					lokiv1.ErrParseEffectiveDates.Error(),
				),
			},
		),
	},
	{
		desc: "missing valid starting date",
		spec: lokiv1.LokiStack{
			Spec: lokiv1.LokiStackSpec{
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV11,
							EffectiveDate: "9000-10-10",
						},
					},
				},
			},
		},
		err: apierrors.NewInvalid(
			schema.GroupKind{Group: "loki.grafana.com", Kind: "LokiStack"},
			"testing-stack",
			field.ErrorList{
				field.Invalid(
					field.NewPath("spec").Child("storage").Child("schemas"),
					[]lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV11,
							EffectiveDate: "9000-10-10",
						},
					},
					lokiv1.ErrMissingValidStartDate.Error(),
				),
			},
		),
	},
	{
		desc: "retroactively adding schema",
		spec: lokiv1.LokiStack{
			Spec: lokiv1.LokiStackSpec{
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV11,
							EffectiveDate: "2020-10-11",
						},
						{
							Version:       lokiv1.ObjectStorageSchemaV12,
							EffectiveDate: "2020-10-14",
						},
					},
				},
			},
			Status: lokiv1.LokiStackStatus{
				Storage: lokiv1.LokiStackStorageStatus{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV11,
							EffectiveDate: "2020-10-11",
						},
					},
				},
			},
		},
		err: apierrors.NewInvalid(
			schema.GroupKind{Group: "loki.grafana.com", Kind: "LokiStack"},
			"testing-stack",
			field.ErrorList{
				field.Invalid(
					field.NewPath("spec").Child("storage").Child("schemas"),
					lokiv1.ObjectStorageSchema{
						Version:       lokiv1.ObjectStorageSchemaV12,
						EffectiveDate: "2020-10-14",
					},
					lokiv1.ErrSchemaRetroactivelyAdded.Error(),
				),
			},
		),
	},
	{
		desc: "retroactively removing schema",
		spec: lokiv1.LokiStack{
			Spec: lokiv1.LokiStackSpec{
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV11,
							EffectiveDate: "2020-10-11",
						},
					},
				},
			},
			Status: lokiv1.LokiStackStatus{
				Storage: lokiv1.LokiStackStorageStatus{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV11,
							EffectiveDate: "2020-10-11",
						},
						{
							Version:       lokiv1.ObjectStorageSchemaV12,
							EffectiveDate: "2020-10-14",
						},
					},
				},
			},
		},
		err: apierrors.NewInvalid(
			schema.GroupKind{Group: "loki.grafana.com", Kind: "LokiStack"},
			"testing-stack",
			field.ErrorList{
				field.Invalid(
					field.NewPath("spec").Child("storage").Child("schemas"),
					[]lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV11,
							EffectiveDate: "2020-10-11",
						},
					},
					lokiv1.ErrSchemaRetroactivelyRemoved.Error(),
				),
			},
		),
	},
	{
		desc: "retroactively changing schema",
		spec: lokiv1.LokiStack{
			Spec: lokiv1.LokiStackSpec{
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV12,
							EffectiveDate: "2020-10-11",
						},
					},
				},
			},
			Status: lokiv1.LokiStackStatus{
				Storage: lokiv1.LokiStackStorageStatus{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV11,
							EffectiveDate: "2020-10-11",
						},
					},
				},
			},
		},
		err: apierrors.NewInvalid(
			schema.GroupKind{Group: "loki.grafana.com", Kind: "LokiStack"},
			"testing-stack",
			field.ErrorList{
				field.Invalid(
					field.NewPath("spec").Child("storage").Child("schemas"),
					lokiv1.ObjectStorageSchema{
						Version:       lokiv1.ObjectStorageSchemaV12,
						EffectiveDate: "2020-10-11",
					},
					lokiv1.ErrSchemaRetroactivelyChanged.Error(),
				),
			},
		),
	},
	{
		desc: "valid replication zones",
		spec: lokiv1.LokiStack{
			Spec: lokiv1.LokiStackSpec{
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV12,
							EffectiveDate: "2020-10-11",
						},
					},
				},
				Replication: &lokiv1.ReplicationSpec{
					Zones: []lokiv1.ZoneSpec{
						{
							TopologyKey: "zone",
						},
					},
					Factor: 1,
				},
			},
		},
	},
	{
		desc: "using both replication and replicationFactor",
		spec: lokiv1.LokiStack{
			Spec: lokiv1.LokiStackSpec{
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV12,
							EffectiveDate: "2020-10-11",
						},
					},
				},
				ReplicationFactor: 2,
				Replication: &lokiv1.ReplicationSpec{
					Zones: []lokiv1.ZoneSpec{
						{
							TopologyKey: "zone",
						},
						{
							TopologyKey: "region",
						},
						{
							TopologyKey: "planet",
						},
					},
					Factor: 1,
				},
			},
		},
		err: apierrors.NewInvalid(
			schema.GroupKind{Group: "loki.grafana.com", Kind: "LokiStack"},
			"testing-stack",
			field.ErrorList{
				field.Invalid(
					field.NewPath("spec", "replicationFactor"),
					2,
					lokiv1.ErrReplicationSpecConflict.Error(),
				),
			},
		),
	},
	{
		desc: "using default InstanceAddrType and enableIPv6",
		spec: lokiv1.LokiStack{
			Spec: lokiv1.LokiStackSpec{
				HashRing: &lokiv1.HashRingSpec{
					Type: lokiv1.HashRingMemberList,
					MemberList: &lokiv1.MemberListSpec{
						EnableIPv6:       true,
						InstanceAddrType: lokiv1.InstanceAddrDefault,
					},
				},
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV12,
							EffectiveDate: "2020-10-11",
						},
					},
				},
				Replication: &lokiv1.ReplicationSpec{
					Zones: []lokiv1.ZoneSpec{
						{
							TopologyKey: "zone",
						},
					},
					Factor: 1,
				},
			},
		},
		err: apierrors.NewInvalid(
			schema.GroupKind{Group: "loki.grafana.com", Kind: "LokiStack"},
			"testing-stack",
			field.ErrorList{
				field.Invalid(
					field.NewPath("spec", "hashRing", "memberlist", "instanceAddrType"),
					lokiv1.InstanceAddrDefault,
					lokiv1.ErrIPv6InstanceAddrTypeNotAllowed.Error(),
				),
			},
		),
	},
	{
		desc: "lokistack with custom OTLP configuration with a global stream label",
		spec: lokiv1.LokiStack{
			Spec: lokiv1.LokiStackSpec{
				Limits: &lokiv1.LimitsSpec{
					Global: &lokiv1.LimitsTemplateSpec{
						OTLP: &lokiv1.OTLPSpec{
							StreamLabels: &lokiv1.OTLPStreamLabelSpec{
								ResourceAttributes: []lokiv1.OTLPAttributeReference{
									{
										Name: "global.stream.label",
									},
								},
							},
						},
					},
				},
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV13,
							EffectiveDate: "2024-10-22",
						},
					},
				},
				Tenants: &lokiv1.TenantsSpec{
					Mode: lokiv1.Static,
				},
			},
		},
		err: nil,
	},
	{
		desc: "lokistack with custom OTLP configuration with a global stream label and a tenant with no stream label",
		spec: lokiv1.LokiStack{
			Spec: lokiv1.LokiStackSpec{
				Limits: &lokiv1.LimitsSpec{
					Global: &lokiv1.LimitsTemplateSpec{
						OTLP: &lokiv1.OTLPSpec{
							StreamLabels: &lokiv1.OTLPStreamLabelSpec{
								ResourceAttributes: []lokiv1.OTLPAttributeReference{
									{
										Name: "global.stream.label",
									},
								},
							},
						},
					},
					Tenants: map[string]lokiv1.PerTenantLimitsTemplateSpec{
						"test-tenant": {
							OTLP: &lokiv1.OTLPSpec{},
						},
					},
				},
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV13,
							EffectiveDate: "2024-10-22",
						},
					},
				},
				Tenants: &lokiv1.TenantsSpec{
					Mode: lokiv1.Static,
					Authentication: []lokiv1.AuthenticationSpec{
						{
							TenantName: "test-tenant",
						},
					},
				},
			},
		},
		err: nil,
	},
	{
		desc: "lokistack with custom OTLP configuration with no global stream label and a tenant with a stream label",
		spec: lokiv1.LokiStack{
			Spec: lokiv1.LokiStackSpec{
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
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV13,
							EffectiveDate: "2024-10-22",
						},
					},
				},
				Tenants: &lokiv1.TenantsSpec{
					Mode: lokiv1.Static,
					Authentication: []lokiv1.AuthenticationSpec{
						{
							TenantName: "test-tenant",
						},
					},
				},
			},
		},
		err: nil,
	},
	{
		desc: "lokistack with custom OTLP configuration missing a global stream label",
		spec: lokiv1.LokiStack{
			Spec: lokiv1.LokiStackSpec{
				Limits: &lokiv1.LimitsSpec{
					Global: &lokiv1.LimitsTemplateSpec{
						OTLP: &lokiv1.OTLPSpec{},
					},
				},
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV13,
							EffectiveDate: "2024-10-22",
						},
					},
				},
				Tenants: &lokiv1.TenantsSpec{
					Mode: lokiv1.Static,
				},
			},
		},
		err: apierrors.NewInvalid(
			schema.GroupKind{Group: "loki.grafana.com", Kind: "LokiStack"},
			"testing-stack",
			field.ErrorList{
				field.Invalid(
					field.NewPath("spec", "limits", "global", "otlp", "streamLabels", "resourceAttributes"),
					nil,
					lokiv1.ErrOTLPGlobalNoStreamLabel.Error(),
				),
			},
		),
	},
	{
		desc: "lokistack with custom OTLP configuration missing a tenant",
		spec: lokiv1.LokiStack{
			Spec: lokiv1.LokiStackSpec{
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
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV13,
							EffectiveDate: "2024-10-22",
						},
					},
				},
				Tenants: &lokiv1.TenantsSpec{
					Mode: lokiv1.Static,
					Authentication: []lokiv1.AuthenticationSpec{
						{
							TenantName: "test-tenant",
						},
						{
							TenantName: "second-tenant",
						},
					},
				},
			},
		},
		err: apierrors.NewInvalid(
			schema.GroupKind{Group: "loki.grafana.com", Kind: "LokiStack"},
			"testing-stack",
			field.ErrorList{
				field.Invalid(
					field.NewPath("spec", "limits", "tenants", "second-tenant", "otlp"),
					nil,
					lokiv1.ErrOTLPTenantMissing.Error(),
				),
			},
		),
	},
	{
		desc: "lokistack with custom OTLP configuration with a tenant without stream label",
		spec: lokiv1.LokiStack{
			Spec: lokiv1.LokiStackSpec{
				Limits: &lokiv1.LimitsSpec{
					Tenants: map[string]lokiv1.PerTenantLimitsTemplateSpec{
						"test-tenant": {
							OTLP: &lokiv1.OTLPSpec{},
						},
					},
				},
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV13,
							EffectiveDate: "2024-10-22",
						},
					},
				},
				Tenants: &lokiv1.TenantsSpec{
					Mode: lokiv1.Static,
					Authentication: []lokiv1.AuthenticationSpec{
						{
							TenantName: "test-tenant",
						},
					},
				},
			},
		},
		err: apierrors.NewInvalid(
			schema.GroupKind{Group: "loki.grafana.com", Kind: "LokiStack"},
			"testing-stack",
			field.ErrorList{
				field.Invalid(
					field.NewPath("spec", "limits", "tenants", "test-tenant", "otlp", "streamLabels", "resourceAttributes"),
					nil,
					lokiv1.ErrOTLPTenantNoStreamLabel.Error(),
				),
			},
		),
	},
	{
		desc: "lokistack with custom OTLP configuration listing an attribute as both stream-label and drop",
		spec: lokiv1.LokiStack{
			Spec: lokiv1.LokiStackSpec{
				Limits: &lokiv1.LimitsSpec{
					Global: &lokiv1.LimitsTemplateSpec{
						OTLP: &lokiv1.OTLPSpec{
							StreamLabels: &lokiv1.OTLPStreamLabelSpec{
								ResourceAttributes: []lokiv1.OTLPAttributeReference{
									{
										Name: "global.stream.label",
									},
								},
							},
							Drop: &lokiv1.OTLPMetadataSpec{
								ResourceAttributes: []lokiv1.OTLPAttributeReference{
									{
										Name: "global.stream.label",
									},
								},
							},
						},
					},
				},
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV13,
							EffectiveDate: "2024-10-22",
						},
					},
				},
				Tenants: &lokiv1.TenantsSpec{
					Mode: lokiv1.Static,
				},
			},
		},
		err: apierrors.NewInvalid(
			schema.GroupKind{Group: "loki.grafana.com", Kind: "LokiStack"},
			"testing-stack",
			field.ErrorList{
				field.Invalid(
					field.NewPath("spec", "limits", "global", "otlp", "drop", "resourceAttributes").Index(0),
					"global.stream.label",
					lokiv1.ErrOTLPInvalidDrop.Error(),
				),
			},
		),
	},
}

func TestLokiStackValidationWebhook_ValidateCreate(t *testing.T) {
	for _, tc := range ltt {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			l := &lokiv1.LokiStack{
				ObjectMeta: metav1.ObjectMeta{
					Name: "testing-stack",
				},
				Spec: tc.spec.Spec,
			}
			ctx := context.Background()

			v := &validation.LokiStackValidator{}
			_, err := v.ValidateCreate(ctx, l)
			if err != nil {
				require.Equal(t, tc.err, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestLokiStackValidationWebhook_ValidateUpdate(t *testing.T) {
	for _, tc := range ltt {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			l := &lokiv1.LokiStack{
				ObjectMeta: metav1.ObjectMeta{
					Name: "testing-stack",
				},
				Spec: tc.spec.Spec,
			}
			ctx := context.Background()

			v := &validation.LokiStackValidator{}
			_, err := v.ValidateUpdate(ctx, &lokiv1.LokiStack{}, l)
			if err != nil {
				require.Equal(t, tc.err, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
