package v1beta1_test

import (
	"testing"

	v1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/api/loki/v1beta1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestConvertToV1_AlertingRule(t *testing.T) {
	tt := []struct {
		desc string
		src  v1beta1.AlertingRule
		want v1.AlertingRule
	}{
		{
			desc: "empty src(v1beta1) and dst(v1) AlertingRule",
			src:  v1beta1.AlertingRule{},
			want: v1.AlertingRule{},
		},
		{
			desc: "full conversion of src(v1beta1) to dst(v1) AlertingRule",
			src: v1beta1.AlertingRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "alerting-rule",
					Namespace: "mars",
					Labels: map[string]string{
						"app":     "loki",
						"part-of": "lokistack",
					},
					Annotations: map[string]string{
						"discoveredAt": "2022-06-28",
					},
				},
				Spec: v1beta1.AlertingRuleSpec{
					TenantID: "tenant-1",
					Groups: []*v1beta1.AlertingRuleGroup{
						{
							Name:     "group-1",
							Interval: v1beta1.PrometheusDuration("50s"),
							Limit:    49,
							Rules: []*v1beta1.AlertingRuleGroupSpec{
								{
									Alert: "alert-1",
									Expr:  "sum(increase(loki_panic_total[10m])) by (namespace, job) > 0",
									For:   "1m",
									Annotations: map[string]string{
										"foo": "bar",
										"tik": "tok",
									},
									Labels: map[string]string{
										"flip": "flop",
										"tic":  "tac",
									},
								},
								{
									Alert: "alert-2",
									Expr:  "sum(increase(loki_panic_total[10m])) by (namespace, job) < 0",
									For:   "2m",
									Annotations: map[string]string{
										"foo2": "bar2",
										"tik2": "tok2",
									},
									Labels: map[string]string{
										"flip2": "flop2",
										"tic2":  "tac2",
									},
								},
							},
						},
					},
				},
				Status: v1beta1.AlertingRuleStatus{
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
			want: v1.AlertingRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "alerting-rule",
					Namespace: "mars",
					Labels: map[string]string{
						"app":     "loki",
						"part-of": "lokistack",
					},
					Annotations: map[string]string{
						"discoveredAt": "2022-06-28",
					},
				},
				Spec: v1.AlertingRuleSpec{
					TenantID: "tenant-1",
					Groups: []*v1.AlertingRuleGroup{
						{
							Name:     "group-1",
							Interval: v1.PrometheusDuration("50s"),
							Limit:    49,
							Rules: []*v1.AlertingRuleGroupSpec{
								{
									Alert: "alert-1",
									Expr:  "sum(increase(loki_panic_total[10m])) by (namespace, job) > 0",
									For:   "1m",
									Annotations: map[string]string{
										"foo": "bar",
										"tik": "tok",
									},
									Labels: map[string]string{
										"flip": "flop",
										"tic":  "tac",
									},
								},
								{
									Alert: "alert-2",
									Expr:  "sum(increase(loki_panic_total[10m])) by (namespace, job) < 0",
									For:   "2m",
									Annotations: map[string]string{
										"foo2": "bar2",
										"tik2": "tok2",
									},
									Labels: map[string]string{
										"flip2": "flop2",
										"tic2":  "tac2",
									},
								},
							},
						},
					},
				},
				Status: v1.AlertingRuleStatus{
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
			dst := v1.AlertingRule{}
			err := tc.src.ConvertTo(&dst)
			require.NoError(t, err)
			require.Equal(t, dst, tc.want)
		})
	}
}

func TestConvertFromV1_AlertingRule(t *testing.T) {
	tt := []struct {
		desc string
		src  v1.AlertingRule
		want v1beta1.AlertingRule
	}{
		{
			desc: "empty src(v1) and dst(v1beta1) AlertingRule",
			src:  v1.AlertingRule{},
			want: v1beta1.AlertingRule{},
		},
		{
			desc: "full conversion of src(v1) to dst(v1beta1) AlertingRule",
			src: v1.AlertingRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "alerting-rule",
					Namespace: "mars",
					Labels: map[string]string{
						"app":     "loki",
						"part-of": "lokistack",
					},
					Annotations: map[string]string{
						"discoveredAt": "2022-06-28",
					},
				},
				Spec: v1.AlertingRuleSpec{
					TenantID: "tenant-1",
					Groups: []*v1.AlertingRuleGroup{
						{
							Name:     "group-1",
							Interval: v1.PrometheusDuration("50s"),
							Limit:    49,
							Rules: []*v1.AlertingRuleGroupSpec{
								{
									Alert: "alert-1",
									Expr:  "sum(increase(loki_panic_total[10m])) by (namespace, job) > 0",
									For:   "1m",
									Annotations: map[string]string{
										"foo": "bar",
										"tik": "tok",
									},
									Labels: map[string]string{
										"flip": "flop",
										"tic":  "tac",
									},
								},
								{
									Alert: "alert-2",
									Expr:  "sum(increase(loki_panic_total[10m])) by (namespace, job) < 0",
									For:   "2m",
									Annotations: map[string]string{
										"foo2": "bar2",
										"tik2": "tok2",
									},
									Labels: map[string]string{
										"flip2": "flop2",
										"tic2":  "tac2",
									},
								},
							},
						},
					},
				},
				Status: v1.AlertingRuleStatus{
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
			want: v1beta1.AlertingRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "alerting-rule",
					Namespace: "mars",
					Labels: map[string]string{
						"app":     "loki",
						"part-of": "lokistack",
					},
					Annotations: map[string]string{
						"discoveredAt": "2022-06-28",
					},
				},
				Spec: v1beta1.AlertingRuleSpec{
					TenantID: "tenant-1",
					Groups: []*v1beta1.AlertingRuleGroup{
						{
							Name:     "group-1",
							Interval: v1beta1.PrometheusDuration("50s"),
							Limit:    49,
							Rules: []*v1beta1.AlertingRuleGroupSpec{
								{
									Alert: "alert-1",
									Expr:  "sum(increase(loki_panic_total[10m])) by (namespace, job) > 0",
									For:   "1m",
									Annotations: map[string]string{
										"foo": "bar",
										"tik": "tok",
									},
									Labels: map[string]string{
										"flip": "flop",
										"tic":  "tac",
									},
								},
								{
									Alert: "alert-2",
									Expr:  "sum(increase(loki_panic_total[10m])) by (namespace, job) < 0",
									For:   "2m",
									Annotations: map[string]string{
										"foo2": "bar2",
										"tik2": "tok2",
									},
									Labels: map[string]string{
										"flip2": "flop2",
										"tic2":  "tac2",
									},
								},
							},
						},
					},
				},
				Status: v1beta1.AlertingRuleStatus{
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

			dst := v1beta1.AlertingRule{}
			err := dst.ConvertFrom(&tc.src)
			require.NoError(t, err)
			require.Equal(t, dst, tc.want)
		})
	}
}
