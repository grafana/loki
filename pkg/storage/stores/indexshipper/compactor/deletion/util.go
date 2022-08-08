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
	mode, err := deleteModeFromLimits(l, userID)
	if err != nil {
		return false, err
	}

	return mode.DeleteEnabled(), nil
}

func deleteModeFromLimits(l retention.Limits, userID string) (Mode, error) {
	allLimits := l.AllByUserID()
	if userLimits, ok := allLimits[userID]; ok {
		return ParseMode(userLimits.DeletionMode)
	}
	return ParseMode(l.DefaultLimits().DeletionMode)
}
