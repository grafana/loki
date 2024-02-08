package handlers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
)

func TestDeleteCredentialsRequest_DoNothing_WhenManagedAuthEnvMissing(t *testing.T) {
	k := &k8sfakes.FakeClient{}
	key := client.ObjectKey{Name: "my-stack", Namespace: "ns"}

	err := DeleteCredentialsRequest(context.Background(), k, key)
	require.NoError(t, err)
}

func TestDeleteCredentialsRequest_DeleteExistingResource(t *testing.T) {
	k := &k8sfakes.FakeClient{}
	key := client.ObjectKey{Name: "my-stack", Namespace: "ns"}

	t.Setenv("ROLEARN", "a-role-arn")

	err := DeleteCredentialsRequest(context.Background(), k, key)
	require.NoError(t, err)
	require.Equal(t, 1, k.DeleteCallCount())
}

func TestDeleteCredentialsRequest_DoNothing_WhenCredentialsRequestNotExists(t *testing.T) {
	k := &k8sfakes.FakeClient{}
	key := client.ObjectKey{Name: "my-stack", Namespace: "ns"}

	t.Setenv("ROLEARN", "a-role-arn")

	k.DeleteStub = func(_ context.Context, _ client.Object, _ ...client.DeleteOption) error {
		return errors.NewNotFound(schema.GroupResource{}, "credentials request not found")
	}

	err := DeleteCredentialsRequest(context.Background(), k, key)
	require.NoError(t, err)
	require.Equal(t, 1, k.DeleteCallCount())
}
