package gateway

import (
	"context"
	"testing"

	routev1 "github.com/openshift/api/route/v1"
	"github.com/stretchr/testify/require"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
)

var (
	_ = routev1.AddToScheme(scheme.Scheme)
	_ = networkingv1.AddToScheme(scheme.Scheme)
	_ = lokiv1.AddToScheme(scheme.Scheme)
)

func TestCleanup_ExternalAccessEnabled_ReturnsEarly(t *testing.T) {
	k := &k8sfakes.FakeClient{}

	stack := &lokiv1.LokiStack{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-stack",
			Namespace: "test-ns",
		},
		Spec: lokiv1.LokiStackSpec{
			Tenants: &lokiv1.TenantsSpec{
				Mode:           lokiv1.Static,
				DisableIngress: false, // External access enabled
			},
		},
	}

	err := Cleanup(context.TODO(), logger, k, stack)
	require.NoError(t, err)

	require.Equal(t, 0, k.GetCallCount())
	require.Equal(t, 0, k.DeleteCallCount())
}

func TestCleanup_ExternalAccessDisabled_Delete(t *testing.T) {
	stack := &lokiv1.LokiStack{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-stack",
			Namespace: "test-ns",
			UID:       "test-uid",
		},
		Spec: lokiv1.LokiStackSpec{
			Tenants: &lokiv1.TenantsSpec{
				Mode:           lokiv1.Static,
				DisableIngress: true, // External access disabled
			},
		},
	}

	testCases := []struct {
		name            string
		object          client.Object
		shouldBeDeleted bool
	}{
		{
			name: "ingress without owner reference is not deleted",
			object: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-stack",
					Namespace: "test-ns",
				},
			},
			shouldBeDeleted: false,
		},
		{
			name: "ingress with owner reference gets deleted",
			object: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-stack",
					Namespace: "test-ns",
					OwnerReferences: []metav1.OwnerReference{
						*metav1.NewControllerRef(stack, lokiv1.GroupVersion.WithKind("LokiStack")),
					},
				},
			},
			shouldBeDeleted: true,
		},
		{
			name: "route with owner reference gets deleted",
			object: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-stack",
					Namespace: "test-ns",
					OwnerReferences: []metav1.OwnerReference{
						*metav1.NewControllerRef(stack, lokiv1.GroupVersion.WithKind("LokiStack")),
					},
				},
			},
			shouldBeDeleted: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			k := fake.NewClientBuilder().WithObjects(tc.object).WithScheme(scheme.Scheme).Build()

			err := Cleanup(context.TODO(), logger, k, stack)
			require.NoError(t, err)

			key := client.ObjectKeyFromObject(tc.object)

			var checkObj client.Object
			switch tc.object.(type) {
			case *routev1.Route:
				checkObj = &routev1.Route{}
			case *networkingv1.Ingress:
				checkObj = &networkingv1.Ingress{}
			default:
				t.Fatalf("unsupported object type: %T", tc.object)
			}

			err = k.Get(context.TODO(), key, checkObj)

			if tc.shouldBeDeleted {
				require.True(t, apierrors.IsNotFound(err), "expected %s/%s to be deleted", tc.object.GetObjectKind().GroupVersionKind().Kind, tc.object.GetName())
			} else {
				require.NoError(t, err, "expected %s/%s to still exist", tc.object.GetObjectKind().GroupVersionKind().Kind, tc.object.GetName())
			}
		})
	}
}

func TestCleanup_RouteNotRegistered_NoError(t *testing.T) {
	stack := &lokiv1.LokiStack{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-stack",
			Namespace: "test-ns",
			UID:       "test-uid",
		},
		Spec: lokiv1.LokiStackSpec{
			Tenants: &lokiv1.TenantsSpec{
				Mode:           lokiv1.Static,
				DisableIngress: true, // External access disabled
			},
		},
	}

	newScheme := runtime.NewScheme()
	// Only add Ingress and LokiStack, not Route
	require.NoError(t, networkingv1.AddToScheme(newScheme))
	require.NoError(t, lokiv1.AddToScheme(newScheme))

	k := fake.NewClientBuilder().WithScheme(newScheme).Build()

	err := Cleanup(context.TODO(), logger, k, stack)
	require.NoError(t, err, "cleanup should handle route not registered gracefully")
}
