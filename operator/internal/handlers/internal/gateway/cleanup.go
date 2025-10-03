package gateway

import (
	"context"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	routev1 "github.com/openshift/api/route/v1"
	networkingv1 "k8s.io/api/networking/v1"

	v1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/manifests"
)

// Cleanup removes external access resources (Routes/Ingress) when external access is disabled.
func Cleanup(ctx context.Context, log logr.Logger, k k8s.Client, stack *v1.LokiStack) error {
	if manifests.ExternalAccessEnabled(stack.Spec) {
		return nil
	}

	key := client.ObjectKeyFromObject(stack)

	if err := removeResource[*routev1.Route](ctx, k, stack, key, manifests.GatewayName(key.Name)); err != nil {
		return kverrors.Wrap(err, "failed to remove Route")
	}

	if err := removeResource[*networkingv1.Ingress](ctx, k, stack, key, manifests.GatewayName(key.Name)); err != nil {
		return kverrors.Wrap(err, "failed to remove Ingress")
	}

	return nil
}

func removeResource[T client.Object](ctx context.Context, c client.Client, stack *v1.LokiStack, key client.ObjectKey, resourceName string) error {
	resourceKey := client.ObjectKey{Name: resourceName, Namespace: key.Namespace}

	obj := new(T)
	if err := c.Get(ctx, resourceKey, *obj); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return kverrors.Wrap(err, "failed to lookup resource", "name", resourceKey)
	}

	hasOwnerRef, err := controllerutil.HasOwnerReference((*obj).GetOwnerReferences(), stack, c.Scheme())
	if err != nil {
		return kverrors.Wrap(err, "failed to check owner reference for resource")
	}
	if !hasOwnerRef {
		return nil
	}

	if err := c.Delete(ctx, *obj, &client.DeleteOptions{}); err != nil {
		return kverrors.Wrap(err, "failed to delete resource",
			"name", (*obj).GetName(),
			"namespace", (*obj).GetNamespace(),
		)
	}

	return nil
}
