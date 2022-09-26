package openshift

import (
	"context"
	"fmt"

	lokiv1beta1 "github.com/grafana/loki/operator/apis/loki/v1beta1"

	"k8s.io/apimachinery/pkg/util/validation/field"
)

// RecordingRuleValidator does extended-validation of RecordingRule resources for Openshift-based deployments.
func RecordingRuleValidator(_ context.Context, recordingRule *lokiv1beta1.RecordingRule) field.ErrorList {
	var allErrs field.ErrorList

	// Check tenant matches expected value
	wantTenant := tenantForNamespace(recordingRule.Namespace)
	if recordingRule.Spec.TenantID != wantTenant {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("Spec").Child("TenantID"),
			recordingRule.Spec.TenantID,
			fmt.Sprintf("RecordingRule does not use correct tenant %q", wantTenant)))
	}

	for i, g := range recordingRule.Spec.Groups {
		for j, rule := range g.Rules {
			if err := validateRuleExpression(recordingRule.Namespace, rule.Expr); err != nil {
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
