package handlers

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
)

func TestCreateOrRotateCertificates_WhenGetReturnsNotFound_DoesNotError(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	k.GetStub = func(ctx context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	err := CreateOrRotateCertificates(context.TODO(), logger, r, k, scheme, featureGates)
	require.NoError(t, err)

	// make sure create was NOT called because the Get failed
	require.Zero(t, k.CreateCallCount())
}

func TestCreateOrRotateCertificates_WhenGetReturnsAnErrorOtherThanNotFound_ReturnsTheError(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	badRequestErr := apierrors.NewBadRequest("you do not belong here")
	k.GetStub = func(ctx context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		return badRequestErr
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	err := CreateOrRotateCertificates(context.TODO(), logger, r, k, scheme, featureGates)

	require.Equal(t, badRequestErr, errors.Unwrap(err))

	// make sure create was NOT called because the Get failed
	require.Zero(t, k.CreateCallCount())
}

func TestCreateOrRotateCertificates_SetsNamespaceOnAllObjects(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	stack := lokiv1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
		Spec: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXExtraSmall,
			Storage: lokiv1.ObjectStorageSpec{
				Schemas: []lokiv1.ObjectStorageSchema{
					{
						Version:       lokiv1.ObjectStorageSchemaV11,
						EffectiveDate: "2020-10-11",
					},
				},
				Secret: lokiv1.ObjectStorageSecretSpec{
					Name: defaultSecret.Name,
					Type: lokiv1.ObjectStorageSecretS3,
				},
			},
			Tenants: &lokiv1.TenantsSpec{
				Mode: "dynamic",
				Authentication: []lokiv1.AuthenticationSpec{
					{
						TenantName: "test",
						TenantID:   "1234",
						OIDC: &lokiv1.OIDCSpec{
							Secret: &lokiv1.TenantSecretSpec{
								Name: defaultGatewaySecret.Name,
							},
						},
					},
				},
				Authorization: &lokiv1.AuthorizationSpec{
					OPA: &lokiv1.OPASpec{
						URL: "some-url",
					},
				},
			},
		},
	}

	k.GetStub = func(_ context.Context, name types.NamespacedName, out client.Object, _ ...client.GetOption) error {
		if r.Name == name.Name && r.Namespace == name.Namespace {
			k.SetClientObject(out, &stack)
			return nil
		}
		if defaultSecret.Name == name.Name {
			k.SetClientObject(out, &defaultSecret)
			return nil
		}
		if defaultGatewaySecret.Name == name.Name {
			k.SetClientObject(out, &defaultGatewaySecret)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	k.CreateStub = func(_ context.Context, o client.Object, _ ...client.CreateOption) error {
		assert.Equal(t, r.Namespace, o.GetNamespace())
		return nil
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	err := CreateOrRotateCertificates(context.TODO(), logger, r, k, scheme, featureGates)
	require.NoError(t, err)

	// make sure create was called
	require.NotZero(t, k.CreateCallCount())
}

func TestCreateOrRotateCertificates_SetsOwnerRefOnAllObjects(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	stack := lokiv1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "someStack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
		Spec: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXExtraSmall,
			Storage: lokiv1.ObjectStorageSpec{
				Schemas: []lokiv1.ObjectStorageSchema{
					{
						Version:       lokiv1.ObjectStorageSchemaV11,
						EffectiveDate: "2020-10-11",
					},
				},
				Secret: lokiv1.ObjectStorageSecretSpec{
					Name: defaultSecret.Name,
					Type: lokiv1.ObjectStorageSecretS3,
				},
			},
			Tenants: &lokiv1.TenantsSpec{
				Mode: "dynamic",
				Authentication: []lokiv1.AuthenticationSpec{
					{
						TenantName: "test",
						TenantID:   "1234",
						OIDC: &lokiv1.OIDCSpec{
							Secret: &lokiv1.TenantSecretSpec{
								Name: defaultGatewaySecret.Name,
							},
						},
					},
				},
				Authorization: &lokiv1.AuthorizationSpec{
					OPA: &lokiv1.OPASpec{
						URL: "some-url",
					},
				},
			},
		},
	}

	// Create looks up the CR first, so we need to return our fake stack
	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		if r.Name == name.Name && r.Namespace == name.Namespace {
			k.SetClientObject(object, &stack)
			return nil
		}
		if defaultSecret.Name == name.Name {
			k.SetClientObject(object, &defaultSecret)
			return nil
		}
		if defaultGatewaySecret.Name == name.Name {
			k.SetClientObject(object, &defaultGatewaySecret)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	expected := metav1.OwnerReference{
		APIVersion:         lokiv1.GroupVersion.String(),
		Kind:               stack.Kind,
		Name:               stack.Name,
		UID:                stack.UID,
		Controller:         ptr.To(true),
		BlockOwnerDeletion: ptr.To(true),
	}

	k.CreateStub = func(_ context.Context, o client.Object, _ ...client.CreateOption) error {
		// OwnerRefs are appended so we have to find ours in the list
		var ref metav1.OwnerReference
		var found bool
		for _, or := range o.GetOwnerReferences() {
			if or.UID == stack.UID {
				found = true
				ref = or
				break
			}
		}

		require.True(t, found, "expected to find a matching ownerRef, but did not")
		require.EqualValues(t, expected, ref)
		return nil
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	err := CreateOrRotateCertificates(context.TODO(), logger, r, k, scheme, featureGates)
	require.NoError(t, err)

	// make sure create was called
	require.NotZero(t, k.CreateCallCount())
}

func TestCreateOrRotateCertificates_WhenSetControllerRefInvalid_ContinueWithOtherObjects(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	stack := lokiv1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "someStack",
			// Set invalid namespace here, because
			// because cross-namespace controller
			// references are not allowed
			Namespace: "invalid-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
		Spec: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXExtraSmall,
			Storage: lokiv1.ObjectStorageSpec{
				Schemas: []lokiv1.ObjectStorageSchema{
					{
						Version:       lokiv1.ObjectStorageSchemaV11,
						EffectiveDate: "2020-10-11",
					},
				},
				Secret: lokiv1.ObjectStorageSecretSpec{
					Name: defaultSecret.Name,
					Type: lokiv1.ObjectStorageSecretS3,
				},
			},
		},
	}

	// Create looks up the CR first, so we need to return our fake stack
	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		if r.Name == name.Name && r.Namespace == name.Namespace {
			k.SetClientObject(object, &stack)
		}
		if defaultSecret.Name == name.Name {
			k.SetClientObject(object, &defaultSecret)
		}
		return nil
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	err := CreateOrRotateCertificates(context.TODO(), logger, r, k, scheme, featureGates)

	// make sure error is returned to re-trigger reconciliation
	require.Error(t, err)
}

func TestCreateOrRotateCertificates_WhenGetReturnsNoError_UpdateObjects(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	stack := lokiv1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
		Spec: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXExtraSmall,
			Storage: lokiv1.ObjectStorageSpec{
				Schemas: []lokiv1.ObjectStorageSchema{
					{
						Version:       lokiv1.ObjectStorageSchemaV11,
						EffectiveDate: "2020-10-11",
					},
				},
				Secret: lokiv1.ObjectStorageSecretSpec{
					Name: defaultSecret.Name,
					Type: lokiv1.ObjectStorageSecretS3,
				},
			},
		},
	}

	secret := corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack-signing-ca",
			Namespace: "some-ns",
			Labels: map[string]string{
				"app.kubernetes.io/name":     "loki",
				"app.kubernetes.io/provider": "openshift",
				"loki.grafana.com/name":      "my-stack",

				// Add custom label to fake semantic not equal
				"test": "test",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "loki.grafana.com/v1",
					Kind:               "LokiStack",
					Name:               "my-stack",
					UID:                "b23f9a38-9672-499f-8c29-15ede74d3ece",
					Controller:         ptr.To(true),
					BlockOwnerDeletion: ptr.To(true),
				},
			},
		},
	}

	// Create looks up the CR first, so we need to return our fake stack
	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		if r.Name == name.Name && r.Namespace == name.Namespace {
			k.SetClientObject(object, &stack)
		}
		if defaultSecret.Name == name.Name {
			k.SetClientObject(object, &defaultSecret)
		}
		if secret.Name == name.Name && secret.Namespace == name.Namespace {
			k.SetClientObject(object, &secret)
		}
		return nil
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	err := CreateOrRotateCertificates(context.TODO(), logger, r, k, scheme, featureGates)
	require.NoError(t, err)

	// make sure create not called
	require.Zero(t, k.CreateCallCount())

	// make sure update was called
	require.NotZero(t, k.UpdateCallCount())
}

