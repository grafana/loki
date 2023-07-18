package lokistackconfig

import (
	"context"
	"testing"

	configv1 "github.com/grafana/loki/operator/apis/config/v1"
	lokiv1beta1 "github.com/grafana/loki/operator/apis/loki/v1beta1"
	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestGetCluster_WhenNotFound_ReturnDefaultForCommunityBundle(t *testing.T) {
	k := &k8sfakes.FakeClient{}

	k.GetStub = func(_ context.Context, _ types.NamespacedName, _ client.Object, _ ...client.GetOption) error {
		return apierrors.NewNotFound(schema.GroupResource{Group: "loki.grafana.com", Resource: "cluster"}, "something not found")
	}

	lsc, err := Get(context.TODO(), k, configv1.BundleTypeCommunity)
	require.NoError(t, err)
	require.Equal(t, communityConfig, lsc.Spec)
}

func TestGetCluster_WhenNotFound_ReturnDefaultForCommunityOpenShiftBundle(t *testing.T) {
	k := &k8sfakes.FakeClient{}

	k.GetStub = func(_ context.Context, _ types.NamespacedName, _ client.Object, _ ...client.GetOption) error {
		return apierrors.NewNotFound(schema.GroupResource{Group: "loki.grafana.com", Resource: "cluster"}, "something not found")
	}

	lsc, err := Get(context.TODO(), k, configv1.BundleTypeCommunityOpenShift)
	require.NoError(t, err)
	require.Equal(t, communityOpenShiftConfig, lsc.Spec)
}

func TestGetCluster_WhenNotFound_ReturnDefaultForOpenShiftBundle(t *testing.T) {
	k := &k8sfakes.FakeClient{}

	k.GetStub = func(_ context.Context, _ types.NamespacedName, _ client.Object, _ ...client.GetOption) error {
		return apierrors.NewNotFound(schema.GroupResource{Group: "loki.grafana.com", Resource: "cluster"}, "something not found")
	}

	lsc, err := Get(context.TODO(), k, configv1.BundleTypeOpenShift)
	require.NoError(t, err)
	require.Equal(t, openShiftConfig, lsc.Spec)
}

func TestGetCluster_WhenFound_ReturnMergedLokiStackConfigSpec(t *testing.T) {
	k := &k8sfakes.FakeClient{}

	cluster := lokiv1beta1.LokiStackConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: resourceName,
		},
		Spec: lokiv1beta1.LokiStackConfigSpec{
			Gates: lokiv1beta1.FeatureGates{
				LokiStackGateway: false,
				OpenShift: lokiv1beta1.OpenShiftFeatureGates{
					ClusterTLSPolicy: false,
				},
			},
		},
	}

	k.GetStub = func(_ context.Context, _ types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		k.SetClientObject(object, &cluster)
		return nil
	}

	lsc, err := Get(context.TODO(), k, configv1.BundleTypeOpenShift)
	require.NoError(t, err)
	require.NotEqual(t, openShiftConfig, lsc.Spec)
	require.False(t, lsc.Spec.Gates.LokiStackGateway)
	require.False(t, lsc.Spec.Gates.OpenShift.ClusterTLSPolicy)
}
