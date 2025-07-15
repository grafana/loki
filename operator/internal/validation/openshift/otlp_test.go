package openshift

import (
	"testing"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
)

func TestValidateOTLPInvalidDrop(t *testing.T) {
	tt := []struct {
		desc string
		spec lokiv1.LokiStack
		err  *apierrors.StatusError
	}{
		{
			desc: "lokistack using openshift-logging tenancy mode trying to remove a required stream label",
			spec: lokiv1.LokiStack{
				Spec: lokiv1.LokiStackSpec{
					Limits: &lokiv1.LimitsSpec{
						Global: &lokiv1.LimitsTemplateSpec{
							OTLP: &lokiv1.OTLPSpec{
								Drop: &lokiv1.OTLPMetadataSpec{
									ResourceAttributes: []lokiv1.OTLPAttributeReference{
										{
											Name: "kubernetes.namespace_name",
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
						Mode: lokiv1.OpenshiftLogging,
					},
				},
			},
			err: apierrors.NewInvalid(
				schema.GroupKind{Group: "loki.grafana.com", Kind: "LokiStack"},
				"testing-stack",
				field.ErrorList{
					field.Invalid(
						field.NewPath("spec", "limits", "global", "otlp", "drop", "resourceAttributes").Index(0).Child("name"),
						"kubernetes.namespace_name",
						lokiv1.ErrOTLPInvalidDrop.Error(),
					),
				},
			),
		},
	}

	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			errList := ValidateOTLPInvalidDrop(&tc.spec.Spec)
			if tc.err != nil {
				testErr := apierrors.NewInvalid(
					schema.GroupKind{Group: "loki.grafana.com", Kind: "LokiStack"},
					"testing-stack",
					errList,
				)
				require.Equal(t, tc.err, testErr)
			} else {
				require.Nil(t, errList)
			}
		})
	}
}
