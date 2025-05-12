package handlers

import (
	"context"

	"github.com/ViaQ/logerr/v2/kverrors"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/manifests"
	"github.com/grafana/loki/operator/internal/manifests/openshift"
)

// DeleteClusterScopedResources removes all cluster-scoped resources.
func DeleteClusterScopedResources(ctx context.Context, k k8s.Client, operatorNs string) error {
	// Since we are deleting we don't need to worry about the subjects.
	opts := openshift.NewOptionsClusterScope(operatorNs, manifests.ClusterScopeLabels(), []rbacv1.Subject{})

	objs := openshift.BuildRBAC(opts)
	objs = append(objs, openshift.BuildDashboards(opts.OperatorNs)...)

	for _, obj := range objs {
		if err := k.Delete(ctx, obj, &client.DeleteOptions{}); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return kverrors.Wrap(err, "failed to delete dashboard", "kind", obj.GetObjectKind(), "key", client.ObjectKeyFromObject(obj))
		}
	}
	return nil
}
