package v1beta1_test

import (
	"testing"

	"github.com/grafana/loki/operator/api/v1beta1"
	"github.com/stretchr/testify/require"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

var ltt = []struct {
	desc string
	spec v1beta1.LokiStack
	err  *apierrors.StatusError
}{
	{
		desc: "valid spec",
		spec: v1beta1.LokiStack{
			Spec: v1beta1.LokiStackSpec{
				Storage: v1beta1.ObjectStorageSpec{
					Schemas: []v1beta1.ObjectStorageSchema{
						{
							Version:       v1beta1.ObjectStorageSchemaV11,
							EffectiveDate: "2020-10-11",
						},
						{
							Version:       v1beta1.ObjectStorageSchemaV12,
							EffectiveDate: "2020-10-13",
						},
					},
				},
			},
		},
	},
	{
		desc: "not unique schema effective dates",
		spec: v1beta1.LokiStack{
			Spec: v1beta1.LokiStackSpec{
				Storage: v1beta1.ObjectStorageSpec{
					Schemas: []v1beta1.ObjectStorageSchema{
						{
							Version:       v1beta1.ObjectStorageSchemaV11,
							EffectiveDate: "2020-10-11",
						},
						{
							Version:       v1beta1.ObjectStorageSchemaV12,
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
					field.NewPath("Spec").Child("Storage").Child("Schemas").Index(1).Child("EffectiveDate"),
					"2020-10-11",
					v1beta1.ErrEffectiveDatesNotUnique.Error(),
				),
			},
		),
	},
	{
		desc: "schema effective dates bad format",
		spec: v1beta1.LokiStack{
			Spec: v1beta1.LokiStackSpec{
				Storage: v1beta1.ObjectStorageSpec{
					Schemas: []v1beta1.ObjectStorageSchema{
						{
							Version:       v1beta1.ObjectStorageSchemaV11,
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
					field.NewPath("Spec").Child("Storage").Child("Schemas").Index(0).Child("EffectiveDate"),
					"2020/10/11",
					v1beta1.ErrParseEffectiveDates.Error(),
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
			l := v1beta1.LokiStack{
				ObjectMeta: metav1.ObjectMeta{
					Name: "testing-stack",
				},
				Spec: tc.spec.Spec,
			}

			err := l.ValidateCreate()
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
			l := v1beta1.LokiStack{
				ObjectMeta: metav1.ObjectMeta{
					Name: "testing-stack",
				},
				Spec: tc.spec.Spec,
			}

			err := l.ValidateUpdate(&v1beta1.LokiStack{})
			if err != nil {
				require.Equal(t, tc.err, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
