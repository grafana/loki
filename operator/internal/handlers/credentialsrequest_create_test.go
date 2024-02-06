package handlers

import (
	"context"
	"testing"

	cloudcredentialv1 "github.com/openshift/cloud-credential-operator/pkg/apis/cloudcredential/v1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/grafana/loki/operator/internal/config"
	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
)

func TestCreateCredentialsRequest_DoNothing_WhenManagedAuthEnvMissing(t *testing.T) {
	k := &k8sfakes.FakeClient{}
	key := client.ObjectKey{Name: "my-stack", Namespace: "ns"}

	secretRef, err := CreateCredentialsRequest(context.Background(), nil, k, key, nil)
	require.NoError(t, err)
	require.Empty(t, secretRef)
}

func TestCreateCredentialsRequest_CreateNewResource(t *testing.T) {
	k := &k8sfakes.FakeClient{}
	key := client.ObjectKey{Name: "my-stack", Namespace: "ns"}

	managedAuth := &config.ManagedAuthEnv{
		AWS: &config.AWSSTSEnv{
			RoleARN: "a-role-arn",
		},
	}

	secretRef, err := CreateCredentialsRequest(context.Background(), managedAuth, k, key, nil)
	require.NoError(t, err)
	require.NotEmpty(t, secretRef)
	require.Equal(t, 1, k.CreateCallCount())
}

func TestCreateCredentialsRequest_CreateNewResourceAzure(t *testing.T) {
	wantRegion := "test-region"

	k := &k8sfakes.FakeClient{}
	key := client.ObjectKey{Name: "my-stack", Namespace: "ns"}
	secret := &corev1.Secret{
		Data: map[string][]byte{
			"region": []byte(wantRegion),
		},
	}

	managedAuth := &config.ManagedAuthEnv{
		Azure: &config.AzureWIFEnvironment{
			ClientID:       "test-client-id",
			SubscriptionID: "test-tenant-id",
			TenantID:       "test-subscription-id",
		},
	}

	secretRef, err := CreateCredentialsRequest(context.Background(), managedAuth, k, key, secret)
	require.NoError(t, err)
	require.NotEmpty(t, secretRef)

	require.Equal(t, 1, k.CreateCallCount())
	_, obj, _ := k.CreateArgsForCall(0)
	credReq, ok := obj.(*cloudcredentialv1.CredentialsRequest)
	require.True(t, ok)

	providerSpec := &cloudcredentialv1.AzureProviderSpec{}
	require.NoError(t, cloudcredentialv1.Codec.DecodeProviderSpec(credReq.Spec.ProviderSpec, providerSpec))

	require.Equal(t, wantRegion, providerSpec.AzureRegion)
}

func TestCreateCredentialsRequest_CreateNewResourceAzure_Errors(t *testing.T) {
	k := &k8sfakes.FakeClient{}
	key := client.ObjectKey{Name: "my-stack", Namespace: "ns"}

	tt := []struct {
		secret    *corev1.Secret
		wantError string
	}{
		{
			secret:    nil,
			wantError: errAzureNoSecretFound.Error(),
		},
		{
			secret:    &corev1.Secret{},
			wantError: errAzureNoRegion.Error(),
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.wantError, func(t *testing.T) {
			t.Parallel()

			managedAuth := &config.ManagedAuthEnv{
				Azure: &config.AzureWIFEnvironment{
					ClientID:       "test-client-id",
					SubscriptionID: "test-tenant-id",
					TenantID:       "test-subscription-id",
				},
			}

			_, err := CreateCredentialsRequest(context.Background(), managedAuth, k, key, tc.secret)
			require.EqualError(t, err, tc.wantError)
		})
	}
}

func TestCreateCredentialsRequest_DoNothing_WhenCredentialsRequestExist(t *testing.T) {
	k := &k8sfakes.FakeClient{}
	key := client.ObjectKey{Name: "my-stack", Namespace: "ns"}

	managedAuth := &config.ManagedAuthEnv{
		AWS: &config.AWSSTSEnv{
			RoleARN: "a-role-arn",
		},
	}

	k.CreateStub = func(_ context.Context, _ client.Object, _ ...client.CreateOption) error {
		return errors.NewAlreadyExists(schema.GroupResource{}, "credentialsrequest exists")
	}

	secretRef, err := CreateCredentialsRequest(context.Background(), managedAuth, k, key, nil)
	require.NoError(t, err)
	require.NotEmpty(t, secretRef)
	require.Equal(t, 1, k.CreateCallCount())
}
