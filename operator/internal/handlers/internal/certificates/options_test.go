package certificates

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
)

func TestGetOptions_ReturnEmpty_WhenCertificatesNotExisting(t *testing.T) {
	k := &k8sfakes.FakeClient{}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "lokistack-dev",
			Namespace: "ns",
		},
	}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	opts, err := GetOptions(context.TODO(), k, req, lokiv1.Static)
	require.NoError(t, err)
	require.NotEmpty(t, opts)

	// Basic options always available
	require.Equal(t, req.Name, opts.StackName)
	require.Equal(t, req.Namespace, opts.StackNamespace)

	// Require all resource empty as per not existing
	require.Nil(t, opts.Signer.Secret)
	require.Nil(t, opts.CABundle)
	require.Len(t, opts.Certificates, 0)
}

func TestGetOptions_ReturnSecrets_WhenCertificatesExisting(t *testing.T) {
	k := &k8sfakes.FakeClient{}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "lokistack-dev",
			Namespace: "ns",
		},
	}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		obj, ok := getManagedPKIResource(req.Name, req.Namespace, name.Name)
		if !ok {
			return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
		}

		k.SetClientObject(object, obj)
		return nil
	}

	opts, err := GetOptions(context.TODO(), k, req, lokiv1.Static)
	require.NoError(t, err)
	require.NotEmpty(t, opts)

	// Basic options always available
	require.Equal(t, req.Name, opts.StackName)
	require.Equal(t, req.Namespace, opts.StackNamespace)

	// Check signingCA and CABundle populated into options
	require.NotNil(t, opts.Signer.Secret)
	require.NotNil(t, opts.CABundle)

	// Check client certificates populated into options
	for name, cert := range opts.Certificates {
		require.NotNil(t, cert.Secret, "missing name %s", name)
	}
}

func TestGetOptions_PruneServiceCAAnnotations_ForTenantMode(t *testing.T) {
	k := &k8sfakes.FakeClient{}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "lokistack-dev",
			Namespace: "ns",
		},
	}

	pruned := []string{
		"service.alpha.openshift.io/expiry",
		"service.beta.openshift.io/expiry",
		"service.beta.openshift.io/originating-service-name",
		"service.beta.openshift.io/originating-service-uid",
		"service.beta.openshift.io/inject-cabundle",
	}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		obj, ok := getManagedPKIResource(req.Name, req.Namespace, name.Name)
		if !ok {
			return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
		}

		annotations := map[string]string{}
		for _, value := range pruned {
			annotations[value] = "test"
		}
		obj.SetAnnotations(annotations)

		k.SetClientObject(object, obj)
		return nil
	}

	tt := []struct {
		mode      lokiv1.ModeType
		wantPrune bool
	}{
		{
			mode: lokiv1.Dynamic,
		},
		{
			mode: lokiv1.Static,
		},
		{
			mode:      lokiv1.OpenshiftLogging,
			wantPrune: true,
		},
		{
			mode:      lokiv1.OpenshiftNetwork,
			wantPrune: true,
		},
	}

	for _, tc := range tt {
		t.Run(string(tc.mode), func(t *testing.T) {
			opts, err := GetOptions(context.TODO(), k, req, tc.mode)
			require.NoError(t, err)
			require.NotEmpty(t, opts)

			if !tc.wantPrune {
				return
			}

			// Require CABundle ConfigMap annotations to be pruned
			for _, annotation := range pruned {
				require.NotContains(t, opts.CABundle.Annotations, annotation)
			}

			// Require Certificate Secrets annotations to be pruned
			for _, cert := range opts.Certificates {
				for _, annotation := range pruned {
					require.NotContains(t, cert.Secret.Annotations, annotation)
				}
			}
		})
	}
}

func getManagedPKIResource(stackName, stackNamespace, name string) (client.Object, bool) {
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

	objsByName := map[string]client.Object{}
	for _, name := range certNames {
		secretName := fmt.Sprintf("%s-%s", stackName, name)

		var obj client.Object
		if strings.HasSuffix(name, "ca-bundle") {
			obj = &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: stackNamespace,
				},
			}
		} else {
			obj = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: stackNamespace,
				},
			}
		}

		objsByName[secretName] = obj
	}

	obj, ok := objsByName[name]
	return obj, ok
}
