package openshift

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
)

func TestRecordingRuleTenantLabels(t *testing.T) {
	tt := []struct {
		rule *lokiv1.RecordingRule
		want *lokiv1.RecordingRule
	}{
		{
			rule: &lokiv1.RecordingRule{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
				},
				Spec: lokiv1.RecordingRuleSpec{
					TenantID: tenantApplication,
					Groups: []*lokiv1.RecordingRuleGroup{
						{
							Name: "test-group",
							Rules: []*lokiv1.RecordingRuleGroupSpec{
								{
									Record: "record",
								},
							},
						},
					},
				},
			},
			want: &lokiv1.RecordingRule{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
				},
				Spec: lokiv1.RecordingRuleSpec{
					TenantID: tenantApplication,
					Groups: []*lokiv1.RecordingRuleGroup{
						{
							Name: "test-group",
							Rules: []*lokiv1.RecordingRuleGroupSpec{
								{
									Record: "record",
									Labels: map[string]string{
										opaDefaultLabelMatcher:    "test-ns",
										ocpMonitoringGroupByLabel: "test-ns",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			rule: &lokiv1.RecordingRule{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
				},
				Spec: lokiv1.RecordingRuleSpec{
					TenantID: tenantInfrastructure,
					Groups: []*lokiv1.RecordingRuleGroup{
						{
							Name: "test-group",
							Rules: []*lokiv1.RecordingRuleGroupSpec{
								{
									Record: "record",
								},
							},
						},
					},
				},
			},
			want: &lokiv1.RecordingRule{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
				},
				Spec: lokiv1.RecordingRuleSpec{
					TenantID: tenantInfrastructure,
					Groups: []*lokiv1.RecordingRuleGroup{
						{
							Name: "test-group",
							Rules: []*lokiv1.RecordingRuleGroupSpec{
								{
									Record: "record",
									Labels: map[string]string{
										ocpMonitoringGroupByLabel: "test-ns",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			rule: &lokiv1.RecordingRule{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
				},
				Spec: lokiv1.RecordingRuleSpec{
					TenantID: tenantAudit,
					Groups: []*lokiv1.RecordingRuleGroup{
						{
							Name: "test-group",
							Rules: []*lokiv1.RecordingRuleGroupSpec{
								{
									Record: "record",
								},
							},
						},
					},
				},
			},
			want: &lokiv1.RecordingRule{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
				},
				Spec: lokiv1.RecordingRuleSpec{
					TenantID: tenantAudit,
					Groups: []*lokiv1.RecordingRuleGroup{
						{
							Name: "test-group",
							Rules: []*lokiv1.RecordingRuleGroupSpec{
								{
									Record: "record",
									Labels: map[string]string{
										ocpMonitoringGroupByLabel: "test-ns",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			rule: &lokiv1.RecordingRule{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
				},
				Spec: lokiv1.RecordingRuleSpec{
					TenantID: tenantNetwork,
					Groups: []*lokiv1.RecordingRuleGroup{
						{
							Name: "test-group",
							Rules: []*lokiv1.RecordingRuleGroupSpec{
								{
									Record: "record",
								},
							},
						},
					},
				},
			},
			want: &lokiv1.RecordingRule{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
				},
				Spec: lokiv1.RecordingRuleSpec{
					TenantID: tenantNetwork,
					Groups: []*lokiv1.RecordingRuleGroup{
						{
							Name: "test-group",
							Rules: []*lokiv1.RecordingRuleGroupSpec{
								{
									Record: "record",
									Labels: map[string]string{
										ocpMonitoringGroupByLabel: "test-ns",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			rule: &lokiv1.RecordingRule{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
				},
				Spec: lokiv1.RecordingRuleSpec{
					TenantID: "unknown",
					Groups: []*lokiv1.RecordingRuleGroup{
						{
							Name: "test-group",
							Rules: []*lokiv1.RecordingRuleGroupSpec{
								{
									Record: "record",
								},
							},
						},
					},
				},
			},
			want: &lokiv1.RecordingRule{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
				},
				Spec: lokiv1.RecordingRuleSpec{
					TenantID: "unknown",
					Groups: []*lokiv1.RecordingRuleGroup{
						{
							Name: "test-group",
							Rules: []*lokiv1.RecordingRuleGroupSpec{
								{
									Record: "record",
								},
							},
						},
					},
				},
			},
		},
	}
	for _, tc := range tt {
		t.Run(tc.rule.Spec.TenantID, func(t *testing.T) {
			t.Parallel()
			RecordingRuleTenantLabels(tc.rule)

			require.Equal(t, tc.want, tc.rule)
		})
	}
}
