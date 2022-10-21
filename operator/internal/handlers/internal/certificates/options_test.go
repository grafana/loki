package certificates

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestGetOptions(t *testing.T) {
	k := &k8sfakes.FakeClient{}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "lokistack-dev",
			Namespace: "ns",
		},
	}

	certNames := []string{
		"signing-ca",
		"ca-bundle",
		// client certificates
		"gateway-client-http",
		// serving certificates
		"compactor-http",
		"compactor-grpc",
		"distributor-http",
		"distributor-grpc",
		"index-gateway-http",
		"index-gateway-grpc",
		"ingester-http",
		"ingester-grpc",
		"querier-http",
		"querier-grpc",
		"query-frontend-http",
		"query-frontend-grpc",
		"ruler-http",
		"ruler-grpc",
	}

	objs := make([]client.Object, 0)
	objsByName := map[string]client.Object{}
	for _, name := range certNames {
		secretName := fmt.Sprintf("%s-%s", req.Name, name)

		var obj client.Object
		if strings.HasSuffix(name, "ca-bundle") {
			obj = &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: req.Namespace,
				},
			}
		} else {
			obj = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: req.Namespace,
				},
			}
		}

		objs = append(objs, obj)
		objsByName[secretName] = obj
	}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		obj, ok := objsByName[name.Name]
		if ok {
			k.SetClientObject(object, obj)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	tt := []struct {
		desc string
		objs []client.Object
	}{
		{
			desc: "empty",
		},
		{
			desc: "populate certificates secrets",
			objs: objs,
		},
	}

	for _, test := range tt {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			opts, err := GetOptions(context.TODO(), k, req)
			require.NoError(t, err)
			require.NotEmpty(t, opts)

			// Basic options always available
			require.Equal(t, req.Name, opts.StackName)
			require.Equal(t, req.Namespace, opts.StackNamespace)

			if test.objs != nil {
				// Check signingCA and CABundle populated into options
				require.NotNil(t, opts.Signer.Secret)
				require.NotNil(t, opts.CABundle)

				// Check client certificates populated into options
				for _, cert := range opts.Certificates {
					require.NotNil(t, cert.Secret)
				}
			}
		})
	}
}
