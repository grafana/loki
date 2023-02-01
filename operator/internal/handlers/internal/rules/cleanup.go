package rules

import (
	"context"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/grafana/loki/operator/internal/manifests"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RemoveRulesConfigMap removes the rules configmap if it exists.
func RemoveRulesConfigMap(ctx context.Context, req ctrl.Request, c client.Client) error {
	// Check if the CM exists before proceeding.
	key := client.ObjectKey{Name: manifests.RulesConfigMapName(req.Name), Namespace: req.Namespace}

	var rulesCm corev1.ConfigMap
	if err := c.Get(ctx, key, &rulesCm); err != nil {
		if apierrors.IsNotFound(err) {
			// resource doesnt exist, so nothing to do.
			return nil
		}
		return kverrors.Wrap(err, "failed to lookup configmap", "name", key)
	}

	if err := c.Delete(ctx, &rulesCm, &client.DeleteOptions{}); err != nil {
		return kverrors.Wrap(err, "failed to delete configmap",
			"name", rulesCm.Name,
			"namespace", rulesCm.Namespace,
		)
	}

	return nil
}

// RemoveRuler removes the ruler statefulset if it exists.
func RemoveRuler(ctx context.Context, req ctrl.Request, c client.Client) error {
	// Check if the Statefulset exists before proceeding.
	key := client.ObjectKey{Name: manifests.RulerName(req.Name), Namespace: req.Namespace}

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
