package storageschema

import (
	"context"
	"testing"

	lokiv1beta1 "github.com/grafana/loki/operator/api/v1beta1"
	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
	"github.com/grafana/loki/operator/internal/manifests/storage"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var lokiConfigData = []byte(`
schema_config:
  configs:
    - from: 2006-01-02
      index:
        period: 24h
        prefix: index_
      object_store: s3
      schema: v11
      store: boltdb-shipper
`)

func TestGetLokiStorageSchemaData_ConfigMapExists(t *testing.T) {
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "lokistack-dev",
			Namespace: "some-ns",
		},
	}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		if name.Name == "lokistack-dev-config" && name.Namespace == "some-ns" {
			k.SetClientObject(object, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "lokistack-dev-config",
					Namespace: "some-ns",
				},
				BinaryData: map[string][]byte{
					"config.yaml": lokiConfigData,
				},
			})
		}
		return nil
	}

	s, e := GetLokiStorageSchemaData(context.TODO(), k, r)

	expected := []storage.Schema{
		{
			From:    "2006-01-02",
			Version: lokiv1beta1.ObjectStorageSchemaV11,
		},
	}

	require.NoError(t, e)
	require.Equal(t, expected, s)
}

func TestGetLokiStorageSchemaData_ConfigMapNotExist(t *testing.T) {
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "lokistack-dev",
			Namespace: "some-ns",
		},
	}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	s, e := GetLokiStorageSchemaData(context.TODO(), k, r)

	require.Empty(t, s)
	require.NoError(t, e)
}
