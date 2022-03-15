package deletion

import (
	"errors"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/logql/syntax"
)

var (
	errInvalidQuery     = errors.New("invalid query expression")
	errUnsupportedQuery = errors.New("unsupported query expression")
)

// parseDeletionQuery checks if the given logQL is valid for deletions
func parseDeletionQuery(query string) ([]*labels.Matcher, error) {
	expr, err := syntax.ParseExpr(query)
	if err != nil {
		return nil, errInvalidQuery
	}

	if matchersExpr, ok := expr.(*syntax.MatchersExpr); ok {
		return matchersExpr.Matchers(), nil
	}

	return nil, errUnsupportedQuery
}
