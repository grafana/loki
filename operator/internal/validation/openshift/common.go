package openshift

import (
	"strings"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"

	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/prometheus/prometheus/model/labels"
)

const (
	namespaceLabelName        = "kubernetes_namespace_name"
	namespaceOpenshiftLogging = "openshift-logging"

	tenantAudit          = "audit"
	tenantApplication    = "application"
	tenantInfrastructure = "infrastructure"
)

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

	matchers := sampleExpr.Selector().Matchers()
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
