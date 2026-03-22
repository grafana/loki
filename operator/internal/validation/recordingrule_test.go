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

var rtt = []struct {
	desc string
	spec lokiv1.RecordingRuleSpec
	err  *apierrors.StatusError
}{
	{
		desc: "valid spec",
		spec: lokiv1.RecordingRuleSpec{
			Groups: []*lokiv1.RecordingRuleGroup{
				{
					Name:     "first",
					Interval: lokiv1.PrometheusDuration("1m"),
					Rules: []*lokiv1.RecordingRuleGroupSpec{
						{
							Record: "valid:record:name",
							Expr:   `sum(rate({app="foo", env="production"} |= "error" [5m])) by (job)`,
						},
						{
							Record: "valid:second:name",
							Expr:   `sum(rate({app="foo", env="stage"} |= "error" [5m])) by (job)`,
						},
					},
				},
				{
					Name:     "second",
					Interval: lokiv1.PrometheusDuration("1m"),
					Rules: []*lokiv1.RecordingRuleGroupSpec{
						{
							Record: "nginx:requests:rate1m",
							Expr:   `sum(rate({container="nginx"}[1m]))`,
						},
						{
							Record: "banana:requests:rate5m",
							Expr:   `sum(rate({container="banana"}[1m]))`,
						},
					},
				},
			},
		},
	},
	{
		desc: "not unique group names",
		spec: lokiv1.RecordingRuleSpec{
			Groups: []*lokiv1.RecordingRuleGroup{
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
			schema.GroupKind{Group: "loki.grafana.com", Kind: "RecordingRule"},
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
		spec: lokiv1.RecordingRuleSpec{
			Groups: []*lokiv1.RecordingRuleGroup{
				{
					Name:     "first",
					Interval: lokiv1.PrometheusDuration("1mo"),
				},
			},
		},
		err: apierrors.NewInvalid(
			schema.GroupKind{Group: "loki.grafana.com", Kind: "RecordingRule"},
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
		desc: "invalid record metric name",
		spec: lokiv1.RecordingRuleSpec{
			Groups: []*lokiv1.RecordingRuleGroup{
				{
					Name:     "first",
					Interval: lokiv1.PrometheusDuration("1m"),
					Rules: []*lokiv1.RecordingRuleGroupSpec{
						{
							Record: "invalid&metric:name",
							Expr:   `sum(rate({label="value"}[1m]))`,
						},
					},
				},
			},
		},
		err: apierrors.NewInvalid(
			schema.GroupKind{Group: "loki.grafana.com", Kind: "RecordingRule"},
			"testing-rule",
			field.ErrorList{
				field.Invalid(
					field.NewPath("spec").Child("groups").Index(0).Child("rules").Index(0).Child("record"),
					"invalid&metric:name",
					lokiv1.ErrInvalidRecordMetricName.Error(),
				),
			},
		),
	},
	{
		desc: "parse LogQL expression err",
		spec: lokiv1.RecordingRuleSpec{
			Groups: []*lokiv1.RecordingRuleGroup{
				{
					Name:     "first",
					Interval: lokiv1.PrometheusDuration("1m"),
					Rules: []*lokiv1.RecordingRuleGroupSpec{
						{
							Expr: "this is not a valid expression",
						},
					},
				},
			},
		},
		err: apierrors.NewInvalid(
			schema.GroupKind{Group: "loki.grafana.com", Kind: "RecordingRule"},
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
		spec: lokiv1.RecordingRuleSpec{
			Groups: []*lokiv1.RecordingRuleGroup{
				{
					Name:     "first",
					Interval: lokiv1.PrometheusDuration("1m"),
					Rules: []*lokiv1.RecordingRuleGroupSpec{
						{
							Expr: `{message=~".+"}`,
						},
					},
				},
			},
		},
		err: apierrors.NewInvalid(
			schema.GroupKind{Group: "loki.grafana.com", Kind: "RecordingRule"},
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

func TestRecordingRuleValidationWebhook_ValidateCreate(t *testing.T) {
	for _, tc := range rtt {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			l := &lokiv1.RecordingRule{
				ObjectMeta: metav1.ObjectMeta{
					Name: "testing-rule",
				},
				Spec: tc.spec,
			}

			v := &validation.RecordingRuleValidator{}
			_, err := v.ValidateCreate(ctx, l)
			if err != nil {
				require.Equal(t, tc.err, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRecordingRuleValidationWebhook_ValidateUpdate(t *testing.T) {
	for _, tc := range rtt {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			l := &lokiv1.RecordingRule{
				ObjectMeta: metav1.ObjectMeta{
					Name: "testing-rule",
				},
				Spec: tc.spec,
			}

			v := &validation.RecordingRuleValidator{}
			_, err := v.ValidateUpdate(ctx, &lokiv1.RecordingRule{}, l)
			if err != nil {
				require.Equal(t, tc.err, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
