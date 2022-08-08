package deletion

import (
	"errors"

	"github.com/grafana/loki/pkg/storage/stores/indexshipper/compactor/retention"

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

func validDeletionLimit(l retention.Limits, userID string) (bool, error) {
	allLimits := l.AllByUserID()
	if userLimits, ok := allLimits[userID]; ok {
		hasDelete, err := Enabled(userLimits.DeletionMode)
		if hasDelete {
			return true, nil
		}
		return false, err
	}

	hasDelete, err := Enabled(l.DefaultLimits().DeletionMode)
	if hasDelete {
		return true, nil
	}
	return false, err
}
