package deletion

import (
	"errors"

	"github.com/grafana/loki/pkg/logql/syntax"
)

var (
	errInvalidQuery = errors.New("invalid query expression")
)

// parseDeletionQuery checks if the given logQL is valid for deletions
func parseDeletionQuery(query string) (syntax.LogSelectorExpr, error) {
	logSelectorExpr, err := syntax.ParseLogSelector(query, false)
	if err != nil {
		return nil, errInvalidQuery
	}

	return logSelectorExpr, nil
}
