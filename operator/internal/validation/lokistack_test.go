package validation_test

import (
	"context"
	"fmt"
	"testing"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
	"github.com/grafana/loki/operator/internal/validation"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
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
		desc: "invalid zones to replication factor ratio",
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
					Factor: 2,
				},
			},
		},
		err: apierrors.NewInvalid(
			schema.GroupKind{Group: "loki.grafana.com", Kind: "LokiStack"},
			"testing-stack",
			field.ErrorList{
				field.Invalid(
					field.NewPath("spec", "replication", "factor"),
					2,
					lokiv1.ErrReplicationFactorToZonesRatio.Error(),
				),
			},
		),
	},
	{
		desc: "invalid zones topologyKey",
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
					field.NewPath("spec", "replication", "zones"),
					[]lokiv1.ZoneSpec{
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
					lokiv1.ErrReplicationZonesNodes.Error(),
				),
				field.Invalid(
					field.NewPath("spec", "replication", "factor"),
					1,
					lokiv1.ErrReplicationFactorToZonesRatio.Error(),
				),
			},
		),
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

			k := prepFakeClient()
			v := &validation.LokiStackValidator{
				Client: k,
			}
			err := v.ValidateCreate(ctx, l)
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

			k := prepFakeClient()
			v := &validation.LokiStackValidator{
				Client: k,
			}
			err := v.ValidateUpdate(ctx, &lokiv1.LokiStack{}, l)
			if err != nil {
				require.Equal(t, tc.err, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func prepFakeClient() *k8sfakes.FakeClient {
	k := &k8sfakes.FakeClient{}
	nodes := corev1.NodeList{
		Items: []corev1.Node{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node1",
					Labels: map[string]string{
						"zone":   "z1",
						"region": "r1",
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node2",
					Labels: map[string]string{
						"zone":   "z2",
						"region": "r1",
					},
				},
			},
		},
	}

	k.ListStub = func(_ context.Context, object client.ObjectList, opts ...client.ListOption) error {

		res := corev1.NodeList{
			Items: []corev1.Node{},
		}

		for _, opt := range opts {
			optLabels := opt.(client.HasLabels)
			for _, l := range optLabels {
				for _, node := range nodes.Items {
					if _, ok := node.Labels[l]; !ok {
						return fmt.Errorf("node with label %s not found", l)
					}
					res.Items = append(res.Items, node)
				}
			}
		}

		k.SetClientObjectList(object, &nodes)
		return nil
	}

	return k
}
