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

func TestCreateCredentialsRequest_DoNothing_WhenManagedAuthEnvMissing(t *testing.T) {
	k := &k8sfakes.FakeClient{}
	key := client.ObjectKey{Name: "my-stack", Namespace: "ns"}

	secretRef, err := CreateCredentialsRequest(context.Background(), k, key)
	require.NoError(t, err)
	require.Empty(t, secretRef)
}

func TestCreateCredentialsRequest_CreateNewResource(t *testing.T) {
	k := &k8sfakes.FakeClient{}
	key := client.ObjectKey{Name: "my-stack", Namespace: "ns"}

	t.Setenv("ROLEARN", "a-role-arn")

	secretRef, err := CreateCredentialsRequest(context.Background(), k, key)
	require.NoError(t, err)
	require.NotEmpty(t, secretRef)
	require.Equal(t, 1, k.CreateCallCount())
}

func TestCreateCredentialsRequest_DoNothing_WhenCredentialsRequestExist(t *testing.T) {
	k := &k8sfakes.FakeClient{}
	key := client.ObjectKey{Name: "my-stack", Namespace: "ns"}

	t.Setenv("ROLEARN", "a-role-arn")

	k.CreateStub = func(_ context.Context, _ client.Object, _ ...client.CreateOption) error {
		return errors.NewAlreadyExists(schema.GroupResource{}, "credentialsrequest exists")
	}

	secretRef, err := CreateCredentialsRequest(context.Background(), k, key)
	require.NoError(t, err)
	require.NotEmpty(t, secretRef)
	require.Equal(t, 1, k.CreateCallCount())
}
