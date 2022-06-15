package status

import (
	"context"
	"fmt"

	"github.com/ViaQ/logerr/v2/kverrors"
	lokiv1beta1 "github.com/grafana/loki/operator/api/v1beta1"
	"github.com/grafana/loki/operator/internal/external/k8s"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DegradedError contains information about why the managed LokiStack has an invalid configuration.
type DegradedError struct {
	Message string
	Reason  lokiv1beta1.LokiStackConditionReason
	Requeue bool
}

func (e *DegradedError) Error() string {
	return fmt.Sprintf("cluster degraded: %s", e.Message)
}

// SetReadyCondition updates or appends the condition Ready to the lokistack status conditions.
// In addition it resets all other Status conditions to false.
func SetReadyCondition(ctx context.Context, k k8s.Client, req ctrl.Request) error {
	var s lokiv1beta1.LokiStack
	if err := k.Get(ctx, req.NamespacedName, &s); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return kverrors.Wrap(err, "failed to lookup lokistack", "name", req.NamespacedName)
	}

	for _, cond := range s.Status.Conditions {
		if cond.Type == string(lokiv1beta1.ConditionReady) && cond.Status == metav1.ConditionTrue {
			return nil
		}
	}

	ready := metav1.Condition{
		Type:               string(lokiv1beta1.ConditionReady),
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Message:            "All components ready",
		Reason:             string(lokiv1beta1.ReasonReadyComponents),
	}

	index := -1
	for i := range s.Status.Conditions {
		// Reset all other conditions first
		s.Status.Conditions[i].Status = metav1.ConditionFalse
		s.Status.Conditions[i].LastTransitionTime = metav1.Now()

		// Locate existing ready condition if any
		if s.Status.Conditions[i].Type == string(lokiv1beta1.ConditionReady) {
			index = i
		}
	}

	if index == -1 {
		s.Status.Conditions = append(s.Status.Conditions, ready)
	} else {
		s.Status.Conditions[index] = ready
	}

	return k.Status().Update(ctx, &s, &client.UpdateOptions{})
}

// SetFailedCondition updates or appends the condition Failed to the lokistack status conditions.
// In addition it resets all other Status conditions to false.
func SetFailedCondition(ctx context.Context, k k8s.Client, req ctrl.Request) error {
	var s lokiv1beta1.LokiStack
	if err := k.Get(ctx, req.NamespacedName, &s); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return kverrors.Wrap(err, "failed to lookup lokistack", "name", req.NamespacedName)
	}

	for _, cond := range s.Status.Conditions {
		if cond.Type == string(lokiv1beta1.ConditionFailed) && cond.Status == metav1.ConditionTrue {
			return nil
		}
	}

	failed := metav1.Condition{
		Type:               string(lokiv1beta1.ConditionFailed),
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Message:            "Some LokiStack components failed",
		Reason:             string(lokiv1beta1.ReasonFailedComponents),
	}

	index := -1
	for i := range s.Status.Conditions {
		// Reset all other conditions first
		s.Status.Conditions[i].Status = metav1.ConditionFalse
		s.Status.Conditions[i].LastTransitionTime = metav1.Now()

		// Locate existing failed condition if any
		if s.Status.Conditions[i].Type == string(lokiv1beta1.ConditionFailed) {
			index = i
		}
	}

	if index == -1 {
		s.Status.Conditions = append(s.Status.Conditions, failed)
	} else {
		s.Status.Conditions[index] = failed
	}

	return k.Status().Update(ctx, &s, &client.UpdateOptions{})
}

// SetPendingCondition updates or appends the condition Pending to the lokistack status conditions.
// In addition it resets all other Status conditions to false.
func SetPendingCondition(ctx context.Context, k k8s.Client, req ctrl.Request) error {
	var s lokiv1beta1.LokiStack
	if err := k.Get(ctx, req.NamespacedName, &s); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return kverrors.Wrap(err, "failed to lookup lokistack", "name", req.NamespacedName)
	}

	for _, cond := range s.Status.Conditions {
		if cond.Type == string(lokiv1beta1.ConditionPending) && cond.Status == metav1.ConditionTrue {
			return nil
		}
	}

	pending := metav1.Condition{
		Type:               string(lokiv1beta1.ConditionPending),
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Message:            "Some LokiStack components pending on dependendies",
		Reason:             string(lokiv1beta1.ReasonPendingComponents),
	}

	index := -1
	for i := range s.Status.Conditions {
		// Reset all other conditions first
		s.Status.Conditions[i].Status = metav1.ConditionFalse
		s.Status.Conditions[i].LastTransitionTime = metav1.Now()

		// Locate existing pending condition if any
		if s.Status.Conditions[i].Type == string(lokiv1beta1.ConditionPending) {
			index = i
		}
	}

	if index == -1 {
		s.Status.Conditions = append(s.Status.Conditions, pending)
	} else {
		s.Status.Conditions[index] = pending
	}

	return k.Status().Update(ctx, &s, &client.UpdateOptions{})
}

// SetDegradedCondition appends the condition Degraded to the lokistack status conditions.
func SetDegradedCondition(ctx context.Context, k k8s.Client, req ctrl.Request, msg string, reason lokiv1beta1.LokiStackConditionReason) error {
	var s lokiv1beta1.LokiStack
	if err := k.Get(ctx, req.NamespacedName, &s); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return kverrors.Wrap(err, "failed to lookup lokistack", "name", req.NamespacedName)
	}

	reasonStr := string(reason)
	for _, cond := range s.Status.Conditions {
		if cond.Type == string(lokiv1beta1.ConditionDegraded) && cond.Reason == reasonStr && cond.Status == metav1.ConditionTrue {
			return nil
		}
	}

	degraded := metav1.Condition{
		Type:               string(lokiv1beta1.ConditionDegraded),
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             reasonStr,
		Message:            msg,
	}

	index := -1
	for i := range s.Status.Conditions {
		// Reset all other conditions first
		s.Status.Conditions[i].Status = metav1.ConditionFalse
		s.Status.Conditions[i].LastTransitionTime = metav1.Now()

		// Locate existing pending condition if any
		if s.Status.Conditions[i].Type == string(lokiv1beta1.ConditionDegraded) {
			index = i
		}
	}

	if index == -1 {
		s.Status.Conditions = append(s.Status.Conditions, degraded)
	} else {
		s.Status.Conditions[index] = degraded
	}

	return k.Status().Update(ctx, &s, &client.UpdateOptions{})
}
