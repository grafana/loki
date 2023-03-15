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
								},
							},
						},
					},
				},
			},
			wantErrors: []*field.Error{
				{
					Type:     field.ErrorTypeInvalid,
					Field:    "Spec.TenantID",
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
								},
							},
						},
					},
				},
			},
			wantErrors: []*field.Error{
				{
					Type:     field.ErrorTypeInvalid,
					Field:    "Spec.Groups[0].Rules[0].Expr",
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
								},
							},
						},
					},
				},
			},
			wantErrors: []*field.Error{
				{
					Type:     field.ErrorTypeInvalid,
					Field:    "Spec.Groups[0].Rules[0].Expr",
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
								},
							},
						},
					},
				},
			},
			wantErrors: []*field.Error{
				{
					Type:     field.ErrorTypeInvalid,
					Field:    "Spec.Groups[0].Rules[0].Expr",
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
								},
							},
						},
					},
				},
			},
			wantErrors: []*field.Error{
				{
					Type:     field.ErrorTypeInvalid,
					Field:    "Spec.Groups[0].Rules[0].Expr",
					BadValue: `sum(rate({kubernetes_namespace_name="other-ns", level="error"}[5m])) by (job) > 0.1`,
					Detail:   lokiv1.ErrRuleMustMatchNamespace.Error(),
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
