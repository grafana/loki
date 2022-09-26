package openshift

import (
	"strings"

	lokiv1beta1 "github.com/grafana/loki/operator/apis/loki/v1beta1"

	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/prometheus/prometheus/model/labels"
)

func validateRuleExpression(namespace, rawExpr string) error {
	// Check if the LogQL parser can parse the rule expression
	expr, err := syntax.ParseExpr(rawExpr)
	if err != nil {
		return lokiv1beta1.ErrParseLogQLExpression
	}

	sampleExpr, ok := expr.(syntax.SampleExpr)
	if !ok {
		return lokiv1beta1.ErrParseLogQLNotSample
	}

	matchers := sampleExpr.Selector().Matchers()
	if !validateIncludesNamespace(namespace, matchers) {
		return lokiv1beta1.ErrRuleMustMatchNamespace
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

func tenantForNamespace(namespace string) string {
	if strings.HasPrefix(namespace, "openshift") ||
		strings.HasPrefix(namespace, "kube-") ||
		namespace == "default" {
		return "infrastructure"
	}

	return "application"
}
