package patterns

import (
	"context"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/manifests"
)

// Cleanup deletes the pattern ingester deployment in case it is disabled
func Cleanup(ctx context.Context, log logr.Logger, k k8s.Client, stack *lokiv1.LokiStack) error {
	if stack.Spec.PatternIngester != nil && stack.Spec.PatternIngester.Enabled {
		return nil
	}

	stackKey := client.ObjectKeyFromObject(stack)

	if err := removePatternIngester(ctx, k, stackKey); err != nil {
		log.Error(err, "failed to remove pattern ingester Statefulset")
		return err
	}

	return nil
}

func removePatternIngester(ctx context.Context, c client.Client, stack client.ObjectKey) error {
	key := client.ObjectKey{Name: manifests.PatternIngesterName(stack.Name), Namespace: stack.Namespace}

	var patternIngester appsv1.StatefulSet
	if err := c.Get(ctx, key, &patternIngester); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return kverrors.Wrap(err, "failed to lookup StatefulSet", "name", key)
	}

	if err := c.Delete(ctx, &patternIngester, &client.DeleteOptions{}); err != nil {
		return kverrors.Wrap(err, "failed to delete deployment",
			"name", patternIngester.Name,
			"namespace", patternIngester.Namespace,
		)
	}

	return nil
}
