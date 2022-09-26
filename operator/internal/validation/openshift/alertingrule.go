package openshift

import (
	"context"
	"fmt"

	lokiv1beta1 "github.com/grafana/loki/operator/apis/loki/v1beta1"

	"k8s.io/apimachinery/pkg/util/validation/field"
)

const (
	namespaceLabelName = "kubernetes_namespace_name"
)

// AlertingRuleValidator does extended-validation of AlertingRule resources for Openshift-based deployments.
func AlertingRuleValidator(_ context.Context, alertingRule *lokiv1beta1.AlertingRule) field.ErrorList {
	var allErrs field.ErrorList

	// Check tenant matches expected value
	wantTenant := tenantForNamespace(alertingRule.Namespace)
	if alertingRule.Spec.TenantID != wantTenant {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("Spec").Child("TenantID"),
			alertingRule.Spec.TenantID,
			fmt.Sprintf("AlertingRule does not use correct tenant %q", wantTenant)))
	}

	for i, g := range alertingRule.Spec.Groups {
		for j, rule := range g.Rules {
			if err := validateRuleExpression(alertingRule.Namespace, rule.Expr); err != nil {
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
