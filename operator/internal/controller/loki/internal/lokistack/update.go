package lokistack

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
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
