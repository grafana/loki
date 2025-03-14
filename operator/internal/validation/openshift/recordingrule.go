package openshift

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/strings/slices"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
)

// RecordingRuleValidator does extended-validation of RecordingRule resources for Openshift-based deployments.
func RecordingRuleValidator(_ context.Context, recordingRule *lokiv1.RecordingRule) field.ErrorList {
	validateTenantIDs, fieldErr := tenantIDValidationEnabled(recordingRule.Annotations)
	if fieldErr != nil {
		return field.ErrorList{fieldErr}
	}

	if !validateTenantIDs {
		return nil
	}

	var allErrs field.ErrorList

	// Check tenant matches expected value
	tenantID := recordingRule.Spec.TenantID
	wantTenant := tenantForNamespace(recordingRule.Namespace)
	if !slices.Contains(wantTenant, tenantID) {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("spec").Child("tenantID"),
			tenantID,
			fmt.Sprintf("RecordingRule does not use correct tenant %q", wantTenant)))
	}

	for i, g := range recordingRule.Spec.Groups {
		for j, rule := range g.Rules {
			if err := validateRuleExpression(recordingRule.Namespace, tenantID, rule.Expr); err != nil {
				allErrs = append(allErrs, field.Invalid(
					field.NewPath("spec").Child("groups").Index(i).Child("rules").Index(j).Child("expr"),
					rule.Expr,
					err.Error(),
				))
			}
		}
	}

	return allErrs
}
