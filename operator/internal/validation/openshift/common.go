package openshift

import (
	"regexp"
	"strings"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"

	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/prometheus/prometheus/model/labels"
)

const (
	severityLabelName = "severity"

	summaryAnnotationName     = "summary"
	descriptionAnnotationName = "description"

	namespaceLabelName        = "kubernetes_namespace_name"
	namespaceOpenshiftLogging = "openshift-logging"

	tenantAudit          = "audit"
	tenantApplication    = "application"
	tenantInfrastructure = "infrastructure"
)

var severityRe = regexp.MustCompile("^critical|warning|info$")

func validateRuleExpression(namespace, tenantID, rawExpr string) error {
	// Check if the LogQL parser can parse the rule expression
	expr, err := syntax.ParseExpr(rawExpr)
	if err != nil {
		return lokiv1.ErrParseLogQLExpression
	}

	sampleExpr, ok := expr.(syntax.SampleExpr)
	if !ok {
		return lokiv1.ErrParseLogQLNotSample
	}

	selector, err := sampleExpr.Selector()
	if err != nil {
		return lokiv1.ErrParseLogQLSelector
	}

	matchers := selector.Matchers()
	if tenantID != tenantAudit && !validateIncludesNamespace(namespace, matchers) {
		return lokiv1.ErrRuleMustMatchNamespace
	}

	return nil
}

func validateIncludesNamespace(namespace string, matchers []*labels.Matcher) bool {
	for _, m := range matchers {
		if m.Name == namespaceLabelName && m.Type == labels.MatchEqual && m.Value == namespace {
			return true
		}
	}

	return false
}

func tenantForNamespace(namespace string) []string {
	if strings.HasPrefix(namespace, "openshift") ||
		strings.HasPrefix(namespace, "kube-") ||
		namespace == "default" {
		if namespace == namespaceOpenshiftLogging {
			return []string{tenantAudit, tenantInfrastructure}
		}

		return []string{tenantInfrastructure}
	}

	return []string{tenantApplication}
}

func validateRuleLabels(labels map[string]string) error {
	value, found := labels[severityLabelName]
	if !found {
		return lokiv1.ErrSeverityLabelMissing
	}

	if !severityRe.MatchString(value) {
		return lokiv1.ErrSeverityLabelInvalid
	}

	return nil
}

func validateRuleAnnotations(annotations map[string]string) error {
	value, found := annotations[summaryAnnotationName]
	if !found || value == "" {
		return lokiv1.ErrSummaryAnnotationMissing
	}

	value, found = annotations[descriptionAnnotationName]
	if !found || value == "" {
		return lokiv1.ErrDescriptionAnnotationMissing
	}

	return nil
}
