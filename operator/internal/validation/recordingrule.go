package validation

import (
	"context"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/prometheus/common/model"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
)

var _ admission.Validator[*lokiv1.RecordingRule] = &RecordingRuleValidator{}

// RecordingRuleValidator implements a custom validator for RecordingRule resources.
type RecordingRuleValidator struct {
	ExtendedValidator func(context.Context, *lokiv1.RecordingRule) field.ErrorList
}

// SetupWebhookWithManager registers the RecordingRuleValidator as a validating webhook
// with the controller-runtime manager or returns an error.
func (v *RecordingRuleValidator) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &lokiv1.RecordingRule{}).
		WithValidator(v).
		Complete()
}

// ValidateCreate implements admission.Validator.
func (v *RecordingRuleValidator) ValidateCreate(ctx context.Context, obj *lokiv1.RecordingRule) (admission.Warnings, error) {
	return v.validate(ctx, obj)
}

// ValidateUpdate implements admission.Validator.
func (v *RecordingRuleValidator) ValidateUpdate(ctx context.Context, _, newObj *lokiv1.RecordingRule) (admission.Warnings, error) {
	return v.validate(ctx, newObj)
}

// ValidateDelete implements admission.Validator.
func (v *RecordingRuleValidator) ValidateDelete(_ context.Context, _ *lokiv1.RecordingRule) (admission.Warnings, error) {
	// No validation on delete
	return nil, nil
}

func (v *RecordingRuleValidator) validate(ctx context.Context, recordingRule *lokiv1.RecordingRule) (admission.Warnings, error) {
	var allErrs field.ErrorList

	found := make(map[string]bool)

	for i, g := range recordingRule.Spec.Groups {
		// Check for group name uniqueness
		if found[g.Name] {
			allErrs = append(allErrs, field.Invalid(
				field.NewPath("spec").Child("groups").Index(i).Child("name"),
				g.Name,
				lokiv1.ErrGroupNamesNotUnique.Error(),
			))
		}

		found[g.Name] = true

		// Check if rule evaluation period is a valid PromQL duration
		_, err := model.ParseDuration(string(g.Interval))
		if err != nil {
			allErrs = append(allErrs, field.Invalid(
				field.NewPath("spec").Child("groups").Index(i).Child("interval"),
				g.Interval,
				lokiv1.ErrParseEvaluationInterval.Error(),
			))
		}

		for j, r := range g.Rules {
			// Check if recording rule name is a valid PromQL Label Name
			if r.Record != "" {
				if !model.UTF8Validation.IsValidMetricName(r.Record) {
					allErrs = append(allErrs, field.Invalid(
						field.NewPath("spec").Child("groups").Index(i).Child("rules").Index(j).Child("record"),
						r.Record,
						lokiv1.ErrInvalidRecordMetricName.Error(),
					))
				}
			}

			// Check if the LogQL parser can parse the rule expression
			expr, err := syntax.ParseExpr(r.Expr)
			if err != nil {
				allErrs = append(allErrs, field.Invalid(
					field.NewPath("spec").Child("groups").Index(i).Child("rules").Index(j).Child("expr"),
					r.Expr,
					lokiv1.ErrParseLogQLExpression.Error(),
				))

				continue
			}

			// Validate that the expression is a sample-expression (metrics as result) and not for logs
			if _, ok := expr.(syntax.SampleExpr); !ok {
				allErrs = append(allErrs, field.Invalid(
					field.NewPath("spec").Child("groups").Index(i).Child("rules").Index(j).Child("expr"),
					r.Expr,
					lokiv1.ErrParseLogQLNotSample.Error(),
				))
			}
		}
	}

	if v.ExtendedValidator != nil {
		allErrs = append(allErrs, v.ExtendedValidator(ctx, recordingRule)...)
	}

	if len(allErrs) == 0 {
		return nil, nil
	}

	return nil, apierrors.NewInvalid(
		schema.GroupKind{Group: "loki.grafana.com", Kind: "RecordingRule"},
		recordingRule.Name,
		allErrs,
	)
}
