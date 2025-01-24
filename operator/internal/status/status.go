package status

import (
	"context"
	"time"

	"github.com/ViaQ/logerr/v2/kverrors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
)

// Refresh executes an aggregate update of the LokiStack Status struct, i.e.
// - It recreates the Status.Components pod status map per component.
// - It sets the appropriate Status.Condition to true that matches the pod status maps.
func Refresh(ctx context.Context, k k8s.Client, req ctrl.Request, now time.Time, credentialMode lokiv1.CredentialMode, degradedErr *DegradedError) error {
	var stack lokiv1.LokiStack
	if err := k.Get(ctx, req.NamespacedName, &stack); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return kverrors.Wrap(err, "failed to lookup lokistack", "name", req.NamespacedName)
	}

	cs, err := generateComponentStatus(ctx, k, &stack)
	if err != nil {
		return err
	}

	activeConditions, err := generateConditions(ctx, cs, k, &stack, degradedErr)
	if err != nil {
		return err
	}

	metaTime := metav1.NewTime(now)
	for _, c := range activeConditions {
		c.LastTransitionTime = metaTime
		c.Status = metav1.ConditionTrue
	}

	statusUpdater := func(stack *lokiv1.LokiStack) {
		stack.Status.Components = *cs
		stack.Status.Conditions = mergeConditions(stack.Status.Conditions, activeConditions, metaTime)
		stack.Status.Storage.CredentialMode = credentialMode
	}

	statusUpdater(&stack)
	err = k.Status().Update(ctx, &stack)
	switch {
	case err == nil:
		return nil
	case apierrors.IsConflict(err):
		// break into retry-logic below on conflict
		break
	case err != nil:
		// return non-conflict errors
		return err
	}

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := k.Get(ctx, req.NamespacedName, &stack); err != nil {
			return err
		}

		statusUpdater(&stack)
		return k.Status().Update(ctx, &stack)
	})
}
