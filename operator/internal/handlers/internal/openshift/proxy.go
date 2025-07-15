package openshift

import (
	"context"

	configv1 "github.com/openshift/api/config/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
)

const proxyName = "cluster"

// GetProxy returns the cluster-wide proxy configuration of OpenShift, if one is set.
// It can also return an error.
func GetProxy(ctx context.Context, k k8s.Client) (*lokiv1.ClusterProxy, error) {
	key := client.ObjectKey{Name: proxyName}
	p := &configv1.Proxy{}
	if err := k.Get(ctx, key, p); err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	if p.Status.HTTPProxy == "" && p.Status.HTTPSProxy == "" && p.Status.NoProxy == "" {
		return nil, nil
	}

	return &lokiv1.ClusterProxy{
		HTTPProxy:  p.Status.HTTPProxy,
		HTTPSProxy: p.Status.HTTPSProxy,
		NoProxy:    p.Status.NoProxy,
	}, nil
}
