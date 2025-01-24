package validation_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/validation"
)

var att = []struct {
	desc string
	spec lokiv1.AlertingRuleSpec
	err  *apierrors.StatusError
}{
	{
		desc: "valid spec",
		spec: lokiv1.AlertingRuleSpec{
			Groups: []*lokiv1.AlertingRuleGroup{
				{
					Name:     "first",
					Interval: lokiv1.PrometheusDuration("1m"),
					Limit:    10,
					Rules: []*lokiv1.AlertingRuleGroupSpec{
						{
							Alert: "first-alert",
							For:   lokiv1.PrometheusDuration("10m"),
							Expr:  `sum(rate({app="foo", env="production"} |= "error" [5m])) by (job)`,
							Annotations: map[string]string{
								"annot": "something",
							},
							Labels: map[string]string{
								"severity": "critical",
							},
						},
						{
							Alert: "second-alert",
							For:   lokiv1.PrometheusDuration("10m"),
							Expr:  `sum(rate({app="foo", env="stage"} |= "error" [5m])) by (job)`,
							Annotations: map[string]string{
								"env": "something",
							},
							Labels: map[string]string{
								"severity": "warning",
							},
						},
					},
				},
				{
					Name:     "second",
					Interval: lokiv1.PrometheusDuration("1m"),
					Limit:    10,
					Rules: []*lokiv1.AlertingRuleGroupSpec{
						{
							Alert: "third-alert",
							For:   lokiv1.PrometheusDuration("10m"),
							Expr:  `sum(rate({app="foo", env="production"} |= "error" [5m])) by (job)`,
							Annotations: map[string]string{
								"annot": "something",
							},
							Labels: map[string]string{
								"severity": "critical",
							},
						},
						{
							Alert: "fourth-alert",
							For:   lokiv1.PrometheusDuration("10m"),
							Expr:  `sum(rate({app="foo", env="stage"} |= "error" [5m])) by (job)`,
							Annotations: map[string]string{
								"env": "something",
							},
							Labels: map[string]string{
								"severity": "warning",
							},
						},
					},
				},
			},
		},
	},
	{
		desc: "not unique group names",
		spec: lokiv1.AlertingRuleSpec{
			Groups: []*lokiv1.AlertingRuleGroup{
				{
					Name:     "first",
					Interval: lokiv1.PrometheusDuration("1m"),
				},
				{
					Name:     "first",
					Interval: lokiv1.PrometheusDuration("1m"),
				},
			},
		},
		err: apierrors.NewInvalid(
			schema.GroupKind{Group: "loki.grafana.com", Kind: "AlertingRule"},
			"testing-rule",
			field.ErrorList{
				field.Invalid(
					field.NewPath("spec").Child("groups").Index(1).Child("name"),
					"first",
					lokiv1.ErrGroupNamesNotUnique.Error(),
				),
			},
		),
	},
	{
		desc: "parse eval interval err",
		spec: lokiv1.AlertingRuleSpec{
			Groups: []*lokiv1.AlertingRuleGroup{
				{
					Name:     "first",
					Interval: lokiv1.PrometheusDuration("1mo"),
				},
			},
		},
		err: apierrors.NewInvalid(
			schema.GroupKind{Group: "loki.grafana.com", Kind: "AlertingRule"},
			"testing-rule",
			field.ErrorList{
				field.Invalid(
					field.NewPath("spec").Child("groups").Index(0).Child("interval"),
					"1mo",
					lokiv1.ErrParseEvaluationInterval.Error(),
				),
			},
		),
	},
	{
		desc: "parse for interval err",
		spec: lokiv1.AlertingRuleSpec{
			Groups: []*lokiv1.AlertingRuleGroup{
				{
					Name:     "first",
					Interval: lokiv1.PrometheusDuration("1m"),
					Rules: []*lokiv1.AlertingRuleGroupSpec{
						{
							Alert: "an-alert",
							For:   lokiv1.PrometheusDuration("10years"),
							Expr:  `sum(rate({label="value"}[1m]))`,
						},
					},
				},
			},
		},
		err: apierrors.NewInvalid(
			schema.GroupKind{Group: "loki.grafana.com", Kind: "AlertingRule"},
			"testing-rule",
			field.ErrorList{
				field.Invalid(
					field.NewPath("spec").Child("groups").Index(0).Child("rules").Index(0).Child("for"),
					"10years",
					lokiv1.ErrParseAlertForPeriod.Error(),
				),
			},
		),
	},
	{
		desc: "parse LogQL expression err",
		spec: lokiv1.AlertingRuleSpec{
			Groups: []*lokiv1.AlertingRuleGroup{
				{
					Name:     "first",
					Interval: lokiv1.PrometheusDuration("1m"),
					Rules: []*lokiv1.AlertingRuleGroupSpec{
						{
							Expr: "this is not a valid expression",
						},
					},
				},
			},
		},
		err: apierrors.NewInvalid(
			schema.GroupKind{Group: "loki.grafana.com", Kind: "AlertingRule"},
			"testing-rule",
			field.ErrorList{
				field.Invalid(
					field.NewPath("spec").Child("groups").Index(0).Child("rules").Index(0).Child("expr"),
					"this is not a valid expression",
					lokiv1.ErrParseLogQLExpression.Error(),
				),
			},
		),
	},
	{
		desc: "LogQL not sample-expression",
		spec: lokiv1.AlertingRuleSpec{
			Groups: []*lokiv1.AlertingRuleGroup{
				{
					Name:     "first",
					Interval: lokiv1.PrometheusDuration("1m"),
					Rules: []*lokiv1.AlertingRuleGroupSpec{
						{
							Expr: `{message=~".+"}`,
						},
					},
				},
			},
		},
		err: apierrors.NewInvalid(
			schema.GroupKind{Group: "loki.grafana.com", Kind: "AlertingRule"},
			"testing-rule",
			field.ErrorList{
				field.Invalid(
					field.NewPath("spec").Child("groups").Index(0).Child("rules").Index(0).Child("expr"),
					`{message=~".+"}`,
					lokiv1.ErrParseLogQLNotSample.Error(),
				),
			},
		),
	},
}

func TestAlertingRuleValidationWebhook_ValidateCreate(t *testing.T) {
	for _, tc := range att {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			l := &lokiv1.AlertingRule{
				ObjectMeta: metav1.ObjectMeta{
					Name: "testing-rule",
				},
				Spec: tc.spec,
			}
			ctx := context.Background()

			v := &validation.AlertingRuleValidator{}
			_, err := v.ValidateCreate(ctx, l)
			if err != nil {
				require.Equal(t, tc.err, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestAlertingRuleValidationWebhook_ValidateUpdate(t *testing.T) {
	for _, tc := range att {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			l := &lokiv1.AlertingRule{
				ObjectMeta: metav1.ObjectMeta{
					Name: "testing-rule",
				},
				Spec: tc.spec,
			}
			ctx := context.Background()

			v := &validation.AlertingRuleValidator{}
			_, err := v.ValidateUpdate(ctx, &lokiv1.AlertingRule{}, l)
			if err != nil {
				require.Equal(t, tc.err, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
