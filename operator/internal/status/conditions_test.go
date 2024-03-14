package status

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
)

func TestMergeConditions(t *testing.T) {
	now := metav1.NewTime(time.Unix(0, 0))
	tt := []struct {
		desc       string
		old        []metav1.Condition
		active     []metav1.Condition
		wantMerged []metav1.Condition
	}{
		{
			desc: "set status and time",
			old:  []metav1.Condition{},
			active: []metav1.Condition{
				conditionReady,
			},
			wantMerged: []metav1.Condition{
				{
					Type:               conditionReady.Type,
					Status:             metav1.ConditionTrue,
					LastTransitionTime: now,
					Reason:             conditionReady.Reason,
					Message:            conditionReady.Message,
				},
			},
		},
		{
			desc: "reset old condition",
			old: []metav1.Condition{
				conditionPending,
			},
			active: []metav1.Condition{
				conditionReady,
			},
			wantMerged: []metav1.Condition{
				{
					Type:               conditionPending.Type,
					Status:             metav1.ConditionFalse,
					LastTransitionTime: now,
					Reason:             conditionPending.Reason,
					Message:            conditionPending.Message,
				},
				{
					Type:               conditionReady.Type,
					Status:             metav1.ConditionTrue,
					LastTransitionTime: now,
					Reason:             conditionReady.Reason,
					Message:            conditionReady.Message,
				},
			},
		},
		{
			desc: "keep active conditions",
			old: []metav1.Condition{
				{
					Type:               conditionReady.Type,
					Status:             metav1.ConditionTrue,
					LastTransitionTime: now,
					Reason:             conditionReady.Reason,
					Message:            conditionReady.Message,
				},
				{
					Type:               conditionPending.Type,
					Status:             metav1.ConditionFalse,
					LastTransitionTime: now,
					Reason:             conditionPending.Reason,
					Message:            conditionPending.Message,
				},
			},
			active: []metav1.Condition{
				conditionReady,
				{
					Type:    string(lokiv1.ConditionWarning),
					Reason:  "test-warning",
					Message: "test-warning-message",
				},
			},
			wantMerged: []metav1.Condition{
				{
					Type:               conditionReady.Type,
					Status:             metav1.ConditionTrue,
					LastTransitionTime: now,
					Reason:             conditionReady.Reason,
					Message:            conditionReady.Message,
				},
				{
					Type:               conditionPending.Type,
					Status:             metav1.ConditionFalse,
					LastTransitionTime: now,
					Reason:             conditionPending.Reason,
					Message:            conditionPending.Message,
				},
				{
					Type:               string(lokiv1.ConditionWarning),
					Status:             metav1.ConditionTrue,
					LastTransitionTime: now,
					Reason:             "test-warning",
					Message:            "test-warning-message",
				},
			},
		},
	}

	for _, tc := range tt {
		tc := tc

		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			beforeLenOld := len(tc.old)
			beforeLenActive := len(tc.active)

			merged := mergeConditions(tc.old, tc.active, now)

			afterLenOld := len(tc.old)
			afterLenActive := len(tc.active)

			if diff := cmp.Diff(merged, tc.wantMerged); diff != "" {
				t.Errorf("Merged conditions differ: -got+want\n%s", diff)
			}

			if beforeLenOld != afterLenOld {
				t.Errorf("old length differs: got %v, want %v", afterLenOld, beforeLenOld)
			}

			if beforeLenActive != afterLenActive {
				t.Errorf("active length differs: got %v, want %v", afterLenActive, beforeLenActive)
			}
		})
	}
}
