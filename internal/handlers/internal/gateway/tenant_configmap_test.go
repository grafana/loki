package gateway

import (
	"context"
	"testing"

	"github.com/ViaQ/loki-operator/internal/manifests/openshift"

	"github.com/ViaQ/loki-operator/internal/external/k8s/k8sfakes"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var tenantConfigData = []byte(`
tenants:
- name: application
  id: test-123
  openshift:
    serviceAccount: lokistack-gateway-lokistack-dev
    cookieSecret: test123
- name: infrastructure
  id: test-456
  openshift:
    serviceAccount: lokistack-gateway-lokistack-dev
    cookieSecret: test456
- name: audit
  id: test-789
  openshift:
    serviceAccount: lokistack-gateway-lokistack-dev
    cookieSecret: test789
`)

func TestGetTenantConfigMapData_ConfigMapExist(t *testing.T) {
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "lokistack-gateway",
			Namespace: "some-ns",
		},
	}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		if name.Name == "lokistack-gateway" && name.Namespace == "some-ns" {
			k.SetClientObject(object, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "lokistack-gateway",
					Namespace: "some-ns",
				},
				BinaryData: map[string][]byte{
					"tenants.yaml": tenantConfigData,
				},
			})
		}
		return nil
	}

	ts := GetTenantConfigMapData(context.TODO(), k, r)
	require.NotNil(t, ts)

	expected := map[string]openshift.TenantData{
		"application": {
			TenantID:     "test-123",
			CookieSecret: "test123",
		},
		"infrastructure": {
			TenantID:     "test-456",
			CookieSecret: "test456",
		},
		"audit": {
			TenantID:     "test-789",
			CookieSecret: "test789",
		},
	}
	require.Equal(t, expected, ts)
}

func TestGetTenantConfigMapData_ConfigMapNotExist(t *testing.T) {
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "lokistack-gateway",
			Namespace: "some-ns",
		},
	}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		return nil
	}

	ts := GetTenantConfigMapData(context.TODO(), k, r)
	require.Nil(t, ts)
}
