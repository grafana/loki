package gateway

import (
	"context"
	"testing"

	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
	"github.com/grafana/loki/operator/internal/manifests"

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
    serviceAccount: lokistack-dev-gateway
    cookieSecret: test123
- name: infrastructure
  id: test-456
  openshift:
    serviceAccount: lokistack-dev-gateway
    cookieSecret: test456
- name: audit
  id: test-789
  openshift:
    serviceAccount: lokistack-dev-gateway
    cookieSecret: test789
`)

func TestGetTenantConfigMapData_ConfigMapExist(t *testing.T) {
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "lokistack-dev",
			Namespace: "some-ns",
		},
	}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		if name.Name == "lokistack-dev-gateway" && name.Namespace == "some-ns" {
			k.SetClientObject(object, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "lokistack-dev-gateway",
					Namespace: "some-ns",
				},
				BinaryData: map[string][]byte{
					"tenants.yaml": tenantConfigData,
				},
			})
		}
		return nil
	}

	ts, err := GetTenantConfigMapData(context.TODO(), k, r)
	require.NotNil(t, ts)
	require.NoError(t, err)

	expected := map[string]manifests.TenantConfig{
		"application": {
			OpenShift: &manifests.TenantOpenShiftSpec{
				CookieSecret: "test123",
			},
		},
		"infrastructure": {
			OpenShift: &manifests.TenantOpenShiftSpec{
				CookieSecret: "test456",
			},
		},
		"audit": {
			OpenShift: &manifests.TenantOpenShiftSpec{
				CookieSecret: "test789",
			},
		},
	}
	require.Equal(t, expected, ts)
}

func TestGetTenantConfigMapData_ConfigMapNotExist(t *testing.T) {
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "lokistack-dev",
			Namespace: "some-ns",
		},
	}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		return nil
	}

	ts, err := GetTenantConfigMapData(context.TODO(), k, r)
	require.Nil(t, ts)
	require.Error(t, err)
}