func TestCreateOrRotateCertificats_WhenCreateReturnsError_ContinueWithOtherObjects(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	stack := lokiv1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "someStack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
		Spec: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXExtraSmall,
			Storage: lokiv1.ObjectStorageSpec{
				Schemas: []lokiv1.ObjectStorageSchema{
					{
						Version:       lokiv1.ObjectStorageSchemaV11,
						EffectiveDate: "2020-10-11",
					},
				},
				Secret: lokiv1.ObjectStorageSecretSpec{
					Name: defaultSecret.Name,
					Type: lokiv1.ObjectStorageSecretS3,
				},
			},
		},
	}

	// GetStub looks up the CR first, so we need to return our fake stack
	// return NotFound for everything else to trigger create.
	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		if r.Name == name.Name && r.Namespace == name.Namespace {
			k.SetClientObject(object, &stack)
			return nil
		}
		if defaultSecret.Name == name.Name {
			k.SetClientObject(object, &defaultSecret)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something is not found")
	}

	// CreateStub returns an error for each resource to trigger reconciliation a new.
	k.CreateStub = func(_ context.Context, o client.Object, _ ...client.CreateOption) error {
		return apierrors.NewTooManyRequestsError("too many create requests")
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	err := CreateOrRotateCertificates(context.TODO(), logger, r, k, scheme, featureGates)

	// make sure error is returned to re-trigger reconciliation
	require.Error(t, err)
}

