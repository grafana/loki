package handlers

import (
	"context"
	"testing"

	cloudcredentialv1 "github.com/openshift/cloud-credential-operator/pkg/apis/cloudcredential/v1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/config"
	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
)

func credentialsRequestFakeClient(cr *cloudcredentialv1.CredentialsRequest, lokistack *lokiv1.LokiStack, secret *corev1.Secret) *k8sfakes.FakeClient {
	k := &k8sfakes.FakeClient{}
	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		switch object.(type) {
		case *cloudcredentialv1.CredentialsRequest:
			if cr == nil {
				return errors.NewNotFound(schema.GroupResource{}, name.Name)
			}
			k.SetClientObject(object, cr)
		case *lokiv1.LokiStack:
			if lokistack == nil {
				return errors.NewNotFound(schema.GroupResource{}, name.Name)
			}
			k.SetClientObject(object, lokistack)
		case *corev1.Secret:
			if secret == nil {
				return errors.NewNotFound(schema.GroupResource{}, name.Name)
			}
			k.SetClientObject(object, secret)
		}
		return nil
	}

	return k
}

func TestCreateCredentialsRequest_CreateNewResource(t *testing.T) {
	wantServiceAccountNames := []string{
		"my-stack",
		"my-stack-ruler",
	}

	lokistack := &lokiv1.LokiStack{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "ns",
		},
	}

	k := credentialsRequestFakeClient(nil, lokistack, nil)
	req := ctrl.Request{
		NamespacedName: client.ObjectKey{Name: "my-stack", Namespace: "ns"},
	}

	managedAuth := &config.ManagedAuthConfig{
		AWS: &config.AWSEnvironment{
			RoleARN: "a-role-arn",
		},
	}

	err := CreateCredentialsRequest(context.Background(), logger, scheme, managedAuth, k, req)
	require.NoError(t, err)
	require.Equal(t, 1, k.CreateCallCount())

	_, obj, _ := k.CreateArgsForCall(0)
	credReq, ok := obj.(*cloudcredentialv1.CredentialsRequest)
	require.True(t, ok)

	require.Equal(t, wantServiceAccountNames, credReq.Spec.ServiceAccountNames)
}

func TestCreateCredentialsRequest_CreateNewResourceAzure(t *testing.T) {
	wantRegion := "test-region"

	lokistack := &lokiv1.LokiStack{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "ns",
		},
	}
	secret := &corev1.Secret{
		Data: map[string][]byte{
			"region": []byte(wantRegion),
		},
	}

	k := credentialsRequestFakeClient(nil, lokistack, secret)
	req := ctrl.Request{
		NamespacedName: client.ObjectKey{Name: "my-stack", Namespace: "ns"},
	}

	managedAuth := &config.ManagedAuthConfig{
		Azure: &config.AzureEnvironment{
			ClientID:       "test-client-id",
			SubscriptionID: "test-tenant-id",
			TenantID:       "test-subscription-id",
		},
	}

	err := CreateCredentialsRequest(context.Background(), logger, scheme, managedAuth, k, req)
	require.NoError(t, err)

	require.Equal(t, 1, k.CreateCallCount())
	_, obj, _ := k.CreateArgsForCall(0)
	credReq, ok := obj.(*cloudcredentialv1.CredentialsRequest)
	require.True(t, ok)

	providerSpec := &cloudcredentialv1.AzureProviderSpec{}
	require.NoError(t, cloudcredentialv1.Codec.DecodeProviderSpec(credReq.Spec.ProviderSpec, providerSpec))

	require.Equal(t, wantRegion, providerSpec.AzureRegion)
}

func TestCreateCredentialsRequest_CreateNewResourceAzure_Errors(t *testing.T) {
	lokistack := &lokiv1.LokiStack{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "ns",
		},
	}
	req := ctrl.Request{
		NamespacedName: client.ObjectKey{Name: "my-stack", Namespace: "ns"},
	}

	tt := []struct {
		secret    *corev1.Secret
		wantError string
	}{
		{
			secret:    &corev1.Secret{},
			wantError: errAzureNoRegion.Error(),
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.wantError, func(t *testing.T) {
			t.Parallel()

			managedAuth := &config.ManagedAuthConfig{
				Azure: &config.AzureEnvironment{
					ClientID:       "test-client-id",
					SubscriptionID: "test-tenant-id",
					TenantID:       "test-subscription-id",
				},
			}
			k := credentialsRequestFakeClient(nil, lokistack, tc.secret)

			err := CreateCredentialsRequest(context.Background(), logger, scheme, managedAuth, k, req)
			require.EqualError(t, err, tc.wantError)
		})
	}
}

func TestCreateCredentialsRequest_DoNothing_WhenCredentialsRequestExist(t *testing.T) {
	req := ctrl.Request{
		NamespacedName: client.ObjectKey{Name: "my-stack", Namespace: "ns"},
	}

	managedAuth := &config.ManagedAuthConfig{
		AWS: &config.AWSEnvironment{
			RoleARN: "a-role-arn",
		},
	}

	cr := &cloudcredentialv1.CredentialsRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "ns",
		},
	}
	lokistack := &lokiv1.LokiStack{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "ns",
		},
	}

	k := credentialsRequestFakeClient(cr, lokistack, nil)

	err := CreateCredentialsRequest(context.Background(), logger, scheme, managedAuth, k, req)
	require.NoError(t, err)
	require.Equal(t, 2, k.GetCallCount())
	require.Equal(t, 0, k.CreateCallCount())
	require.Equal(t, 1, k.UpdateCallCount())
}
