package status

import (
	"context"

	lokiv1beta1 "github.com/ViaQ/loki-operator/api/v1beta1"
	"github.com/ViaQ/loki-operator/internal/external/k8s"
	"sigs.k8s.io/controller-runtime/pkg/client"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SetDegradedCondition appends the condition Degraded to the lokistack status conditions.
func SetDegradedCondition(ctx context.Context, k k8s.Client, s *lokiv1beta1.LokiStack, msg string, reason lokiv1beta1.LokiStackConditionReason) error {
	reasonStr := string(reason)
	for _, cond := range s.Status.Conditions {
		if cond.Type == string(lokiv1beta1.ConditionDegraded) && cond.Reason == reasonStr {
			return nil
		}
	}

	status := s.Status.DeepCopy()
	if status.Conditions == nil {
		status.Conditions = []metav1.Condition{}
	}

	degraded := metav1.Condition{
		Type:               string(lokiv1beta1.ConditionDegraded),
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             reasonStr,
		Message:            msg,
	}

	status.Conditions = append(status.Conditions, degraded)
	s.Status = *status
	return k.Status().Update(ctx, s, &client.UpdateOptions{})
}
