package openshift

import (
	"context"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
)

func TestGetProxy_ReturnError_WhenOtherThanNotFound(t *testing.T) {
	k := &k8sfakes.FakeClient{}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		return apierrors.NewBadRequest("bad request")
	}

	_, err := GetProxy(context.TODO(), k)
	require.Error(t, err)
}

func TestGetProxy_ReturnEmpty_WhenNotFound(t *testing.T) {
	k := &k8sfakes.FakeClient{}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	proxy, err := GetProxy(context.TODO(), k)
	require.NoError(t, err)
	require.Nil(t, proxy)
}

func TestGetProxy_ReturnEnvVars_WhenProxyExists(t *testing.T) {
	k := &k8sfakes.FakeClient{}

	k.GetStub = func(_ context.Context, name types.NamespacedName, out client.Object, _ ...client.GetOption) error {
		if name.Name == proxyName {
			k.SetClientObject(out, &configv1.Proxy{
				Status: configv1.ProxyStatus{
					HTTPProxy:  "http-test",
					HTTPSProxy: "https-test",
					NoProxy:    "noproxy-test",
				},
			})
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	proxy, err := GetProxy(context.TODO(), k)
	require.NoError(t, err)
	require.NotNil(t, proxy)
	require.Equal(t, "http-test", proxy.HTTPProxy)
	require.Equal(t, "https-test", proxy.HTTPSProxy)
	require.Equal(t, "noproxy-test", proxy.NoProxy)
}
