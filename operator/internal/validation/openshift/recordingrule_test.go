package openshift

import (
	"context"
	"testing"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

func TestRecordingRuleValidator(t *testing.T) {
	tt := []struct {
		desc       string
		spec       *lokiv1.RecordingRule
		wantErrors field.ErrorList
	}{
		{
			desc: "success",
			spec: &lokiv1.RecordingRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "recording-rule",
					Namespace: "example",
				},
				Spec: lokiv1.RecordingRuleSpec{
					TenantID: "application",
					Groups: []*lokiv1.RecordingRuleGroup{
						{
							Rules: []*lokiv1.RecordingRuleGroupSpec{
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
			desc: "wrong tenant",
			spec: &lokiv1.RecordingRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "recording-rule",
					Namespace: "openshift-example",
				},
				Spec: lokiv1.RecordingRuleSpec{
					TenantID: "application",
					Groups: []*lokiv1.RecordingRuleGroup{
						{
							Rules: []*lokiv1.RecordingRuleGroupSpec{
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
					Detail:   `RecordingRule does not use correct tenant ["infrastructure"]`,
				},
			},
		},
		{
			desc: "expression does not parse",
			spec: &lokiv1.RecordingRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "recording-rule",
					Namespace: "example",
				},
				Spec: lokiv1.RecordingRuleSpec{
					TenantID: "application",
					Groups: []*lokiv1.RecordingRuleGroup{
						{
							Rules: []*lokiv1.RecordingRuleGroupSpec{
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
			spec: &lokiv1.RecordingRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "recording-rule",
					Namespace: "example",
				},
				Spec: lokiv1.RecordingRuleSpec{
					TenantID: "application",
					Groups: []*lokiv1.RecordingRuleGroup{
						{
							Rules: []*lokiv1.RecordingRuleGroupSpec{
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
			spec: &lokiv1.RecordingRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "recording-rule",
					Namespace: "example",
				},
				Spec: lokiv1.RecordingRuleSpec{
					TenantID: "application",
					Groups: []*lokiv1.RecordingRuleGroup{
						{
							Rules: []*lokiv1.RecordingRuleGroupSpec{
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
			desc: "matcher does not match RecordingRule namespace",
			spec: &lokiv1.RecordingRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "recording-rule",
					Namespace: "example",
				},
				Spec: lokiv1.RecordingRuleSpec{
					TenantID: "application",
					Groups: []*lokiv1.RecordingRuleGroup{
						{
							Rules: []*lokiv1.RecordingRuleGroupSpec{
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
			errors := RecordingRuleValidator(ctx, tc.spec)
			require.Equal(t, tc.wantErrors, errors)
		})
	}
}
