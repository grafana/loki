package deletion

import (
	"errors"

	"github.com/grafana/loki/pkg/logql"
)

var (
	errInvalidLogQL     = errors.New("invalid LogQL expression")
	errUnsupportedLogQL = errors.New("unsupported LogQL expression")
)

// checkLogQLExpressionForDeletion checks if the given logQL is valid for deletions
func checkLogQLExpressionForDeletion(logQL string) error {
	expr, err := logql.ParseExpr(logQL)
	if err != nil {
		return errInvalidLogQL
	}

	if _, ok := expr.(*logql.MatchersExpr); ok {
		return nil
	}
	if pipelineExpr, ok := expr.(*logql.PipelineExpr); ok {
		// Only support line filters for now
		for _, stage := range pipelineExpr.MultiStages {
			if _, ok := stage.(*logql.LineFilterExpr); !ok {
				return errUnsupportedLogQL
			}
		}
		return nil
	}

	return errUnsupportedLogQL
}
