package openshift

import (
	"testing"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAlertingRuleTenantLabels(t *testing.T) {
	tt := []struct {
		rule *lokiv1.AlertingRule
		want *lokiv1.AlertingRule
	}{
		{
			rule: &lokiv1.AlertingRule{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
				},
				Spec: lokiv1.AlertingRuleSpec{
					TenantID: tenantApplication,
					Groups: []*lokiv1.AlertingRuleGroup{
						{
							Name: "test-group",
							Rules: []*lokiv1.AlertingRuleGroupSpec{
								{
									Alert: "alert",
								},
							},
						},
					},
				},
			},
			want: &lokiv1.AlertingRule{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
				},
				Spec: lokiv1.AlertingRuleSpec{
					TenantID: tenantApplication,
					Groups: []*lokiv1.AlertingRuleGroup{
						{
							Name: "test-group",
							Rules: []*lokiv1.AlertingRuleGroupSpec{
								{
									Alert: "alert",
									Labels: map[string]string{
										opaDefaultLabelMatcher: "test-ns",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			rule: &lokiv1.AlertingRule{
				Spec: lokiv1.AlertingRuleSpec{
					TenantID: tenantInfrastructure,
					Groups: []*lokiv1.AlertingRuleGroup{
						{
							Name: "test-group",
							Rules: []*lokiv1.AlertingRuleGroupSpec{
								{
									Alert: "alert",
								},
							},
						},
					},
				},
			},
			want: &lokiv1.AlertingRule{
				Spec: lokiv1.AlertingRuleSpec{
					TenantID: tenantInfrastructure,
					Groups: []*lokiv1.AlertingRuleGroup{
						{
							Name: "test-group",
							Rules: []*lokiv1.AlertingRuleGroupSpec{
								{
									Alert: "alert",
								},
							},
						},
					},
				},
			},
		},
		{
			rule: &lokiv1.AlertingRule{
				Spec: lokiv1.AlertingRuleSpec{
					TenantID: tenantAudit,
					Groups: []*lokiv1.AlertingRuleGroup{
						{
							Name: "test-group",
							Rules: []*lokiv1.AlertingRuleGroupSpec{
								{
									Alert: "alert",
								},
							},
						},
					},
				},
			},
			want: &lokiv1.AlertingRule{
				Spec: lokiv1.AlertingRuleSpec{
					TenantID: tenantAudit,
					Groups: []*lokiv1.AlertingRuleGroup{
						{
							Name: "test-group",
							Rules: []*lokiv1.AlertingRuleGroupSpec{
								{
									Alert: "alert",
								},
							},
						},
					},
				},
			},
		},
		{
			rule: &lokiv1.AlertingRule{
				Spec: lokiv1.AlertingRuleSpec{
					TenantID: tenantNetwork,
					Groups: []*lokiv1.AlertingRuleGroup{
						{
							Name: "test-group",
							Rules: []*lokiv1.AlertingRuleGroupSpec{
								{
									Alert: "alert",
								},
							},
						},
					},
				},
			},
			want: &lokiv1.AlertingRule{
				Spec: lokiv1.AlertingRuleSpec{
					TenantID: tenantNetwork,
					Groups: []*lokiv1.AlertingRuleGroup{
						{
							Name: "test-group",
							Rules: []*lokiv1.AlertingRuleGroupSpec{
								{
									Alert: "alert",
								},
							},
						},
					},
				},
			},
		},
		{
			rule: &lokiv1.AlertingRule{
				Spec: lokiv1.AlertingRuleSpec{
					TenantID: "unknown",
					Groups: []*lokiv1.AlertingRuleGroup{
						{
							Name: "test-group",
							Rules: []*lokiv1.AlertingRuleGroupSpec{
								{
									Alert: "alert",
								},
							},
						},
					},
				},
			},
			want: &lokiv1.AlertingRule{
				Spec: lokiv1.AlertingRuleSpec{
					TenantID: "unknown",
					Groups: []*lokiv1.AlertingRuleGroup{
						{
							Name: "test-group",
							Rules: []*lokiv1.AlertingRuleGroupSpec{
								{
									Alert: "alert",
								},
							},
						},
					},
				},
			},
		},
	}
	for _, tc := range tt {
		tc := tc
		t.Run(tc.rule.Spec.TenantID, func(t *testing.T) {
			t.Parallel()
			AlertingRuleTenantLabels(tc.rule)

			require.Equal(t, tc.want, tc.rule)
		})
	}
}
