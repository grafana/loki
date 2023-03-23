package openshift

import (
	"context"
	"testing"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

func TestAlertingRuleValidator(t *testing.T) {
	var nilMap map[string]string

	tt := []struct {
		desc       string
		spec       *lokiv1.AlertingRule
		wantErrors field.ErrorList
	}{
		{
			desc: "success",
			spec: &lokiv1.AlertingRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "alerting-rule",
					Namespace: "example",
				},
				Spec: lokiv1.AlertingRuleSpec{
					TenantID: "application",
					Groups: []*lokiv1.AlertingRuleGroup{
						{
							Rules: []*lokiv1.AlertingRuleGroupSpec{
								{
									Expr: `sum(rate({kubernetes_namespace_name="example", level="error"}[5m])) by (job) > 0.1`,
									Labels: map[string]string{
										severityLabelName: "warning",
									},
									Annotations: map[string]string{
										summaryAnnotationName:     "alert summary",
										descriptionAnnotationName: "alert description",
									},
								},
							},
						},
					},
				},
			},
			wantErrors: nil,
		},
		{
			desc: "allow audit in openshift-logging",
			spec: &lokiv1.AlertingRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "alerting-rule",
					Namespace: "openshift-logging",
				},
				Spec: lokiv1.AlertingRuleSpec{
					TenantID: "audit",
					Groups: []*lokiv1.AlertingRuleGroup{
						{
							Rules: []*lokiv1.AlertingRuleGroupSpec{
								{
									Expr: `sum(rate({level="error"}[5m])) by (job) > 0.1`,
									Labels: map[string]string{
										severityLabelName: "warning",
									},
									Annotations: map[string]string{
										summaryAnnotationName:     "alert summary",
										descriptionAnnotationName: "alert description",
									},
								},
							},
						},
					},
				},
			},
			wantErrors: nil,
		},
		{
			desc: "wrong tenant",
			spec: &lokiv1.AlertingRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "alerting-rule",
					Namespace: "openshift-example",
				},
				Spec: lokiv1.AlertingRuleSpec{
					TenantID: "application",
					Groups: []*lokiv1.AlertingRuleGroup{
						{
							Rules: []*lokiv1.AlertingRuleGroupSpec{
								{
									Expr: `sum(rate({kubernetes_namespace_name="openshift-example", level="error"}[5m])) by (job) > 0.1`,
									Labels: map[string]string{
										severityLabelName: "warning",
									},
									Annotations: map[string]string{
										summaryAnnotationName:     "alert summary",
										descriptionAnnotationName: "alert description",
									},
								},
							},
						},
					},
				},
			},
			wantErrors: []*field.Error{
				{
					Type:     field.ErrorTypeInvalid,
					Field:    "spec.tenantID",
					BadValue: "application",
					Detail:   `AlertingRule does not use correct tenant ["infrastructure"]`,
				},
			},
		},
		{
			desc: "expression does not parse",
			spec: &lokiv1.AlertingRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "alerting-rule",
					Namespace: "example",
				},
				Spec: lokiv1.AlertingRuleSpec{
					TenantID: "application",
					Groups: []*lokiv1.AlertingRuleGroup{
						{
							Rules: []*lokiv1.AlertingRuleGroupSpec{
								{
									Expr: "invalid",
									Labels: map[string]string{
										severityLabelName: "warning",
									},
									Annotations: map[string]string{
										summaryAnnotationName:     "alert summary",
										descriptionAnnotationName: "alert description",
									},
								},
							},
						},
					},
				},
			},
			wantErrors: []*field.Error{
				{
					Type:     field.ErrorTypeInvalid,
					Field:    "spec.groups[0].rules[0].expr",
					BadValue: "invalid",
					Detail:   lokiv1.ErrParseLogQLExpression.Error(),
				},
			},
		},
		{
			desc: "expression does not produce samples",
			spec: &lokiv1.AlertingRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "alerting-rule",
					Namespace: "example",
				},
				Spec: lokiv1.AlertingRuleSpec{
					TenantID: "application",
					Groups: []*lokiv1.AlertingRuleGroup{
						{
							Rules: []*lokiv1.AlertingRuleGroupSpec{
								{
									Expr: `{kubernetes_namespace_name="example", level="error"}`,
									Labels: map[string]string{
										severityLabelName: "warning",
									},
									Annotations: map[string]string{
										summaryAnnotationName:     "alert summary",
										descriptionAnnotationName: "alert description",
									},
								},
							},
						},
					},
				},
			},
			wantErrors: []*field.Error{
				{
					Type:     field.ErrorTypeInvalid,
					Field:    "spec.groups[0].rules[0].expr",
					BadValue: `{kubernetes_namespace_name="example", level="error"}`,
					Detail:   lokiv1.ErrParseLogQLNotSample.Error(),
				},
			},
		},
		{
			desc: "no namespace matcher",
			spec: &lokiv1.AlertingRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "alerting-rule",
					Namespace: "example",
				},
				Spec: lokiv1.AlertingRuleSpec{
					TenantID: "application",
					Groups: []*lokiv1.AlertingRuleGroup{
						{
							Rules: []*lokiv1.AlertingRuleGroupSpec{
								{
									Expr: `sum(rate({level="error"}[5m])) by (job) > 0.1`,
									Labels: map[string]string{
										severityLabelName: "warning",
									},
									Annotations: map[string]string{
										summaryAnnotationName:     "alert summary",
										descriptionAnnotationName: "alert description",
									},
								},
							},
						},
					},
				},
			},
			wantErrors: []*field.Error{
				{
					Type:     field.ErrorTypeInvalid,
					Field:    "spec.groups[0].rules[0].expr",
					BadValue: `sum(rate({level="error"}[5m])) by (job) > 0.1`,
					Detail:   lokiv1.ErrRuleMustMatchNamespace.Error(),
				},
			},
		},
		{
			desc: "matcher does not match AlertingRule namespace",
			spec: &lokiv1.AlertingRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "alerting-rule",
					Namespace: "example",
				},
				Spec: lokiv1.AlertingRuleSpec{
					TenantID: "application",
					Groups: []*lokiv1.AlertingRuleGroup{
						{
							Rules: []*lokiv1.AlertingRuleGroupSpec{
								{
									Expr: `sum(rate({kubernetes_namespace_name="other-ns", level="error"}[5m])) by (job) > 0.1`,
									Labels: map[string]string{
										severityLabelName: "warning",
									},
									Annotations: map[string]string{
										summaryAnnotationName:     "alert summary",
										descriptionAnnotationName: "alert description",
									},
								},
							},
						},
					},
				},
			},
			wantErrors: []*field.Error{
				{
					Type:     field.ErrorTypeInvalid,
					Field:    "spec.groups[0].rules[0].expr",
					BadValue: `sum(rate({kubernetes_namespace_name="other-ns", level="error"}[5m])) by (job) > 0.1`,
					Detail:   lokiv1.ErrRuleMustMatchNamespace.Error(),
				},
			},
		},
		{
			desc: "missing severity label",
			spec: &lokiv1.AlertingRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "alerting-rule",
					Namespace: "example",
				},
				Spec: lokiv1.AlertingRuleSpec{
					TenantID: "application",
					Groups: []*lokiv1.AlertingRuleGroup{
						{
							Rules: []*lokiv1.AlertingRuleGroupSpec{
								{
									Expr: `sum(rate({kubernetes_namespace_name="example", level="error"}[5m])) by (job) > 0.1`,
									Annotations: map[string]string{
										summaryAnnotationName:     "alert summary",
										descriptionAnnotationName: "alert description",
									},
								},
							},
						},
					},
				},
			},
			wantErrors: []*field.Error{
				{
					Type:     field.ErrorTypeInvalid,
					Field:    "spec.groups[0].rules[0].labels",
					BadValue: nilMap,
					Detail:   lokiv1.ErrSeverityLabelMissing.Error(),
				},
			},
		},
		{
			desc: "invalid severity label",
			spec: &lokiv1.AlertingRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "alerting-rule",
					Namespace: "example",
				},
				Spec: lokiv1.AlertingRuleSpec{
					TenantID: "application",
					Groups: []*lokiv1.AlertingRuleGroup{
						{
							Rules: []*lokiv1.AlertingRuleGroupSpec{
								{
									Expr: `sum(rate({kubernetes_namespace_name="example", level="error"}[5m])) by (job) > 0.1`,
									Labels: map[string]string{
										severityLabelName: "page",
									},
									Annotations: map[string]string{
										summaryAnnotationName:     "alert summary",
										descriptionAnnotationName: "alert description",
									},
								},
							},
						},
					},
				},
			},
			wantErrors: []*field.Error{
				{
					Type:  field.ErrorTypeInvalid,
					Field: "spec.groups[0].rules[0].labels",
					BadValue: map[string]string{
						severityLabelName: "page",
					},
					Detail: lokiv1.ErrSeverityLabelInvalid.Error(),
				},
			},
		},
		{
			desc: "missing summary annotation",
			spec: &lokiv1.AlertingRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "alerting-rule",
					Namespace: "example",
				},
				Spec: lokiv1.AlertingRuleSpec{
					TenantID: "application",
					Groups: []*lokiv1.AlertingRuleGroup{
						{
							Rules: []*lokiv1.AlertingRuleGroupSpec{
								{
									Expr: `sum(rate({kubernetes_namespace_name="example", level="error"}[5m])) by (job) > 0.1`,
									Labels: map[string]string{
										severityLabelName: "warning",
									},
									Annotations: map[string]string{
										descriptionAnnotationName: "alert description",
									},
								},
							},
						},
					},
				},
			},
			wantErrors: []*field.Error{
				{
					Type:  field.ErrorTypeInvalid,
					Field: "spec.groups[0].rules[0].annotations",
					BadValue: map[string]string{
						descriptionAnnotationName: "alert description",
					},
					Detail: lokiv1.ErrSummaryAnnotationMissing.Error(),
				},
			},
		},
		{
			desc: "missing description annotation",
			spec: &lokiv1.AlertingRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "alerting-rule",
					Namespace: "example",
				},
				Spec: lokiv1.AlertingRuleSpec{
					TenantID: "application",
					Groups: []*lokiv1.AlertingRuleGroup{
						{
							Rules: []*lokiv1.AlertingRuleGroupSpec{
								{
									Expr: `sum(rate({kubernetes_namespace_name="example", level="error"}[5m])) by (job) > 0.1`,
									Labels: map[string]string{
										severityLabelName: "warning",
									},
									Annotations: map[string]string{
										summaryAnnotationName: "alert summary",
									},
								},
							},
						},
					},
				},
			},
			wantErrors: []*field.Error{
				{
					Type:  field.ErrorTypeInvalid,
					Field: "spec.groups[0].rules[0].annotations",
					BadValue: map[string]string{
						summaryAnnotationName: "alert summary",
					},
					Detail: lokiv1.ErrDescriptionAnnotationMissing.Error(),
				},
			},
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			errors := AlertingRuleValidator(ctx, tc.spec)
			require.Equal(t, tc.wantErrors, errors)
		})
	}
}
