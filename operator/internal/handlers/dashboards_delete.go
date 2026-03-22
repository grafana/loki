package handlers

import (
	"context"

	"github.com/ViaQ/logerr/v2/kverrors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/manifests/openshift"
)

// DeleteDashboards removes all cluster-scoped dashboard resources.
func DeleteDashboards(ctx context.Context, k k8s.Client, operatorNs string) error {
	objs, err := openshift.BuildDashboards(operatorNs)
	if err != nil {
		return kverrors.Wrap(err, "failed to build dashboards manifests")
	}

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
