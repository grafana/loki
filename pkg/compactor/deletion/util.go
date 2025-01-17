package deletion

import (
	"errors"

	"github.com/grafana/loki/v3/pkg/compactor/deletionmode"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
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

func partitionByRequestID(reqs []DeleteRequest) map[string][]DeleteRequest {
	groups := make(map[string][]DeleteRequest)
	for _, req := range reqs {
		groups[req.RequestID] = append(groups[req.RequestID], req)
	}
	return groups
}
