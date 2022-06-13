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

var rtt = []struct {
	desc string
	spec v1beta1.RecordingRuleSpec
	err  *apierrors.StatusError
}{
	{
		desc: "valid spec",
		spec: v1beta1.RecordingRuleSpec{
			Groups: []*v1beta1.RecordingRuleGroup{
				{
					Name:     "first",
					Interval: v1beta1.PrometheusDuration("1m"),
					Rules: []*v1beta1.RecordingRuleGroupSpec{
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
					Interval: v1beta1.PrometheusDuration("1m"),
					Rules: []*v1beta1.RecordingRuleGroupSpec{
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
		spec: v1beta1.RecordingRuleSpec{
			Groups: []*v1beta1.RecordingRuleGroup{
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
			schema.GroupKind{Group: "loki.grafana.com", Kind: "RecordingRule"},
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
		spec: v1beta1.RecordingRuleSpec{
			Groups: []*v1beta1.RecordingRuleGroup{
				{
					Name:     "first",
					Interval: v1beta1.PrometheusDuration("1mo"),
				},
			},
		},
		err: apierrors.NewInvalid(
			schema.GroupKind{Group: "loki.grafana.com", Kind: "RecordingRule"},
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
		desc: "invalid record metric name",
		spec: v1beta1.RecordingRuleSpec{
			Groups: []*v1beta1.RecordingRuleGroup{
				{
					Name:     "first",
					Interval: v1beta1.PrometheusDuration("1m"),
					Rules: []*v1beta1.RecordingRuleGroupSpec{
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
					field.NewPath("Spec").Child("Groups").Index(0).Child("Rules").Index(0).Child("Record"),
					"invalid&metric:name",
					v1beta1.ErrInvalidRecordMetricName.Error(),
				),
			},
		),
	},
	{
		desc: "parse LogQL expression err",
		spec: v1beta1.RecordingRuleSpec{
			Groups: []*v1beta1.RecordingRuleGroup{
				{
					Name:     "first",
					Interval: v1beta1.PrometheusDuration("1m"),
					Rules: []*v1beta1.RecordingRuleGroupSpec{
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
					field.NewPath("Spec").Child("Groups").Index(0).Child("Rules").Index(0).Child("Expr"),
					"this is not a valid expression",
					v1beta1.ErrParseLogQLExpression.Error(),
				),
			},
		),
	},
}

func TestRecordingRuleValidationWebhook_ValidateCreate(t *testing.T) {
	for _, tc := range rtt {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			l := v1beta1.RecordingRule{
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

func TestRecordingRuleValidationWebhook_ValidateUpdate(t *testing.T) {
	for _, tc := range rtt {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			l := v1beta1.RecordingRule{
				ObjectMeta: metav1.ObjectMeta{
					Name: "testing-rule",
				},
				Spec: tc.spec,
			}

			err := l.ValidateUpdate(&v1beta1.RecordingRule{})
			if err != nil {
				require.Equal(t, tc.err, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
