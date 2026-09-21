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
					field.NewPath("spec").Child("storage").Child("schemas").Index(1),
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
		desc: "removing schema with no retention configured - should fail",
		spec: lokiv1.LokiStack{
			Spec: lokiv1.LokiStackSpec{
				// No limits configured at all
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
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
							Version:       lokiv1.ObjectStorageSchemaV12,
							EffectiveDate: "2020-10-14",
						},
					},
					lokiv1.ErrSchemaNotExpired.Error(),
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
					lokiv1.ErrSchemaNotExpired.Error(),
				),
			},
		),
	},
	{
		desc: "removing expired schema - should succeed",
		spec: lokiv1.LokiStack{
			Spec: lokiv1.LokiStackSpec{
				Limits: &lokiv1.LimitsSpec{
					Global: &lokiv1.LimitsTemplateSpec{
						Retention: &lokiv1.RetentionLimitSpec{
							Days: 30,
						},
					},
				},
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
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
						{
							Version:       lokiv1.ObjectStorageSchemaV12,
							EffectiveDate: "2020-10-14",
						},
					},
				},
			},
		},
		err: nil,
	},
	{
		desc: "removing schema before retention expires - should fail",
		spec: lokiv1.LokiStack{
			Spec: lokiv1.LokiStackSpec{
				Limits: &lokiv1.LimitsSpec{
					Global: &lokiv1.LimitsTemplateSpec{
						Retention: &lokiv1.RetentionLimitSpec{
							Days: 3650, // 10 years retention - schema won't expire
						},
					},
				},
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
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
							Version:       lokiv1.ObjectStorageSchemaV12,
							EffectiveDate: "2020-10-14",
						},
					},
					lokiv1.ErrSchemaNotExpired.Error(),
				),
			},
		),
	},
	{
		desc: "removing schema with tenant retention longer than global - should fail",
		spec: lokiv1.LokiStack{
			Spec: lokiv1.LokiStackSpec{
				Limits: &lokiv1.LimitsSpec{
					Global: &lokiv1.LimitsTemplateSpec{
						Retention: &lokiv1.RetentionLimitSpec{
							Days: 30, // Global: 30 days
						},
					},
					Tenants: map[string]lokiv1.PerTenantLimitsTemplateSpec{
						"tenant-a": {
							Retention: &lokiv1.RetentionLimitSpec{
								Days: 3650, // Tenant has 10 year retention - should block removal
							},
						},
					},
				},
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
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
							Version:       lokiv1.ObjectStorageSchemaV12,
							EffectiveDate: "2020-10-14",
						},
					},
					lokiv1.ErrSchemaNotExpired.Error(),
				),
			},
		),
	},
	{
		desc: "removing schema with tenant retention honored - should succeed after tenant retention expires",
		spec: lokiv1.LokiStack{
			Spec: lokiv1.LokiStackSpec{
				Limits: &lokiv1.LimitsSpec{
					Global: &lokiv1.LimitsTemplateSpec{
						Retention: &lokiv1.RetentionLimitSpec{
							Days: 30, // Global: 30 days
						},
					},
					Tenants: map[string]lokiv1.PerTenantLimitsTemplateSpec{
						"tenant-a": {
							Retention: &lokiv1.RetentionLimitSpec{
								Days: 45, // expired for 2020 schema
							},
						},
					},
				},
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
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
						{
							Version:       lokiv1.ObjectStorageSchemaV12,
							EffectiveDate: "2020-10-14",
						},
					},
				},
			},
		},
		err: nil,
	},
	{
		desc: "removing schema when tenant has no retention and no global - should fail",
		spec: lokiv1.LokiStack{
			Spec: lokiv1.LokiStackSpec{
				Limits: &lokiv1.LimitsSpec{
					// No global retention
					Tenants: map[string]lokiv1.PerTenantLimitsTemplateSpec{
						"tenant-a": {
							Retention: &lokiv1.RetentionLimitSpec{
								Days: 30,
							},
						},
						"tenant-b": {
							// no retention config = infinite retention
						},
					},
				},
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
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
							Version:       lokiv1.ObjectStorageSchemaV12,
							EffectiveDate: "2020-10-14",
						},
					},
					lokiv1.ErrSchemaNotExpired.Error(),
				),
			},
		),
	},
	{
		desc: "removing schema with no global retention - should fail (unlisted tenants have infinite retention)",
		spec: lokiv1.LokiStack{
			Spec: lokiv1.LokiStackSpec{
				Limits: &lokiv1.LimitsSpec{
					// No global retention - unlisted tenants have infinite retention
					Tenants: map[string]lokiv1.PerTenantLimitsTemplateSpec{
						"tenant-a": {
							Retention: &lokiv1.RetentionLimitSpec{
								Days: 30,
							},
						},
						// tenant-b and other unlisted tenants have infinite retention
					},
				},
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
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
							Version:       lokiv1.ObjectStorageSchemaV12,
							EffectiveDate: "2020-10-14",
						},
					},
					lokiv1.ErrSchemaNotExpired.Error(),
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
					field.NewPath("spec").Child("storage").Child("schemas").Index(0),
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
				ReplicationFactor: 2, //nolint:staticcheck
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
				Spec:   tc.spec.Spec,
				Status: tc.spec.Status,
			}
			ctx := context.Background()

			v := &validation.LokiStackValidator{}
			_, err := v.ValidateCreate(ctx, l)
			if tc.err != nil {
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
				Spec:   tc.spec.Spec,
				Status: tc.spec.Status,
			}
			ctx := context.Background()

			v := &validation.LokiStackValidator{}
			_, err := v.ValidateUpdate(ctx, &lokiv1.LokiStack{}, l)
			if tc.err != nil {
				require.Equal(t, tc.err, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestLokiStackValidationWebhook_SchemaRemovalWarning(t *testing.T) {
	t.Run("warning returned when successful schema removal", func(t *testing.T) {
		l := &lokiv1.LokiStack{
			ObjectMeta: metav1.ObjectMeta{
				Name: "testing-stack",
			},
			Spec: lokiv1.LokiStackSpec{
				Size: lokiv1.SizeOneXExtraSmall,
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV13,
							EffectiveDate: "2024-10-22",
						},
					},
				},
				Limits: &lokiv1.LimitsSpec{
					Global: &lokiv1.LimitsTemplateSpec{
						Retention: &lokiv1.RetentionLimitSpec{
							Days: 30,
						},
					},
				},
			},
			Status: lokiv1.LokiStackStatus{
				Storage: lokiv1.LokiStackStorageStatus{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV12,
							EffectiveDate: "2020-10-11",
						},
						{
							Version:       lokiv1.ObjectStorageSchemaV13,
							EffectiveDate: "2024-10-22",
						},
					},
				},
			},
		}
		ctx := context.Background()

		v := &validation.LokiStackValidator{}
		warnings, err := v.ValidateUpdate(ctx, &lokiv1.LokiStack{}, l)

		require.Len(t, warnings, 1)
		require.Equal(t, lokiv1.WarnSchemaRemoval, warnings[0])
		require.NoError(t, err)
	})

	t.Run("no warning when schema removal fails validation", func(t *testing.T) {
		l := &lokiv1.LokiStack{
			ObjectMeta: metav1.ObjectMeta{
				Name: "testing-stack",
			},
			Spec: lokiv1.LokiStackSpec{
				Size: lokiv1.SizeOneXExtraSmall,
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV13,
							EffectiveDate: "2024-10-22",
						},
					},
				},
				// No global retention - will fail validation
			},
			Status: lokiv1.LokiStackStatus{
				Storage: lokiv1.LokiStackStorageStatus{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV12,
							EffectiveDate: "2020-10-11",
						},
						{
							Version:       lokiv1.ObjectStorageSchemaV13,
							EffectiveDate: "2024-10-22",
						},
					},
				},
			},
		}
		ctx := context.Background()

		v := &validation.LokiStackValidator{}
		warnings, err := v.ValidateUpdate(ctx, &lokiv1.LokiStack{}, l)

		require.Len(t, warnings, 0)
		require.Error(t, err)
	})

	t.Run("no warning when no schema removal", func(t *testing.T) {
		l := &lokiv1.LokiStack{
			ObjectMeta: metav1.ObjectMeta{
				Name: "testing-stack",
			},
			Spec: lokiv1.LokiStackSpec{
				Size: lokiv1.SizeOneXExtraSmall,
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV13,
							EffectiveDate: "2024-10-22",
						},
					},
				},
			},
			Status: lokiv1.LokiStackStatus{
				Storage: lokiv1.LokiStackStorageStatus{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV13,
							EffectiveDate: "2024-10-22",
						},
					},
				},
			},
		}
		ctx := context.Background()

		v := &validation.LokiStackValidator{}
		warnings, err := v.ValidateUpdate(ctx, &lokiv1.LokiStack{}, l)

		require.Len(t, warnings, 0)
		require.NoError(t, err)
	})
}

