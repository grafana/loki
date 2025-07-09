package rules

import (
	"context"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/manifests"
)

// Cleanup removes the ruler component's statefulset and configmaps if available, or
// else it returns an error to retry the reconciliation loop.
func Cleanup(ctx context.Context, log logr.Logger, k k8s.Client, stack *v1.LokiStack) error {
	if stack.Spec.Rules != nil && stack.Spec.Rules.Enabled {
		return nil
	}

	stackKey := client.ObjectKeyFromObject(stack)

	// Clean up ruler resources
	if err := removeRulesConfigMap(ctx, k, stackKey); err != nil {
		log.Error(err, "failed to remove rules ConfigMap")
		return err
	}

	if err := removeRuler(ctx, k, stackKey); err != nil {
		log.Error(err, "failed to remove ruler StatefulSet")
		return err
	}

	return nil
}

func removeRulesConfigMap(ctx context.Context, c client.Client, key client.ObjectKey) error {
	var rulesCmList corev1.ConfigMapList

	err := c.List(ctx, &rulesCmList, &client.ListOptions{
		Namespace: key.Namespace,
		LabelSelector: labels.SelectorFromSet(labels.Set{
			"app.kubernetes.io/component": manifests.LabelRulerComponent,
			"app.kubernetes.io/instance":  key.Name,
		}),
	})
	if err != nil {
		return err
	}

	for _, rulesCm := range rulesCmList.Items {
		if err := c.Delete(ctx, &rulesCm, &client.DeleteOptions{}); err != nil {
			return kverrors.Wrap(err, "failed to delete ConfigMap",
				"name", rulesCm.Name,
				"namespace", rulesCm.Namespace,
			)
		}
	}

	return nil
}

func removeRuler(ctx context.Context, c client.Client, stack client.ObjectKey) error {
	// Check if the Statefulset exists before proceeding.
	key := client.ObjectKey{Name: manifests.RulerName(stack.Name), Namespace: stack.Namespace}

	var ruler appsv1.StatefulSet
	if err := c.Get(ctx, key, &ruler); err != nil {
		if apierrors.IsNotFound(err) {
			// resource doesnt exist, so nothing to do.
			return nil
		}
		return kverrors.Wrap(err, "failed to lookup Statefulset", "name", key)
	}

	if err := c.Delete(ctx, &ruler, &client.DeleteOptions{}); err != nil {
		return kverrors.Wrap(err, "failed to delete statefulset",
			"name", ruler.Name,
			"namespace", ruler.Namespace,
		)
	}

	return nil
}
