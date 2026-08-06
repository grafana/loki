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
			"bucketnames":       []byte("loki-data"),
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

func TestBuildOptions_PassthroughMode_MissingCA_SetDegraded(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	degradedErr := &status.DegradedError{
		Message: "Invalid passthrough configuration: missing CA configuration",
		Reason:  lokiv1.ReasonInvalidPassthroughConfiguration,
		Requeue: false,
	}

	fg := configv1.FeatureGates{
		LokiStackGateway: true,
		HTTPEncryption:   true,
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
				Mode: lokiv1.Passthrough,
				Passthrough: &lokiv1.PassthroughTenantSpec{
					CA: nil, // Missing CA
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

	require.Error(t, err)
	require.Equal(t, degradedErr, err)
}

func TestValidatePassthroughCA(t *testing.T) {
	const stackNamespace = "some-ns"

	validCAConfigMap := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "passthrough-ca-configmap",
			Namespace: stackNamespace,
		},
		Data: map[string]string{
			"ca.crt": "test",
		},
	}

	invalidCAConfigMap := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "passthrough-ca-configmap-invalid",
			Namespace: stackNamespace,
		},
		Data: map[string]string{},
	}

	validCASecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "passthrough-ca-secret",
			Namespace: stackNamespace,
		},
		Data: map[string][]byte{
			"ca.crt": []byte("test"),
		},
	}

	for _, tc := range []struct {
		name           string
		httpEncryption bool
		passthrough    *lokiv1.PassthroughTenantSpec
		expError       error
	}{
		{
			name:           "http encryption disabled skips CA validation entirely",
			httpEncryption: false,
			passthrough:    nil,
			expError:       nil,
		},
		{
			name:           "missing passthrough spec",
			httpEncryption: true,
			passthrough:    nil,
			expError: &status.DegradedError{
				Message: "Invalid passthrough configuration: missing CA configuration",
				Reason:  lokiv1.ReasonInvalidPassthroughConfiguration,
				Requeue: false,
			},
		},
		{
			name:           "missing CA field",
			httpEncryption: true,
			passthrough:    &lokiv1.PassthroughTenantSpec{CA: nil},
			expError: &status.DegradedError{
				Message: "Invalid passthrough configuration: missing CA configuration",
				Reason:  lokiv1.ReasonInvalidPassthroughConfiguration,
				Requeue: false,
			},
		},
		{
			name:           "CA from valid ConfigMap",
			httpEncryption: true,
			passthrough: &lokiv1.PassthroughTenantSpec{
				CA: &lokiv1.ValueReference{
					Key:           "ca.crt",
					ConfigMapName: validCAConfigMap.Name,
				},
			},
			expError: nil,
		},
		{
			name:           "CA from valid Secret",
			httpEncryption: true,
			passthrough: &lokiv1.PassthroughTenantSpec{
				CA: &lokiv1.ValueReference{
					Key:        "ca.crt",
					SecretName: validCASecret.Name,
				},
			},
			expError: nil,
		},
		{
			name:           "CA ConfigMap does not exist in the cluster",
			httpEncryption: true,
			passthrough: &lokiv1.PassthroughTenantSpec{
				CA: &lokiv1.ValueReference{
					Key:           "ca.crt",
					ConfigMapName: "non-existent-configmap",
				},
			},
			expError: &status.DegradedError{
				Message: `Missing configmap for field "ca" in passthrough gateway configuration: non-existent-configmap`,
				Reason:  lokiv1.ReasonInvalidPassthroughConfiguration,
				Requeue: true,
			},
		},
		{
			name:           "CA ConfigMap exists but is missing the referenced key",
			httpEncryption: true,
			passthrough: &lokiv1.PassthroughTenantSpec{
				CA: &lokiv1.ValueReference{
					Key:           "ca.crt",
					ConfigMapName: invalidCAConfigMap.Name,
				},
			},
			expError: &status.DegradedError{
				Message: `Invalid configmap passthrough-ca-configmap-invalid for field "ca" in passthrough gateway configuration, missing key: ca.crt`,
				Reason:  lokiv1.ReasonInvalidPassthroughConfiguration,
				Requeue: true,
			},
		},
		{
			name:           "CA Secret does not exist in the cluster",
			httpEncryption: true,
			passthrough: &lokiv1.PassthroughTenantSpec{
				CA: &lokiv1.ValueReference{
					Key:        "ca.crt",
					SecretName: "non-existent-secret",
				},
			},
			expError: &status.DegradedError{
				Message: `Missing secret for field "ca" in passthrough gateway configuration: non-existent-secret`,
				Reason:  lokiv1.ReasonInvalidPassthroughConfiguration,
				Requeue: true,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k := &k8sfakes.FakeClient{}

			stack := &lokiv1.LokiStack{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-stack",
					Namespace: stackNamespace,
				},
				Spec: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode:        lokiv1.Passthrough,
						Passthrough: tc.passthrough,
					},
				},
			}

			k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
				switch obj := object.(type) {
				case *corev1.ConfigMap:
					switch name.Name {
					case validCAConfigMap.Name:
						k.SetClientObject(obj, &validCAConfigMap)
						return nil
					case invalidCAConfigMap.Name:
						k.SetClientObject(obj, &invalidCAConfigMap)
						return nil
					}
				case *corev1.Secret:
					switch name.Name {
					case validCASecret.Name:
						k.SetClientObject(obj, &validCASecret)
						return nil
					}
				}
				return apierrors.NewNotFound(schema.GroupResource{}, name.Name)
			}

			err := validatePassthroughCA(context.Background(), k, tc.httpEncryption, stack)

			if tc.expError != nil {
				require.Equal(t, tc.expError, err)
				return
			}

			require.NoError(t, err)
		})
	}
}