func TestLokiStackValidationWebhook_RetentionUpdateWarning(t *testing.T) {
	t.Run("warning when retention changes", func(t *testing.T) {
		currentStack := &lokiv1.LokiStack{
			ObjectMeta: metav1.ObjectMeta{
				Name: "testing-stack",
			},
			Spec: lokiv1.LokiStackSpec{
				Size: lokiv1.SizeOneXExtraSmall,
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV13,
							EffectiveDate: "2024-10-22",
						},
					},
				},
				Limits: &lokiv1.LimitsSpec{
					Global: &lokiv1.LimitsTemplateSpec{
						Retention: &lokiv1.RetentionLimitSpec{
							Days: 30,
						},
					},
				},
			},
		}

		newStack := &lokiv1.LokiStack{
			ObjectMeta: metav1.ObjectMeta{
				Name: "testing-stack",
			},
			Spec: lokiv1.LokiStackSpec{
				Size: lokiv1.SizeOneXExtraSmall,
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV13,
							EffectiveDate: "2024-10-22",
						},
					},
				},
				Limits: &lokiv1.LimitsSpec{
					Global: &lokiv1.LimitsTemplateSpec{
						Retention: &lokiv1.RetentionLimitSpec{
							Days: 60, // Changed from 30 to 60
						},
					},
				},
			},
		}
		ctx := context.Background()

		v := &validation.LokiStackValidator{}
		warnings, err := v.ValidateUpdate(ctx, currentStack, newStack)

		require.Len(t, warnings, 1)
		require.Equal(t, lokiv1.WarnRetentionUpdate, warnings[0])
		require.NoError(t, err)
	})

	t.Run("no warning when retention unchanged", func(t *testing.T) {
		currentStack := &lokiv1.LokiStack{
			ObjectMeta: metav1.ObjectMeta{
				Name: "testing-stack",
			},
			Spec: lokiv1.LokiStackSpec{
				Size: lokiv1.SizeOneXExtraSmall,
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV13,
							EffectiveDate: "2024-10-22",
						},
					},
				},
				Limits: &lokiv1.LimitsSpec{
					Global: &lokiv1.LimitsTemplateSpec{
						Retention: &lokiv1.RetentionLimitSpec{
							Days: 30,
						},
					},
				},
			},
		}

		newStack := &lokiv1.LokiStack{
			ObjectMeta: metav1.ObjectMeta{
				Name: "testing-stack",
			},
			Spec: lokiv1.LokiStackSpec{
				Size: lokiv1.SizeOneXExtraSmall,
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV13,
							EffectiveDate: "2024-10-22",
						},
					},
				},
				Limits: &lokiv1.LimitsSpec{
					Global: &lokiv1.LimitsTemplateSpec{
						Retention: &lokiv1.RetentionLimitSpec{
							Days: 30, // Same as before
						},
					},
				},
			},
		}
		ctx := context.Background()

		v := &validation.LokiStackValidator{}
		warnings, err := v.ValidateUpdate(ctx, currentStack, newStack)

		require.Len(t, warnings, 0)
		require.NoError(t, err)
	})

	t.Run("warning when tenant retention changes", func(t *testing.T) {
		currentStack := &lokiv1.LokiStack{
			ObjectMeta: metav1.ObjectMeta{
				Name: "testing-stack",
			},
			Spec: lokiv1.LokiStackSpec{
				Size: lokiv1.SizeOneXExtraSmall,
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV13,
							EffectiveDate: "2024-10-22",
						},
					},
				},
				Limits: &lokiv1.LimitsSpec{
					Global: &lokiv1.LimitsTemplateSpec{
						Retention: &lokiv1.RetentionLimitSpec{
							Days: 30,
						},
					},
					Tenants: map[string]lokiv1.PerTenantLimitsTemplateSpec{
						"tenant-a": {
							Retention: &lokiv1.RetentionLimitSpec{
								Days: 40,
							},
						},
					},
				},
			},
		}

		newStack := &lokiv1.LokiStack{
			ObjectMeta: metav1.ObjectMeta{
				Name: "testing-stack",
			},
			Spec: lokiv1.LokiStackSpec{
				Size: lokiv1.SizeOneXExtraSmall,
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV13,
							EffectiveDate: "2024-10-22",
						},
					},
				},
				Limits: &lokiv1.LimitsSpec{
					Global: &lokiv1.LimitsTemplateSpec{
						Retention: &lokiv1.RetentionLimitSpec{
							Days: 30,
						},
					},
					Tenants: map[string]lokiv1.PerTenantLimitsTemplateSpec{
						"tenant-a": {
							Retention: &lokiv1.RetentionLimitSpec{
								Days: 50, // Changed from 40 to 50
							},
						},
					},
				},
			},
		}
		ctx := context.Background()

		v := &validation.LokiStackValidator{}
		warnings, err := v.ValidateUpdate(ctx, currentStack, newStack)

		require.Len(t, warnings, 1)
		require.Equal(t, lokiv1.WarnRetentionUpdate, warnings[0])
		require.NoError(t, err)
	})
}

