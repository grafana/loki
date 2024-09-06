package validation_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
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
		desc: "enabling global limits OTLP IgnoreDefaults without resource attributes",
		spec: lokiv1.LokiStack{
			Spec: lokiv1.LokiStackSpec{
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV13,
							EffectiveDate: "2020-10-11",
						},
					},
				},
				Limits: &lokiv1.LimitsSpec{
					Global: &lokiv1.LimitsTemplateSpec{
						OTLP: &lokiv1.GlobalOTLPSpec{
							OTLPSpec: lokiv1.OTLPSpec{
								ResourceAttributes: &lokiv1.OTLPResourceAttributesSpec{
									IgnoreDefaults: true,
								},
							},
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
					field.NewPath("spec", "limits", "global", "otlp", "resourceAttributes"),
					[]lokiv1.OTLPAttributesSpec{},
					lokiv1.ErrOTLPResourceAttributesEmptyNotAllowed.Error(),
				),
			},
		),
	},
	{
		desc: "enabling global limits OTLP IgnoreDefaults without index label action for resource attributes",
		spec: lokiv1.LokiStack{
			Spec: lokiv1.LokiStackSpec{
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV13,
							EffectiveDate: "2020-10-11",
						},
					},
				},
				Limits: &lokiv1.LimitsSpec{
					Global: &lokiv1.LimitsTemplateSpec{
						OTLP: &lokiv1.GlobalOTLPSpec{
							OTLPSpec: lokiv1.OTLPSpec{
								ResourceAttributes: &lokiv1.OTLPResourceAttributesSpec{
									IgnoreDefaults: true,
									Attributes: []lokiv1.OTLPResourceAttributesConfigSpec{
										{
											Action:     lokiv1.OTLPAttributeActionStructuredMetadata,
											Attributes: []string{"test"},
										},
									},
								},
							},
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
					field.NewPath("spec", "limits", "global", "otlp", "resourceAttributes"),
					[]lokiv1.OTLPResourceAttributesConfigSpec{
						{
							Action:     lokiv1.OTLPAttributeActionStructuredMetadata,
							Attributes: []string{"test"},
						},
					},
					lokiv1.ErrOTLPResourceAttributesIndexLabelActionMissing.Error(),
				),
			},
		),
	},
	{
		desc: "enabling global limits OTLP IgnoreDefaults with invalid resource attributes config",
		spec: lokiv1.LokiStack{
			Spec: lokiv1.LokiStackSpec{
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV13,
							EffectiveDate: "2020-10-11",
						},
					},
				},
				Limits: &lokiv1.LimitsSpec{
					Global: &lokiv1.LimitsTemplateSpec{
						OTLP: &lokiv1.GlobalOTLPSpec{
							OTLPSpec: lokiv1.OTLPSpec{
								ResourceAttributes: &lokiv1.OTLPResourceAttributesSpec{
									IgnoreDefaults: true,
									Attributes: []lokiv1.OTLPResourceAttributesConfigSpec{
										{
											Action: lokiv1.OTLPAttributeActionStructuredMetadata,
										},
									},
								},
							},
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
					field.NewPath("spec", "limits", "global", "otlp", "resourceAttributes"),
					[]lokiv1.OTLPResourceAttributesConfigSpec{
						{
							Action: lokiv1.OTLPAttributeActionStructuredMetadata,
						},
					},
					lokiv1.ErrOTLPResourceAttributesIndexLabelActionMissing.Error(),
				),
				field.Invalid(
					field.NewPath("spec", "limits", "global", "otlp", "resourceAttributes").Index(0),
					[]string{},
					lokiv1.ErrOTLPAttributesSpecInvalid.Error(),
				),
			},
		),
	},
	{
		desc: "enabling global limits OTLP with invalid resource attributes config",
		spec: lokiv1.LokiStack{
			Spec: lokiv1.LokiStackSpec{
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV13,
							EffectiveDate: "2020-10-11",
						},
					},
				},
				Limits: &lokiv1.LimitsSpec{
					Global: &lokiv1.LimitsTemplateSpec{
						OTLP: &lokiv1.GlobalOTLPSpec{
							OTLPSpec: lokiv1.OTLPSpec{
								ResourceAttributes: &lokiv1.OTLPResourceAttributesSpec{
									IgnoreDefaults: true,
									Attributes: []lokiv1.OTLPResourceAttributesConfigSpec{
										{
											Action: lokiv1.OTLPAttributeActionIndexLabel,
										},
									},
								},
							},
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
					field.NewPath("spec", "limits", "global", "otlp", "resourceAttributes").Index(0),
					[]string{},
					lokiv1.ErrOTLPAttributesSpecInvalid.Error(),
				),
			},
		),
	},
	{
		desc: "invalid global OTLP scope attribute specs",
		spec: lokiv1.LokiStack{
			Spec: lokiv1.LokiStackSpec{
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV13,
							EffectiveDate: "2020-10-11",
						},
					},
				},
				Limits: &lokiv1.LimitsSpec{
					Global: &lokiv1.LimitsTemplateSpec{
						OTLP: &lokiv1.GlobalOTLPSpec{
							OTLPSpec: lokiv1.OTLPSpec{
								ScopeAttributes: []lokiv1.OTLPAttributesSpec{
									{
										Action: lokiv1.OTLPAttributeActionIndexLabel,
									},
									{
										Action: lokiv1.OTLPAttributeActionStructuredMetadata,
									},
								},
							},
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
					field.NewPath("spec", "limits", "global", "otlp", "scopeAttributes").Index(0),
					[]string{},
					lokiv1.ErrOTLPAttributesSpecInvalid.Error(),
				),
				field.Invalid(
					field.NewPath("spec", "limits", "global", "otlp", "scopeAttributes").Index(1),
					[]string{},
					lokiv1.ErrOTLPAttributesSpecInvalid.Error(),
				),
			},
		),
	},
	{
		desc: "invalid global OTLP log attribute specs",
		spec: lokiv1.LokiStack{
			Spec: lokiv1.LokiStackSpec{
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV13,
							EffectiveDate: "2020-10-11",
						},
					},
				},
				Limits: &lokiv1.LimitsSpec{
					Global: &lokiv1.LimitsTemplateSpec{
						OTLP: &lokiv1.GlobalOTLPSpec{
							OTLPSpec: lokiv1.OTLPSpec{
								LogAttributes: []lokiv1.OTLPAttributesSpec{
									{
										Action: lokiv1.OTLPAttributeActionIndexLabel,
									},
									{
										Action: lokiv1.OTLPAttributeActionStructuredMetadata,
									},
								},
							},
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
					field.NewPath("spec", "limits", "global", "otlp", "logAttributes").Index(0),
					[]string{},
					lokiv1.ErrOTLPAttributesSpecInvalid.Error(),
				),
				field.Invalid(
					field.NewPath("spec", "limits", "global", "otlp", "logAttributes").Index(1),
					[]string{},
					lokiv1.ErrOTLPAttributesSpecInvalid.Error(),
				),
			},
		),
	},
	{
		desc: "enabling per-tenant limits OTLP IgnoreDefaults without resource attributes",
		spec: lokiv1.LokiStack{
			Spec: lokiv1.LokiStackSpec{
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV13,
							EffectiveDate: "2020-10-11",
						},
					},
				},
				Limits: &lokiv1.LimitsSpec{
					Tenants: map[string]lokiv1.PerTenantLimitsTemplateSpec{
						"tenant-a": {
							OTLP: &lokiv1.OTLPSpec{
								ResourceAttributes: &lokiv1.OTLPResourceAttributesSpec{
									IgnoreDefaults: true,
								},
							},
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
					field.NewPath("spec", "limits", "tenants").Key("tenant-a").Child("otlp", "resourceAttributes"),
					[]lokiv1.OTLPAttributesSpec{},
					lokiv1.ErrOTLPResourceAttributesEmptyNotAllowed.Error(),
				),
			},
		),
	},
}

func TestLokiStackValidationWebhook_ValidateCreate(t *testing.T) {
	for _, tc := range ltt {
		tc := tc
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
		tc := tc
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
