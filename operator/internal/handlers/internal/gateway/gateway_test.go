package gateway

import (
	"context"
	"io"
	"testing"

	"github.com/ViaQ/logerr/v2/log"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1 "github.com/grafana/loki/operator/api/config/v1"
	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
	"github.com/grafana/loki/operator/internal/status"
)

var (
	logger = log.NewLogger("testing", log.WithOutput(io.Discard))

	defaultSecret = corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "some-stack-secret",
			Namespace: "some-ns",
		},
		Data: map[string][]byte{
			"endpoint":          []byte("s3://your-endpoint"),
			"region":            []byte("a-region"),
			"bucketnames":       []byte("bucket1,bucket2"),
			"access_key_id":     []byte("a-secret-id"),
			"access_key_secret": []byte("a-secret-key"),
		},
	}

	defaultGatewaySecret = corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "some-stack-gateway-secret",
			Namespace: "some-ns",
		},
		Data: map[string][]byte{
			"clientID":     []byte("client-123"),
			"clientSecret": []byte("client-secret-xyz"),
			"issuerCAPath": []byte("/tmp/test/ca.pem"),
		},
	}

	invalidSecret = corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "some-stack-secret",
			Namespace: "some-ns",
		},
		Data: map[string][]byte{},
	}
)

func TestBuildOptions_WhenInvalidTenantsConfiguration_SetDegraded(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	degradedErr := &status.DegradedError{
		Message: "Invalid tenants configuration: mandatory configuration - missing OPA Url",
		Reason:  lokiv1.ReasonInvalidTenantsConfiguration,
		Requeue: false,
	}

	fg := configv1.FeatureGates{
		LokiStackGateway: true,
	}

	stack := &lokiv1.LokiStack{
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
				Authorization: nil,
			},
		},
	}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		_, isLokiStack := object.(*lokiv1.LokiStack)
		if r.Name == name.Name && r.Namespace == name.Namespace && isLokiStack {
			k.SetClientObject(object, stack)
			return nil
		}
		if defaultSecret.Name == name.Name {
			k.SetClientObject(object, &defaultSecret)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something is not found")
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	_, _, err := BuildOptions(context.TODO(), logger, k, stack, fg)

	// make sure error is returned
	require.Error(t, err)
	require.Equal(t, degradedErr, err)
}

func TestBuildOptions_WhenMissingGatewaySecret_SetDegraded(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	degradedErr := &status.DegradedError{
		Message: "Missing secrets for tenant test",
		Reason:  lokiv1.ReasonMissingGatewayTenantSecret,
		Requeue: true,
	}

	fg := configv1.FeatureGates{
		LokiStackGateway: true,
	}

	stack := &lokiv1.LokiStack{
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

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		o, ok := object.(*lokiv1.LokiStack)
		if r.Name == name.Name && r.Namespace == name.Namespace && ok {
			k.SetClientObject(o, stack)
			return nil
		}
		if defaultSecret.Name == name.Name {
			k.SetClientObject(object, &defaultSecret)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something is not found")
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	_, _, err := BuildOptions(context.TODO(), logger, k, stack, fg)

	// make sure error is returned to re-trigger reconciliation
	require.Error(t, err)
	require.Equal(t, degradedErr, err)
}

func TestBuildOptions_WhenInvalidGatewaySecret_SetDegraded(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	degradedErr := &status.DegradedError{
		Message: "Invalid gateway tenant secret contents",
		Reason:  lokiv1.ReasonInvalidGatewayTenantSecret,
		Requeue: true,
	}

	fg := configv1.FeatureGates{
		LokiStackGateway: true,
	}

	stack := &lokiv1.LokiStack{
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
								Name: invalidSecret.Name,
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

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		o, ok := object.(*lokiv1.LokiStack)
		if r.Name == name.Name && r.Namespace == name.Namespace && ok {
			k.SetClientObject(o, stack)
			return nil
		}
		if defaultSecret.Name == name.Name {
			k.SetClientObject(object, &defaultSecret)
			return nil
		}
		if name.Name == invalidSecret.Name {
			k.SetClientObject(object, &invalidSecret)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something is not found")
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	_, _, err := BuildOptions(context.TODO(), logger, k, stack, fg)

	// make sure error is returned to re-trigger reconciliation
	require.Error(t, err)
	require.Equal(t, degradedErr, err)
}

func TestBuildOptions_MissingTenantsSpec_SetDegraded(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	degradedErr := &status.DegradedError{
		Message: "Invalid tenants configuration: TenantsSpec cannot be nil when gateway flag is enabled",
		Reason:  lokiv1.ReasonInvalidTenantsConfiguration,
		Requeue: false,
	}

	fg := configv1.FeatureGates{
		LokiStackGateway: true,
	}

	stack := &lokiv1.LokiStack{
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
			Tenants: nil,
		},
	}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		o, ok := object.(*lokiv1.LokiStack)
		if r.Name == name.Name && r.Namespace == name.Namespace && ok {
			k.SetClientObject(o, stack)
			return nil
		}
		if defaultSecret.Name == name.Name {
			k.SetClientObject(object, &defaultSecret)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something is not found")
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	_, _, err := BuildOptions(context.TODO(), logger, k, stack, fg)

	// make sure error is returned
	require.Error(t, err)
	require.Equal(t, degradedErr, err)
}
