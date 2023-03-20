package openshift

import (
	"context"
	"fmt"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"

	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/strings/slices"
)

// RecordingRuleValidator does extended-validation of RecordingRule resources for Openshift-based deployments.
func RecordingRuleValidator(_ context.Context, recordingRule *lokiv1.RecordingRule) field.ErrorList {
	var allErrs field.ErrorList

	// Check tenant matches expected value
	tenantID := recordingRule.Spec.TenantID
	wantTenant := tenantForNamespace(recordingRule.Namespace)
	if !slices.Contains(wantTenant, tenantID) {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("Spec").Child("TenantID"),
			tenantID,
			fmt.Sprintf("RecordingRule does not use correct tenant %q", wantTenant)))
	}

	for i, g := range recordingRule.Spec.Groups {
		for j, rule := range g.Rules {
			if err := validateRuleExpression(recordingRule.Namespace, tenantID, rule.Expr); err != nil {
				allErrs = append(allErrs, field.Invalid(
					field.NewPath("Spec").Child("Groups").Index(i).Child("Rules").Index(j).Child("Expr"),
					rule.Expr,
					err.Error(),
				))
			}
		}
	}

	return allErrs
}
