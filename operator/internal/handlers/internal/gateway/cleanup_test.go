package gateway

import (
	"context"
	"errors"
	"testing"

	routev1 "github.com/openshift/api/route/v1"
	"github.com/stretchr/testify/require"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
	"github.com/grafana/loki/operator/internal/manifests"
)

func TestCleanup_ExternalAccessEnabled_ReturnsEarly(t *testing.T) {
	k := &k8sfakes.FakeClient{}

	stack := &lokiv1.LokiStack{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-stack",
			Namespace: "test-ns",
		},
		Spec: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				Gateway: &lokiv1.LokiGatewayComponentSpec{
					ExternalAccess: &lokiv1.ExternalAccessSpec{
						Disabled: false, // External access enabled
					},
				},
			},
		},
	}

	err := Cleanup(context.TODO(), logger, k, stack)
	require.NoError(t, err)

	require.Equal(t, 0, k.GetCallCount())
	require.Equal(t, 0, k.DeleteCallCount())
}

func TestCleanup_ExternalAccessDisabled_ResourcesDoNotExist_ReturnsSuccess(t *testing.T) {
	k := &k8sfakes.FakeClient{}

	stack := &lokiv1.LokiStack{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-stack",
			Namespace: "test-ns",
		},
		Spec: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				Gateway: &lokiv1.LokiGatewayComponentSpec{
					ExternalAccess: &lokiv1.ExternalAccessSpec{
						Disabled: true, // External access disabled
					},
				},
			},
		},
	}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		return apierrors.NewNotFound(schema.GroupResource{}, "not found")
	}

	err := Cleanup(context.TODO(), logger, k, stack)
	require.NoError(t, err)

	// Verify Get was called but no Delete calls
	require.Equal(t, 2, k.GetCallCount())
	require.Equal(t, 0, k.DeleteCallCount())
}

func TestCleanup_GetFails_ReturnsError(t *testing.T) {
	k := &k8sfakes.FakeClient{}

	stack := &lokiv1.LokiStack{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-stack",
			Namespace: "test-ns",
		},
		Spec: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				Gateway: &lokiv1.LokiGatewayComponentSpec{
					ExternalAccess: &lokiv1.ExternalAccessSpec{
						Disabled: true, // External access disabled
					},
				},
			},
		},
	}

	expectedErr := apierrors.NewInternalError(errors.New("test error"))
	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		return expectedErr
	}

	err := Cleanup(context.TODO(), logger, k, stack)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to lookup resource")
}

func TestCleanup_ExternalAccessDisabled_ResourcesExistWithoutOwnerRef_SkipsDeletion(t *testing.T) {
	k := &k8sfakes.FakeClient{}

	stack := &lokiv1.LokiStack{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-stack",
			Namespace: "test-ns",
			UID:       "test-uid",
		},
		Spec: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				Gateway: &lokiv1.LokiGatewayComponentSpec{
					ExternalAccess: &lokiv1.ExternalAccessSpec{
						Disabled: true, // External access disabled
					},
				},
			},
		},
	}

	// Create mock Route without owner reference
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      manifests.GatewayName("test-stack"),
			Namespace: "test-ns",
			// No OwnerReferences
		},
	}

	// Create mock Ingress without owner reference
	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      manifests.GatewayName("test-stack"),
			Namespace: "test-ns",
			// No OwnerReferences
		},
	}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		if name.Name == manifests.GatewayName("test-stack") {
			switch object.(type) {
			case *routev1.Route:
				k.SetClientObject(object, route)
			case *networkingv1.Ingress:
				k.SetClientObject(object, ingress)
			}
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "not found")
	}

	err := Cleanup(context.TODO(), logger, k, stack)
	require.NoError(t, err)

	// Verify no resources were deleted
	require.Equal(t, 2, k.GetCallCount())
	require.Equal(t, 0, k.DeleteCallCount())
}
