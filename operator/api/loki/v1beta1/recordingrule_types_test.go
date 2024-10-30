package v1beta1_test

import (
	"testing"

	v1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/api/loki/v1beta1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestConvertToV1_RecordingRule(t *testing.T) {
	tt := []struct {
		desc string
		src  v1beta1.RecordingRule
		want v1.RecordingRule
	}{
		{
			desc: "empty src(v1beta1) and dst(v1) RecordingRule",
			src:  v1beta1.RecordingRule{},
			want: v1.RecordingRule{},
		},
		{
			desc: "full conversion of src(v1beta1) to dst(v1) RecordingRule",
			src: v1beta1.RecordingRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "recording-rule",
					Namespace: "mars",
					Labels: map[string]string{
						"app":     "loki",
						"part-of": "lokistack",
					},
					Annotations: map[string]string{
						"discoveredAt": "2022-06-28",
					},
				},
				Spec: v1beta1.RecordingRuleSpec{
					TenantID: "tenant-1",
					Groups: []*v1beta1.RecordingRuleGroup{
						{
							Name:     "group-1",
							Interval: v1beta1.PrometheusDuration("50s"),
							Limit:    49,
							Rules: []*v1beta1.RecordingRuleGroupSpec{
								{
									Record: "record-1",
									Expr:   "sum(increase(loki_panic_total[10m])) by (namespace, job) > 0",
								},
								{
									Record: "record-2",
									Expr:   "sum(increase(loki_panic_total[10m])) by (namespace, job) < 0",
								},
							},
						},
					},
				},
				Status: v1beta1.RecordingRuleStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(v1.ConditionReady),
							Status: metav1.ConditionTrue,
						},
						{
							Type:   string(v1.ConditionPending),
							Status: metav1.ConditionFalse,
						},
					},
				},
			},
			want: v1.RecordingRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "recording-rule",
					Namespace: "mars",
					Labels: map[string]string{
						"app":     "loki",
						"part-of": "lokistack",
					},
					Annotations: map[string]string{
						"discoveredAt": "2022-06-28",
					},
				},
				Spec: v1.RecordingRuleSpec{
					TenantID: "tenant-1",
					Groups: []*v1.RecordingRuleGroup{
						{
							Name:     "group-1",
							Interval: v1.PrometheusDuration("50s"),
							Limit:    49,
							Rules: []*v1.RecordingRuleGroupSpec{
								{
									Record: "record-1",
									Expr:   "sum(increase(loki_panic_total[10m])) by (namespace, job) > 0",
								},
								{
									Record: "record-2",
									Expr:   "sum(increase(loki_panic_total[10m])) by (namespace, job) < 0",
								},
							},
						},
					},
				},
				Status: v1.RecordingRuleStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(v1.ConditionReady),
							Status: metav1.ConditionTrue,
						},
						{
							Type:   string(v1.ConditionPending),
							Status: metav1.ConditionFalse,
						},
					},
				},
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			dst := v1.RecordingRule{}
			err := tc.src.ConvertTo(&dst)
			require.NoError(t, err)
			require.Equal(t, dst, tc.want)
		})
	}
}

func TestConvertFromV1_RecordingRule(t *testing.T) {
	tt := []struct {
		desc string
		src  v1.RecordingRule
		want v1beta1.RecordingRule
	}{
		{
			desc: "empty src(v1) and dst(v1beta1) RecordingRule",
			src:  v1.RecordingRule{},
			want: v1beta1.RecordingRule{},
		},
		{
			desc: "full conversion of src(v1) to dst(v1beta1) RecordingRule",
			src: v1.RecordingRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "recording-rule",
					Namespace: "mars",
					Labels: map[string]string{
						"app":     "loki",
						"part-of": "lokistack",
					},
					Annotations: map[string]string{
						"discoveredAt": "2022-06-28",
					},
				},
				Spec: v1.RecordingRuleSpec{
					TenantID: "tenant-1",
					Groups: []*v1.RecordingRuleGroup{
						{
							Name:     "group-1",
							Interval: v1.PrometheusDuration("50s"),
							Limit:    49,
							Rules: []*v1.RecordingRuleGroupSpec{
								{
									Record: "record-1",
									Expr:   "sum(increase(loki_panic_total[10m])) by (namespace, job) > 0",
								},
								{
									Record: "record-2",
									Expr:   "sum(increase(loki_panic_total[10m])) by (namespace, job) < 0",
								},
							},
						},
					},
				},
				Status: v1.RecordingRuleStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(v1.ConditionReady),
							Status: metav1.ConditionTrue,
						},
						{
							Type:   string(v1.ConditionPending),
							Status: metav1.ConditionFalse,
						},
					},
				},
			},
			want: v1beta1.RecordingRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "recording-rule",
					Namespace: "mars",
					Labels: map[string]string{
						"app":     "loki",
						"part-of": "lokistack",
					},
					Annotations: map[string]string{
						"discoveredAt": "2022-06-28",
					},
				},
				Spec: v1beta1.RecordingRuleSpec{
					TenantID: "tenant-1",
					Groups: []*v1beta1.RecordingRuleGroup{
						{
							Name:     "group-1",
							Interval: v1beta1.PrometheusDuration("50s"),
							Limit:    49,
							Rules: []*v1beta1.RecordingRuleGroupSpec{
								{
									Record: "record-1",
									Expr:   "sum(increase(loki_panic_total[10m])) by (namespace, job) > 0",
								},
								{
									Record: "record-2",
									Expr:   "sum(increase(loki_panic_total[10m])) by (namespace, job) < 0",
								},
							},
						},
					},
				},
				Status: v1beta1.RecordingRuleStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(v1.ConditionReady),
							Status: metav1.ConditionTrue,
						},
						{
							Type:   string(v1.ConditionPending),
							Status: metav1.ConditionFalse,
						},
					},
				},
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			dst := v1beta1.RecordingRule{}
			err := dst.ConvertFrom(&tc.src)
			require.NoError(t, err)
			require.Equal(t, dst, tc.want)
		})
	}
}
