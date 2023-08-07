package handlers

import (
	"context"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/manifests/openshift"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DeleteDashboards removes all cluster-scoped dashboard resources.
func DeleteDashboards(ctx context.Context, k k8s.Client, req ctrl.Request) error {
	objs, err := openshift.BuildDashboards(openshift.Options{
		BuildOpts: openshift.BuildOptions{
			LokiStackName:      req.Name,
			LokiStackNamespace: req.Namespace,
		},
	})
	if err != nil {
		return kverrors.Wrap(err, "failed to build dashboards manifests", "req", req)
	}

	for _, obj := range objs {
		// Skip objects managed by owner references, e.g. PrometheusRules
		var skip bool
		for _, ref := range obj.GetOwnerReferences() {
			if ref.Kind == "LokiStack" && ref.Name == req.Name {
				skip = true
				break
			}
		}

		if skip {
			continue
		}

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
