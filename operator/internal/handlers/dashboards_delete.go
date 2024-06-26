package handlers

import (
	"context"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/manifests/openshift"
)

// DeleteDashboards removes all cluster-scoped dashboard resources.
func DeleteDashboards(ctx context.Context, log logr.Logger, k k8s.Client, operatorNs string) error {
	ll := log.WithValues("event", eventDeleteDashboards)

	objs, err := openshift.BuildDashboards(operatorNs)
	if err != nil {
		return kverrors.Wrap(err, "failed to build dashboards manifests")
	}

	for _, obj := range objs {
		l := ll.WithValues(
			"object_name", obj.GetName(),
			"object_kind", obj.GetObjectKind(),
		)

		key := client.ObjectKeyFromObject(obj)
		if err := k.Delete(ctx, obj, &client.DeleteOptions{}); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return kverrors.Wrap(err, "failed to delete dashboard", "key", klog.KRef(key.Namespace, key.Name))
		}

		l.Info("successfully deleted dashboard")
	}
	return nil
}
