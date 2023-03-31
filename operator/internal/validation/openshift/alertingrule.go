package openshift

import (
	"context"
	"fmt"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"

	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/strings/slices"
)

// AlertingRuleValidator does extended-validation of AlertingRule resources for Openshift-based deployments.
func AlertingRuleValidator(_ context.Context, alertingRule *lokiv1.AlertingRule) field.ErrorList {
	var allErrs field.ErrorList

	// Check tenant matches expected value
	tenantID := alertingRule.Spec.TenantID
	wantTenant := tenantForNamespace(alertingRule.Namespace)
	if !slices.Contains(wantTenant, tenantID) {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("spec").Child("tenantID"),
			tenantID,
			fmt.Sprintf("AlertingRule does not use correct tenant %q", wantTenant)))
	}

	for i, g := range alertingRule.Spec.Groups {
		for j, rule := range g.Rules {
			if err := validateRuleExpression(alertingRule.Namespace, tenantID, rule.Expr); err != nil {
				allErrs = append(allErrs, field.Invalid(
					field.NewPath("spec").Child("groups").Index(i).Child("rules").Index(j).Child("expr"),
					rule.Expr,
					err.Error(),
				))
			}

			if err := validateRuleLabels(rule.Labels); err != nil {
				allErrs = append(allErrs, field.Invalid(
					field.NewPath("spec", "groups").Index(i).Child("rules").Index(j).Child("labels"),
					rule.Labels,
					err.Error(),
				))
			}

			if err := validateRuleAnnotations(rule.Annotations); err != nil {
				allErrs = append(allErrs, field.Invalid(
					field.NewPath("spec", "groups").Index(i).Child("rules").Index(j).Child("annotations"),
					rule.Annotations,
					err.Error(),
				))
			}
		}
	}

	return allErrs
}
