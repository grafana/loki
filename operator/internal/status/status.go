package status

import (
	"context"
	"time"

	"github.com/ViaQ/logerr/v2/kverrors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
)

// Refresh executes an aggregate update of the LokiStack Status struct, i.e.
// - It recreates the Status.Components pod status map per component.
// - It sets the appropriate Status.Condition to true that matches the pod status maps.
func Refresh(ctx context.Context, k k8s.Client, req ctrl.Request, now time.Time) error {
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

	condition := generateCondition(cs)

	condition.LastTransitionTime = metav1.NewTime(now)
	condition.Status = metav1.ConditionTrue

	statusUpdater := func(stack *lokiv1.LokiStack) {
		stack.Status.Components = *cs

		index := -1
		for i := range stack.Status.Conditions {
			// Reset all other conditions first
			stack.Status.Conditions[i].Status = metav1.ConditionFalse
			stack.Status.Conditions[i].LastTransitionTime = metav1.NewTime(now)

			// Locate existing pending condition if any
			if stack.Status.Conditions[i].Type == condition.Type {
				index = i
			}
		}

		if index == -1 {
			stack.Status.Conditions = append(stack.Status.Conditions, condition)
		} else {
			stack.Status.Conditions[index] = condition
		}
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
