package gateway

import (
	"context"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/go-logr/logr"
	routev1 "github.com/openshift/api/route/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	v1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
)

// Cleanup removes external access resources (Routes/Ingress) when external access is disabled.
func Cleanup(ctx context.Context, log logr.Logger, k k8s.Client, stack *v1.LokiStack) error {
	if stack.Spec.Tenants == nil || !stack.Spec.Tenants.DisableIngress {
		return nil
	}

	key := client.ObjectKeyFromObject(stack)

	if err := removeRoute(ctx, k, stack, key, key.Name); err != nil {
		return kverrors.Wrap(err, "failed to remove Route")
	}

	if err := removeIngress(ctx, k, stack, key, key.Name); err != nil {
		return kverrors.Wrap(err, "failed to remove Ingress")
	}

	return nil
}

func removeRoute(ctx context.Context, c client.Client, stack *v1.LokiStack, key client.ObjectKey, resourceName string) error {
	resourceKey := client.ObjectKey{Name: resourceName, Namespace: key.Namespace}

	var route routev1.Route
	if err := c.Get(ctx, resourceKey, &route); err != nil {
		if meta.IsNoMatchError(err) || runtime.IsNotRegisteredError(err) || apierrors.IsNotFound(err) {
			return nil
		}
		return kverrors.Wrap(err, "failed to lookup resource", "name", resourceKey)
	}

	return checkOwnerAndDelete(ctx, c, stack, &route)
}

func removeIngress(ctx context.Context, c client.Client, stack *v1.LokiStack, key client.ObjectKey, resourceName string) error {
	resourceKey := client.ObjectKey{Name: resourceName, Namespace: key.Namespace}

	var ingress networkingv1.Ingress
	if err := c.Get(ctx, resourceKey, &ingress); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return kverrors.Wrap(err, "failed to lookup resource", "name", resourceKey)
	}

	return checkOwnerAndDelete(ctx, c, stack, &ingress)
}

func checkOwnerAndDelete(ctx context.Context, c client.Client, stack *v1.LokiStack, obj client.Object) error {
	hasOwnerRef, err := controllerutil.HasOwnerReference(obj.GetOwnerReferences(), stack, c.Scheme())
	if err != nil {
		return kverrors.Wrap(err, "failed to check owner reference for resource")
	}
	if !hasOwnerRef {
		return nil
	}

	if err := c.Delete(ctx, obj, &client.DeleteOptions{}); err != nil {
		return kverrors.Wrap(err, "failed to delete resource",
			"name", obj.GetName(),
			"namespace", obj.GetNamespace(),
		)
	}

	return nil
}
