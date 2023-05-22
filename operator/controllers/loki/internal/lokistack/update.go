package lokistack

import (
	"context"

	"github.com/ViaQ/logerr/v2/kverrors"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/manifests"
)

func updateAnnotation(ctx context.Context, k k8s.Client, stack *lokiv1.LokiStack, key, value string) error {
	if stack.Annotations == nil {
		stack.Annotations = make(map[string]string)
	}
	stack.Annotations[key] = value

	err := k.Update(ctx, stack)
	switch {
	case err == nil:
		return nil
	case errors.IsConflict(err):
		// break into retry logic below on conflict
		break
	case err != nil:
		return err
	}

	objectKey := client.ObjectKeyFromObject(stack)
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := k.Get(ctx, objectKey, stack); err != nil {
			return err
		}

		if stack.Annotations == nil {
			stack.Annotations = make(map[string]string)
		}
		stack.Annotations[key] = value

		return k.Update(ctx, stack)
	})
}

func removeAnnotation(ctx context.Context, k k8s.Client, stack *lokiv1.LokiStack, key string) error {
	if stack.Annotations == nil {
		return nil
	}
	delete(stack.Annotations, key)

	err := k.Update(ctx, stack)
	switch {
	case err == nil:
		return nil
	case errors.IsConflict(err):
		// break into retry logic below on conflict
		break
	case err != nil:
		return err
	}

	objectKey := client.ObjectKeyFromObject(stack)
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := k.Get(ctx, objectKey, stack); err != nil {
			return err
		}

		if stack.Annotations == nil {
			return nil
		}
		delete(stack.Annotations, key)

		return k.Update(ctx, stack)
	})
}

func updateRulerAnnotation(ctx context.Context, k k8s.Client, stack *lokiv1.LokiStack, key, value string) error {
	stsKey := client.ObjectKey{Name: manifests.RulerName(stack.Name), Namespace: stack.Namespace}
	var rulerSts appsv1.StatefulSet
	if err := k.Get(ctx, stsKey, &rulerSts); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return kverrors.Wrap(err, "failed to lookup Statefulset", "name", key)
	}

	rulerSts.Spec.Template.ObjectMeta.Annotations[key] = value

	return k.Update(ctx, &rulerSts)
}
