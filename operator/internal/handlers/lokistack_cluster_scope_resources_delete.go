package handlers

import (
	"context"

	"github.com/ViaQ/logerr/v2/kverrors"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/manifests"
	"github.com/grafana/loki/operator/internal/manifests/openshift"
)

// DeleteClusterScopedResources removes all cluster-scoped resources.
func DeleteClusterScopedResources(ctx context.Context, k k8s.Client, operatorNs string, stacks lokiv1.LokiStackList) error {
	opts := openshift.NewOptionsClusterScope(operatorNs, manifests.ClusterScopeLabels(), []rbacv1.Subject{}, []rbacv1.Subject{})
	objs, err := openshift.BuildDashboards(opts)
	if err != nil {
		return kverrors.Wrap(err, "failed to build dashboards manifests")
	}

	// Delete all re

	for _, obj := range objs {
		key := client.ObjectKeyFromObject(obj)
		if err := k.Delete(ctx, obj, &client.DeleteOptions{}); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return kverrors.Wrap(err, "failed to delete dashboard", "key", key)
		}
	}
	return nil
}
