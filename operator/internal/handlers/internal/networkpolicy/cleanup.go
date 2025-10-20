package networkpolicy

import (
	"context"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/go-logr/logr"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
)

// Cleanup deletes operator-managed NetworkPolicies for a LokiStack
func Cleanup(ctx context.Context, log logr.Logger, c k8s.Client, req ctrl.Request, stack *lokiv1.LokiStack) error {
	ll := log.WithValues("lokistack", req.NamespacedName, "event", "cleanupNetworkPolicies")
	if stack.Spec.NetworkPolicies != nil && !stack.Spec.NetworkPolicies.Disabled {
		// Nothing to do - NetworkPolicies are enabled
		return nil
	}

	var list networkingv1.NetworkPolicyList
	if err := c.List(ctx, &list, client.InNamespace(stack.Namespace),
		client.MatchingLabels(map[string]string{
			"app.kubernetes.io/name":     "lokistack",
			"app.kubernetes.io/instance": stack.Name,
		}),
	); err != nil {
		return err
	}

	for _, np := range list.Items {
		hasOwnerRef, err := controllerutil.HasOwnerReference(np.OwnerReferences, stack, c.Scheme())
		if err != nil {
			ll.Error(err, "failed to check owner reference", "name", req.String())
			continue
		}
		if !hasOwnerRef {
			continue
		}

		if err := c.Delete(ctx, &np); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return kverrors.Wrap(err, "failed to delete NetworkPolicy", "name", req.String())
		}
	}
	ll.Info("deleted NetworkPolicies", "name", req.String())
	return nil
}
