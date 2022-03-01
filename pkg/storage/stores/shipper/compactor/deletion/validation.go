package deletion

import (
	"errors"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/logql"
)

var (
	errInvalidQuery     = errors.New("invalid query expression")
	errUnsupportedQuery = errors.New("unsupported query expression")
)

// parseDeletionQuery checks if the given logQL is valid for deletions
func parseDeletionQuery(query string) ([]*labels.Matcher, error) {
	expr, err := logql.ParseExpr(query)
	if err != nil {
		return nil, errInvalidQuery
	}

	if matchersExpr, ok := expr.(*logql.MatchersExpr); ok {
		return matchersExpr.Matchers(), nil
	}

	return nil, errUnsupportedQuery
}
