package handlers

import (
	"context"
	"testing"

	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
	"github.com/grafana/loki/operator/internal/manifests/openshift"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestDeleteDashboards(t *testing.T) {
	objs, err := openshift.BuildDashboards("operator-ns")
	require.NoError(t, err)

	k := &k8sfakes.FakeClient{}

	err = DeleteDashboards(context.TODO(), k, "operator-ns")
	require.NoError(t, err)
	require.Equal(t, k.DeleteCallCount(), len(objs))
}

func TestDeleteDashboards_ReturnsNoError_WhenNotFound(t *testing.T) {
	k := &k8sfakes.FakeClient{}
	k.DeleteStub = func(context.Context, client.Object, ...client.DeleteOption) error {
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	err := DeleteDashboards(context.TODO(), k, "operator-ns")
	require.NoError(t, err)
}
