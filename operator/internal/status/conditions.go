package status

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type conditionKey struct {
	Type   string
	Reason string
}

// inactiveConditionMessage replaces the message of a condition that is no
// longer active. The previous message describes the active state ("components
// are pending", "all components ready") and is misleading once Status is False.
const inactiveConditionMessage = "Condition is not currently active"

func mergeConditions(old, active []metav1.Condition, now metav1.Time) []metav1.Condition {
	conditions := map[conditionKey]bool{}
	merged := make([]metav1.Condition, 0, len(old)+len(active))
	for _, c := range active {
		c.Status = metav1.ConditionTrue
		c.LastTransitionTime = now

		merged = append(merged, c)
		conditions[conditionKey{Type: c.Type, Reason: c.Reason}] = true
	}

	for _, c := range old {
		if conditions[conditionKey{c.Type, c.Reason}] {
			continue
		}

		if c.Status != metav1.ConditionFalse {
			c.LastTransitionTime = now
			c.Status = metav1.ConditionFalse
		}
		// Rewrite the message on every reconcile, including conditions that are
		// already False. Otherwise a stale active-state message stays forever
		// after the status has transitioned.
		c.Message = inactiveConditionMessage

		merged = append(merged, c)
		conditions[conditionKey{c.Type, c.Reason}] = true
	}

	return merged
}
