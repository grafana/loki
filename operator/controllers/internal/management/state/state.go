package state

import (
	"context"

	lokiv1beta1 "github.com/grafana/loki/operator/api/v1beta1"
	"github.com/grafana/loki/operator/internal/external/k8s"

	"github.com/ViaQ/logerr/v2/kverrors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
)

// IsManaged checks if the custom resource is configured with ManagementState Managed.
func IsManaged(ctx context.Context, req ctrl.Request, k k8s.Client) (bool, error) {
	var stack lokiv1beta1.LokiStack
	if err := k.Get(ctx, req.NamespacedName, &stack); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, kverrors.Wrap(err, "failed to lookup lokistack", "name", req.NamespacedName)
	}
	return stack.Spec.ManagementState == lokiv1beta1.ManagementStateManaged, nil
}
