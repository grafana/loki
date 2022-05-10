package status

import (
	"context"

	"github.com/ViaQ/logerr/v2/kverrors"
	lokiv1beta1 "github.com/grafana/loki/operator/api/v1beta1"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/manifests/storage"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SetStorageSchemaStatus updates the storage status component
func SetStorageSchemaStatus(ctx context.Context, k k8s.Client, req ctrl.Request, schemas []storage.Schema) error {
	var s lokiv1beta1.LokiStack
	if err := k.Get(ctx, req.NamespacedName, &s); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return kverrors.Wrap(err, "failed to lookup lokistack", "name", req.NamespacedName)
	}

	statuses := make([]lokiv1beta1.StorageSchemaStatus, len(schemas))

	for i, schema := range schemas {
		statuses[i] = lokiv1beta1.StorageSchemaStatus{
			DateApplied: schema.From,
			Version:     schema.Version,
		}
	}

	s.Status.Storage = lokiv1beta1.LokiStackStorageStatus{
		Schemas: statuses,
	}

	return k.Status().Update(ctx, &s, &client.UpdateOptions{})
}
