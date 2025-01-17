package validation

import (
	"context"
	"fmt"

	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/prometheus/common/model"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
)

var _ admission.CustomValidator = &AlertingRuleValidator{}

// AlertingRuleValidator implements a custom validator for AlertingRule resources.
type AlertingRuleValidator struct {
	ExtendedValidator func(context.Context, *lokiv1.AlertingRule) field.ErrorList
}

// SetupWebhookWithManager registers the AlertingRuleValidator as a validating webhook
// with the controller-runtime manager or returns an error.
func (v *AlertingRuleValidator) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&lokiv1.AlertingRule{}).
		WithValidator(v).
		Complete()
}

// ValidateCreate implements admission.CustomValidator.
func (v *AlertingRuleValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return v.validate(ctx, obj)
}

// ValidateUpdate implements admission.CustomValidator.
func (v *AlertingRuleValidator) ValidateUpdate(ctx context.Context, _, newObj runtime.Object) (admission.Warnings, error) {
	return v.validate(ctx, newObj)
}

// ValidateDelete implements admission.CustomValidator.
func (v *AlertingRuleValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	// No validation on delete
	return nil, nil
}

func (v *AlertingRuleValidator) validate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	alertingRule, ok := obj.(*lokiv1.AlertingRule)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("object is not of type AlertingRule: %t", obj))
	}

	var allErrs field.ErrorList

	found := make(map[string]bool)

	for i, g := range alertingRule.Spec.Groups {
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

		for j, rule := range g.Rules {
			// Check if alert for period is a valid PromQL duration
			if rule.Alert != "" {
				if _, err := model.ParseDuration(string(rule.For)); err != nil {
					allErrs = append(allErrs, field.Invalid(
						field.NewPath("spec").Child("groups").Index(i).Child("rules").Index(j).Child("for"),
						rule.For,
						lokiv1.ErrParseAlertForPeriod.Error(),
					))

					continue
				}
			}

			// Check if the LogQL parser can parse the rule expression
			expr, err := syntax.ParseExpr(rule.Expr)
			if err != nil {
				allErrs = append(allErrs, field.Invalid(
					field.NewPath("spec").Child("groups").Index(i).Child("rules").Index(j).Child("expr"),
					rule.Expr,
					lokiv1.ErrParseLogQLExpression.Error(),
				))

				continue
			}

			// Validate that the expression is a sample-expression (metrics as result) and not for logs
			if _, ok := expr.(syntax.SampleExpr); !ok {
				allErrs = append(allErrs, field.Invalid(
					field.NewPath("spec").Child("groups").Index(i).Child("rules").Index(j).Child("expr"),
					rule.Expr,
					lokiv1.ErrParseLogQLNotSample.Error(),
				))
			}
		}
	}

	if v.ExtendedValidator != nil {
		allErrs = append(allErrs, v.ExtendedValidator(ctx, alertingRule)...)
	}

	if len(allErrs) == 0 {
		return nil, nil
	}

	return nil, apierrors.NewInvalid(
		schema.GroupKind{Group: "loki.grafana.com", Kind: "AlertingRule"},
		alertingRule.Name,
		allErrs,
	)
}
