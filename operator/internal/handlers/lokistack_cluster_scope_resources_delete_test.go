package handlers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
	"github.com/grafana/loki/operator/internal/manifests/openshift"
)

func TestDeleteClusterScopedResources(t *testing.T) {
	opts := openshift.NewOptionsClusterScope("operator-ns", nil, nil)
	objs := openshift.BuildRBAC(opts)
	objs = append(objs, openshift.BuildDashboards(opts.OperatorNs)...)

	k := &k8sfakes.FakeClient{}

	err := DeleteClusterScopedResources(context.Background(), k, "operator-ns")
	require.NoError(t, err)
	require.Equal(t, k.DeleteCallCount(), len(objs))
}

func TestDeleteClusterScopedResources_ReturnsNoError_WhenNotFound(t *testing.T) {
	k := &k8sfakes.FakeClient{}
	k.DeleteStub = func(context.Context, client.Object, ...client.DeleteOption) error {
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	err := DeleteClusterScopedResources(context.Background(), k, "operator-ns")
	require.NoError(t, err)
}
