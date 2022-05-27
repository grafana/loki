package v1beta1_test

import (
	"testing"

	"github.com/grafana/loki/operator/api/v1beta1"
	"github.com/stretchr/testify/require"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

var att = []struct {
	desc string
	spec v1beta1.AlertingRuleSpec
	err  *apierrors.StatusError
}{
	{
		desc: "valid spec",
		spec: v1beta1.AlertingRuleSpec{
			Groups: []*v1beta1.AlertingRuleGroup{
				{
					Name:     "first",
					Interval: v1beta1.PrometheusDuration("1m"),
					Limit:    10,
					Rules: []*v1beta1.AlertingRuleGroupSpec{
						{
							Alert: "first-alert",
							For:   v1beta1.PrometheusDuration("10m"),
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
							For:   v1beta1.PrometheusDuration("10m"),
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
					Interval: v1beta1.PrometheusDuration("1m"),
					Limit:    10,
					Rules: []*v1beta1.AlertingRuleGroupSpec{
						{
							Alert: "third-alert",
							For:   v1beta1.PrometheusDuration("10m"),
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
							For:   v1beta1.PrometheusDuration("10m"),
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
		spec: v1beta1.AlertingRuleSpec{
			Groups: []*v1beta1.AlertingRuleGroup{
				{
					Name:     "first",
					Interval: v1beta1.PrometheusDuration("1m"),
				},
				{
					Name:     "first",
					Interval: v1beta1.PrometheusDuration("1m"),
				},
			},
		},
		err: apierrors.NewInvalid(
			schema.GroupKind{Group: "loki.grafana.com", Kind: "AlertingRule"},
			"testing-rule",
			field.ErrorList{
				field.Invalid(
					field.NewPath("Spec").Child("Groups").Index(1).Child("Name"),
					"first",
					v1beta1.ErrGroupNamesNotUnique.Error(),
				),
			},
		),
	},
	{
		desc: "parse eval interval err",
		spec: v1beta1.AlertingRuleSpec{
			Groups: []*v1beta1.AlertingRuleGroup{
				{
					Name:     "first",
					Interval: v1beta1.PrometheusDuration("1mo"),
				},
			},
		},
		err: apierrors.NewInvalid(
			schema.GroupKind{Group: "loki.grafana.com", Kind: "AlertingRule"},
			"testing-rule",
			field.ErrorList{
				field.Invalid(
					field.NewPath("Spec").Child("Groups").Index(0).Child("Interval"),
					"1mo",
					v1beta1.ErrParseEvaluationInterval.Error(),
				),
			},
		),
	},
	{
		desc: "parse for interval err",
		spec: v1beta1.AlertingRuleSpec{
			Groups: []*v1beta1.AlertingRuleGroup{
				{
					Name:     "first",
					Interval: v1beta1.PrometheusDuration("1m"),
					Rules: []*v1beta1.AlertingRuleGroupSpec{
						{
							Alert: "an-alert",
							For:   v1beta1.PrometheusDuration("10years"),
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
					field.NewPath("Spec").Child("Groups").Index(0).Child("Rules").Index(0).Child("For"),
					"10years",
					v1beta1.ErrParseAlertForPeriod.Error(),
				),
			},
		),
	},
	{
		desc: "parse LogQL expression err",
		spec: v1beta1.AlertingRuleSpec{
			Groups: []*v1beta1.AlertingRuleGroup{
				{
					Name:     "first",
					Interval: v1beta1.PrometheusDuration("1m"),
					Rules: []*v1beta1.AlertingRuleGroupSpec{
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
					field.NewPath("Spec").Child("Groups").Index(0).Child("Rules").Index(0).Child("Expr"),
					"this is not a valid expression",
					v1beta1.ErrParseLogQLExpression.Error(),
				),
			},
		),
	},
}

func TestAlertingRuleValidationWebhook_ValidateCreate(t *testing.T) {
	for _, tc := range att {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			l := v1beta1.AlertingRule{
				ObjectMeta: metav1.ObjectMeta{
					Name: "testing-rule",
				},
				Spec: tc.spec,
			}

			err := l.ValidateCreate()
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
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			l := v1beta1.AlertingRule{
				ObjectMeta: metav1.ObjectMeta{
					Name: "testing-rule",
				},
				Spec: tc.spec,
			}

			err := l.ValidateUpdate(&v1beta1.AlertingRule{})
			if err != nil {
				require.Equal(t, tc.err, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
