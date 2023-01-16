package validation_test

import (
	"context"
	"testing"

	"github.com/grafana/loki/operator/apis/loki/v1beta1"
	lokiv1beta1 "github.com/grafana/loki/operator/apis/loki/v1beta1"
	"github.com/grafana/loki/operator/internal/validation"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/pointer"
)

var rctt = []struct {
	desc string
	spec v1beta1.RulerConfigSpec
	err  *apierrors.StatusError
}{
	{
		desc: "valid spec with no AM header credentials",
		spec: v1beta1.RulerConfigSpec{
			AlertManagerSpec: &lokiv1beta1.AlertManagerSpec{
				Client: &lokiv1beta1.AlertManagerClientConfig{
					BasicAuth: &lokiv1beta1.AlertManagerClientBasicAuth{
						Username: pointer.String("user"),
						Password: pointer.String("pass"),
					},
				},
			},
			Overrides: map[string]lokiv1beta1.RulerOverrides{
				"tenant": {
					AlertManagerOverrides: &lokiv1beta1.AlertManagerSpec{
						Client: &lokiv1beta1.AlertManagerClientConfig{
							BasicAuth: &lokiv1beta1.AlertManagerClientBasicAuth{
								Username: pointer.String("user1"),
								Password: pointer.String("pass1"),
							},
						},
					},
				},
			},
		},
	},
	{
		desc: "valid spec with Credentials",
		spec: v1beta1.RulerConfigSpec{
			AlertManagerSpec: &lokiv1beta1.AlertManagerSpec{
				Client: &lokiv1beta1.AlertManagerClientConfig{
					HeaderAuth: &lokiv1beta1.AlertManagerClientHeaderAuth{
						Credentials: pointer.String("creds"),
					},
				},
			},
			Overrides: map[string]lokiv1beta1.RulerOverrides{
				"tenant": {
					AlertManagerOverrides: &lokiv1beta1.AlertManagerSpec{
						Client: &lokiv1beta1.AlertManagerClientConfig{
							HeaderAuth: &lokiv1beta1.AlertManagerClientHeaderAuth{
								Credentials: pointer.String("creds1"),
							},
						},
					},
				},
			},
		},
	},
	{
		desc: "valid spec with CredentialsFile",
		spec: v1beta1.RulerConfigSpec{
			AlertManagerSpec: &lokiv1beta1.AlertManagerSpec{
				Client: &lokiv1beta1.AlertManagerClientConfig{
					HeaderAuth: &lokiv1beta1.AlertManagerClientHeaderAuth{
						CredentialsFile: pointer.String("creds-file"),
					},
				},
			},
			Overrides: map[string]lokiv1beta1.RulerOverrides{
				"tenant": {
					AlertManagerOverrides: &lokiv1beta1.AlertManagerSpec{
						Client: &lokiv1beta1.AlertManagerClientConfig{
							HeaderAuth: &lokiv1beta1.AlertManagerClientHeaderAuth{
								CredentialsFile: pointer.String("creds-file1"),
							},
						},
					},
				},
			},
		},
	},
	{
		desc: "valid spec with CredentialsFile override",
		spec: v1beta1.RulerConfigSpec{
			AlertManagerSpec: &lokiv1beta1.AlertManagerSpec{
				Client: &lokiv1beta1.AlertManagerClientConfig{
					HeaderAuth: &lokiv1beta1.AlertManagerClientHeaderAuth{
						Credentials: pointer.String("creds"),
					},
				},
			},
			Overrides: map[string]lokiv1beta1.RulerOverrides{
				"tenant": {
					AlertManagerOverrides: &lokiv1beta1.AlertManagerSpec{
						Client: &lokiv1beta1.AlertManagerClientConfig{
							HeaderAuth: &lokiv1beta1.AlertManagerClientHeaderAuth{
								CredentialsFile: pointer.String("creds-file1"),
							},
						},
					},
				},
			},
		},
	},
	{
		desc: "both Credentials and CredentialsFile defined",
		spec: v1beta1.RulerConfigSpec{
			AlertManagerSpec: &lokiv1beta1.AlertManagerSpec{
				Client: &lokiv1beta1.AlertManagerClientConfig{
					HeaderAuth: &lokiv1beta1.AlertManagerClientHeaderAuth{
						Credentials:     pointer.String("creds"),
						CredentialsFile: pointer.String("creds-file"),
					},
				},
			},
			Overrides: map[string]lokiv1beta1.RulerOverrides{
				"tenant": {
					AlertManagerOverrides: &lokiv1beta1.AlertManagerSpec{
						Client: &lokiv1beta1.AlertManagerClientConfig{
							HeaderAuth: &lokiv1beta1.AlertManagerClientHeaderAuth{
								Credentials:     pointer.String("creds1"),
								CredentialsFile: pointer.String("creds-file1"),
							},
						},
					},
				},
			},
		},
		err: apierrors.NewInvalid(
			schema.GroupKind{Group: "loki.grafana.com", Kind: "RulerConfig"},
			"testing-ruler",
			field.ErrorList{
				field.Invalid(
					field.NewPath("spec", "alertmanager", "client", "headerAuth", "credentials"),
					"creds",
					lokiv1beta1.ErrHeaderAuthCredentialsConflict.Error(),
				),
				field.Invalid(
					field.NewPath("spec", "alertmanager", "client", "headerAuth", "credentialsFile"),
					"creds-file",
					lokiv1beta1.ErrHeaderAuthCredentialsConflict.Error(),
				),
				field.Invalid(
					field.NewPath("spec", "overrides", "tenant", "alertmanager", "client", "headerAuth", "credentials"),
					"creds1",
					lokiv1beta1.ErrHeaderAuthCredentialsConflict.Error(),
				),
				field.Invalid(
					field.NewPath("spec", "overrides", "tenant", "alertmanager", "client", "headerAuth", "credentialsFile"),
					"creds-file1",
					lokiv1beta1.ErrHeaderAuthCredentialsConflict.Error(),
				),
			},
		),
	},
}

func TestRulerConfigValidationWebhook_ValidateCreate(t *testing.T) {
	for _, tc := range rctt {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			l := &v1beta1.RulerConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "testing-ruler",
				},
				Spec: tc.spec,
			}

			v := &validation.RulerConfigValidator{}
			err := v.ValidateCreate(ctx, l)
			if err != nil {
				require.Equal(t, tc.err, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRulerConfigValidationWebhook_ValidateUpdate(t *testing.T) {
	for _, tc := range rctt {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			l := &v1beta1.RulerConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "testing-ruler",
				},
				Spec: tc.spec,
			}

			v := &validation.RulerConfigValidator{}
			err := v.ValidateUpdate(ctx, &v1beta1.RulerConfig{}, l)
			if err != nil {
				require.Equal(t, tc.err, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
