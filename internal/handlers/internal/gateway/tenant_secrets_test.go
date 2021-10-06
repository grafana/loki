package gateway

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	lokiv1beta1 "github.com/ViaQ/loki-operator/api/v1beta1"
	"github.com/ViaQ/loki-operator/internal/external/k8s/k8sfakes"
	"github.com/ViaQ/loki-operator/internal/manifests"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var scheme = runtime.NewScheme()

func TestGetTenantSecrets_StaticMode(t *testing.T) {
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	s := &lokiv1beta1.LokiStack{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mystack",
			Namespace: "some-ns",
		},
		Spec: lokiv1beta1.LokiStackSpec{
			Tenants: &lokiv1beta1.TenantsSpec{
				Mode: lokiv1beta1.Static,
				Authentication: []lokiv1beta1.AuthenticationSpec{
					{
						TenantName: "test",
						TenantID:   "test",
						OIDC: &lokiv1beta1.OIDCSpec{
							Secret: &lokiv1beta1.TenantSecretSpec{
								Name: "test",
							},
						},
					},
				},
			},
		},
	}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		if name.Name == "test" && name.Namespace == "some-ns" {
			k.SetClientObject(object, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "some-ns",
				},
				Data: map[string][]byte{
					"clientID":     []byte("test"),
					"clientSecret": []byte("test"),
					"issuerCAPath": []byte("/path/to/ca/file"),
				},
			})
		}
		return nil
	}

	ts, err := GetTenantSecrets(context.TODO(), k, r, scheme, s)
	require.NoError(t, err)

	expected := []*manifests.TenantSecrets{
		{
			TenantName:   "test",
			ClientID:     "test",
			ClientSecret: "test",
			IssuerCAPath: "/path/to/ca/file",
		},
	}
	require.ElementsMatch(t, ts, expected)
}

func TestGetTenantSecrets_DynamicMode(t *testing.T) {
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	s := &lokiv1beta1.LokiStack{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mystack",
			Namespace: "some-ns",
		},
		Spec: lokiv1beta1.LokiStackSpec{
			Tenants: &lokiv1beta1.TenantsSpec{
				Mode: lokiv1beta1.Dynamic,
				Authentication: []lokiv1beta1.AuthenticationSpec{
					{
						TenantName: "test",
						TenantID:   "test",
						OIDC: &lokiv1beta1.OIDCSpec{
							Secret: &lokiv1beta1.TenantSecretSpec{
								Name: "test",
							},
						},
					},
				},
			},
		},
	}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		if name.Name == "test" && name.Namespace == "some-ns" {
			k.SetClientObject(object, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "some-ns",
				},
				Data: map[string][]byte{
					"clientID":     []byte("test"),
					"clientSecret": []byte("test"),
					"issuerCAPath": []byte("/path/to/ca/file"),
				},
			})
		}
		return nil
	}

	ts, err := GetTenantSecrets(context.TODO(), k, r, scheme, s)
	require.NoError(t, err)

	expected := []*manifests.TenantSecrets{
		{
			TenantName:   "test",
			ClientID:     "test",
			ClientSecret: "test",
			IssuerCAPath: "/path/to/ca/file",
		},
	}
	require.ElementsMatch(t, ts, expected)
}

func TestGetTenantSecrets_OpenShiftLoggingMode(t *testing.T) {
	// Register the clientgo and CRD schemes
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(lokiv1beta1.AddToScheme(scheme))

	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	s := &lokiv1beta1.LokiStack{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
		Spec: lokiv1beta1.LokiStackSpec{
			Tenants: &lokiv1beta1.TenantsSpec{
				Mode: lokiv1beta1.OpenshiftLogging,
			},
		},
	}

	expectedNames := []string{
		"lokistack-gateway-my-stack-application",
		"lokistack-gateway-my-stack-infrastructure",
		"lokistack-gateway-my-stack-audit",
	}

	k.CreateStub = func(_ context.Context, object client.Object, _ ...client.CreateOption) error {
		require.Contains(t, expectedNames, object.GetName())
		require.Equal(t, "some-ns", object.GetNamespace())

		return nil
	}

	ts, err := GetTenantSecrets(context.TODO(), k, r, scheme, s)
	require.NoError(t, err)
	require.Equal(t, 3, k.CreateCallCount())

	expected := []*manifests.TenantSecrets{
		{
			TenantName:   "application",
			ClientID:     "clientID",
			ClientSecret: "clientSecret",
			IssuerCAPath: "/path/to/ca/file",
		},
		{
			TenantName:   "infrastructure",
			ClientID:     "clientID",
			ClientSecret: "clientSecret",
			IssuerCAPath: "/path/to/ca/file",
		},
		{
			TenantName:   "audit",
			ClientID:     "clientID",
			ClientSecret: "clientSecret",
			IssuerCAPath: "/path/to/ca/file",
		},
	}
	require.ElementsMatch(t, ts, expected)
}