func TestLokiStackValidationWebhook_SimultaneousSchemaRetentionChange(t *testing.T) {
	t.Run("error when both schemas and retention change", func(t *testing.T) {
		currentStack := &lokiv1.LokiStack{
			ObjectMeta: metav1.ObjectMeta{
				Name: "testing-stack",
			},
			Spec: lokiv1.LokiStackSpec{
				Size: lokiv1.SizeOneXExtraSmall,
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV12,
							EffectiveDate: "2020-10-11",
						},
						{
							Version:       lokiv1.ObjectStorageSchemaV13,
							EffectiveDate: "2024-10-22",
						},
					},
				},
				Limits: &lokiv1.LimitsSpec{
					Global: &lokiv1.LimitsTemplateSpec{
						Retention: &lokiv1.RetentionLimitSpec{
							Days: 30,
						},
					},
				},
			},
			Status: lokiv1.LokiStackStatus{
				Storage: lokiv1.LokiStackStorageStatus{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV12,
							EffectiveDate: "2020-10-11",
						},
						{
							Version:       lokiv1.ObjectStorageSchemaV13,
							EffectiveDate: "2024-10-22",
						},
					},
				},
			},
		}

		newStack := &lokiv1.LokiStack{
			ObjectMeta: metav1.ObjectMeta{
				Name: "testing-stack",
			},
			Spec: lokiv1.LokiStackSpec{
				Size: lokiv1.SizeOneXExtraSmall,
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV13,
							EffectiveDate: "2024-10-22",
						},
					},
				},
				Limits: &lokiv1.LimitsSpec{
					Global: &lokiv1.LimitsTemplateSpec{
						Retention: &lokiv1.RetentionLimitSpec{
							Days: 60, // Changed from 30 to 60
						},
					},
				},
			},
			Status: lokiv1.LokiStackStatus{
				Storage: lokiv1.LokiStackStorageStatus{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV12,
							EffectiveDate: "2020-10-11",
						},
						{
							Version:       lokiv1.ObjectStorageSchemaV13,
							EffectiveDate: "2024-10-22",
						},
					},
				},
			},
		}
		ctx := context.Background()

		v := &validation.LokiStackValidator{}
		warnings, err := v.ValidateUpdate(ctx, currentStack, newStack)

		require.Error(t, err)
		require.Contains(t, err.Error(), lokiv1.ErrSchemaRetentionConflict.Error())
		// No warnings when validation fails
		require.Len(t, warnings, 1) // Still has retention warning
	})

	t.Run("allow schema change when retention unchanged", func(t *testing.T) {
		currentStack := &lokiv1.LokiStack{
			ObjectMeta: metav1.ObjectMeta{
				Name: "testing-stack",
			},
			Spec: lokiv1.LokiStackSpec{
				Size: lokiv1.SizeOneXExtraSmall,
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV13,
							EffectiveDate: "2024-10-22",
						},
					},
				},
				Limits: &lokiv1.LimitsSpec{
					Global: &lokiv1.LimitsTemplateSpec{
						Retention: &lokiv1.RetentionLimitSpec{
							Days: 30,
						},
					},
				},
			},
		}

		newStack := &lokiv1.LokiStack{
			ObjectMeta: metav1.ObjectMeta{
				Name: "testing-stack",
			},
			Spec: lokiv1.LokiStackSpec{
				Size: lokiv1.SizeOneXExtraSmall,
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV13,
							EffectiveDate: "2024-10-22",
						},
						{
							Version:       lokiv1.ObjectStorageSchemaV13,
							EffectiveDate: "2026-10-22",
						},
					},
				},
				Limits: &lokiv1.LimitsSpec{
					Global: &lokiv1.LimitsTemplateSpec{
						Retention: &lokiv1.RetentionLimitSpec{
							Days: 30, // Same as before
						},
					},
				},
			},
		}
		ctx := context.Background()

		v := &validation.LokiStackValidator{}
		warnings, err := v.ValidateUpdate(ctx, currentStack, newStack)

		require.NoError(t, err)
		require.Len(t, warnings, 0)
	})

	t.Run("allow retention change when schemas unchanged", func(t *testing.T) {
		currentStack := &lokiv1.LokiStack{
			ObjectMeta: metav1.ObjectMeta{
				Name: "testing-stack",
			},
			Spec: lokiv1.LokiStackSpec{
				Size: lokiv1.SizeOneXExtraSmall,
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV13,
							EffectiveDate: "2024-10-22",
						},
					},
				},
				Limits: &lokiv1.LimitsSpec{
					Global: &lokiv1.LimitsTemplateSpec{
						Retention: &lokiv1.RetentionLimitSpec{
							Days: 30,
						},
					},
				},
			},
		}

		newStack := &lokiv1.LokiStack{
			ObjectMeta: metav1.ObjectMeta{
				Name: "testing-stack",
			},
			Spec: lokiv1.LokiStackSpec{
				Size: lokiv1.SizeOneXExtraSmall,
				Storage: lokiv1.ObjectStorageSpec{
					Schemas: []lokiv1.ObjectStorageSchema{
						{
							Version:       lokiv1.ObjectStorageSchemaV13,
							EffectiveDate: "2024-10-22",
						},
					},
				},
				Limits: &lokiv1.LimitsSpec{
					Global: &lokiv1.LimitsTemplateSpec{
						Retention: &lokiv1.RetentionLimitSpec{
							Days: 60, // Changed from 30 to 60
						},
					},
				},
			},
		}
		ctx := context.Background()

		v := &validation.LokiStackValidator{}
		warnings, err := v.ValidateUpdate(ctx, currentStack, newStack)

		require.NoError(t, err)
		require.Len(t, warnings, 1)
		require.Equal(t, lokiv1.WarnRetentionUpdate, warnings[0])
	})
}
