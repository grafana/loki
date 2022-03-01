package deletion

import (
	"errors"

	"github.com/grafana/loki/pkg/logql"
	"github.com/prometheus/prometheus/model/labels"
)

var (
	errInvalidLogQL     = errors.New("invalid LogQL expression")
	errUnsupportedLogQL = errors.New("unsupported LogQL expression")
)

// parseLogQLExpressionForDeletion checks if the given logQL is valid for deletions
func parseLogQLExpressionForDeletion(logQL string) ([]*labels.Matcher, error) {
	expr, err := logql.ParseExpr(logQL)
	if err != nil {
		return nil, errInvalidLogQL
	}

	if matchersExpr, ok := expr.(*logql.MatchersExpr); ok {
		return matchersExpr.Matchers(), nil
	}

	return nil, errUnsupportedLogQL
}