func TestCreateOrRotateCertificates_WhenUpdateReturnsError_ContinueWithOtherObjects(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	stack := lokiv1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
		Spec: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXExtraSmall,
			Storage: lokiv1.ObjectStorageSpec{
				Schemas: []lokiv1.ObjectStorageSchema{
					{
						Version:       lokiv1.ObjectStorageSchemaV11,
						EffectiveDate: "2020-10-11",
					},
				},
				Secret: lokiv1.ObjectStorageSecretSpec{
					Name: defaultSecret.Name,
					Type: lokiv1.ObjectStorageSecretS3,
				},
			},
		},
	}

	secret := corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack-signing-ca",
			Namespace: "some-ns",
			Labels: map[string]string{
				"app.kubernetes.io/name":     "loki",
				"app.kubernetes.io/provider": "openshift",
				"loki.grafana.com/name":      "my-stack",

				// Add custom label to fake semantic not equal
				"test": "test",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "loki.grafana.com/v1",
					Kind:               "LokiStack",
					Name:               "someStack",
					UID:                "b23f9a38-9672-499f-8c29-15ede74d3ece",
					Controller:         ptr.To(true),
					BlockOwnerDeletion: ptr.To(true),
				},
			},
		},
	}

	// GetStub looks up the CR first, so we need to return our fake stack
	// return NotFound for everything else to trigger create.
	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		if r.Name == name.Name && r.Namespace == name.Namespace {
			k.SetClientObject(object, &stack)
		}
		if defaultSecret.Name == name.Name {
			k.SetClientObject(object, &defaultSecret)
		}
		if secret.Name == name.Name && secret.Namespace == name.Namespace {
			k.SetClientObject(object, &secret)
		}
		return nil
	}

	// CreateStub returns an error for each resource to trigger reconciliation a new.
	k.UpdateStub = func(_ context.Context, o client.Object, _ ...client.UpdateOption) error {
		return apierrors.NewTooManyRequestsError("too many create requests")
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	err := CreateOrRotateCertificates(context.TODO(), logger, r, k, scheme, featureGates)

	// make sure error is returned to re-trigger reconciliation
	require.Error(t, err)
}
