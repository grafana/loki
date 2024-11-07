package logqlmodel

import (
	"errors"
	"fmt"
)

// Those errors are useful for comparing error returned by the engine.
// e.g. errors.Is(err,logqlmodel.ErrParse) let you know if this is a ast parsing error.
var (
	ErrLimit                            = errors.New("limit reached while evaluating the query")
	ErrIntervalLimit                    = errors.New("[interval] value exceeds limit")
	ErrBlocked                          = errors.New("query blocked by policy")
	ErrUnsupportedSyntaxForInstantQuery = errors.New("log queries are not supported as an instant query type, please change your query to a range query type")
)

type LimitError struct {
	error
}

func NewSeriesLimitError(limit int) *LimitError {
	return &LimitError{
		error: fmt.Errorf("maximum of series (%d) reached for a single query", limit),
	}
}

// Is allows to use errors.Is(err,ErrLimit) on this error.
func (e LimitError) Is(target error) bool {
	return target == ErrLimit
}
