package status

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

func mergeConditions(old, active []metav1.Condition, now metav1.Time) []metav1.Condition {
	merged := make([]metav1.Condition, 0, len(old)+len(active))
	for len(old) > 0 {
		c := old[0]
		found := -1
		for i, ac := range active {
			if c.Type == ac.Type && c.Reason == ac.Reason {
				found = i
				break
			}
		}

		if found != -1 {
			c = active[found]
			active = append(active[:found], active[found+1:]...)

			c.Status = metav1.ConditionTrue
		} else {
			c.Status = metav1.ConditionFalse
		}

		c.LastTransitionTime = now
		merged = append(merged, c)
		old = old[1:]
	}

	for _, c := range active {
		c.Status = metav1.ConditionTrue
		c.LastTransitionTime = now
		merged = append(merged, c)
	}
	return merged
}
