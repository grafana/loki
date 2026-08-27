package deletion

import (
	"errors"
	"fmt"

	"github.com/grafana/loki/v3/pkg/compactor/deletionmode"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

var (
	errInvalidQuery = errors.New("invalid query expression")
)

// parseDeletionQuery checks if the given logQL is valid for deletions. It applies the
// same validation we apply to queries on the read path, e.g. requiring at least one
// matcher that does not match everything.
func parseDeletionQuery(query string) (syntax.LogSelectorExpr, error) {
	logSelectorExpr, err := syntax.ParseLogSelector(query, true)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", errInvalidQuery, err)
	}

	// Some stages are only checked when the pipeline gets built, for instance line
	// filter regexes and ip() patterns. Build it here and throw it away so we reject
	// the request now, instead of failing every time we try to process it later.
	if _, err := logSelectorExpr.Pipeline(); err != nil {
		return nil, fmt.Errorf("%w: %s", errInvalidQuery, err)
	}

	return logSelectorExpr, nil
}

func validDeletionLimit(l Limits, userID string) (bool, error) {
	mode, err := deleteModeFromLimits(l, userID)
	if err != nil {
		return false, err
	}

	return mode.DeleteEnabled(), nil
}

func deleteModeFromLimits(l Limits, userID string) (deletionmode.Mode, error) {
	mode := l.DeletionMode(userID)
	return deletionmode.ParseMode(mode)
}
