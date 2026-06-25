package logqlmodel

import (
	"github.com/grafana/loki/v3/pkg/logqlmodel/logqlerr"
)

// The parse errors and reserved label keys below were moved to
// pkg/logqlmodel/logqlerr so that the logql parser/pipeline packages can depend
// on them without pulling in the rest of pkg/logqlmodel (and its heavy
// transitive dependencies). They are re-exported here for backwards
// compatibility; errors.Is and type assertions keep working because these are
// aliases to the same variables and types.
var (
	ErrParse                            = logqlerr.ErrParse
	ErrPipeline                         = logqlerr.ErrPipeline
	ErrLimit                            = logqlerr.ErrLimit
	ErrIntervalLimit                    = logqlerr.ErrIntervalLimit
	ErrBlocked                          = logqlerr.ErrBlocked
	ErrParseMatchers                    = logqlerr.ErrParseMatchers
	ErrUnsupportedSyntaxForInstantQuery = logqlerr.ErrUnsupportedSyntaxForInstantQuery
	ErrVariantsDisabled                 = logqlerr.ErrVariantsDisabled
	ErrorLabel                          = logqlerr.ErrorLabel
	PreserveErrorLabel                  = logqlerr.PreserveErrorLabel
	ErrorDetailsLabel                   = logqlerr.ErrorDetailsLabel
)

// ParseError is what is returned when we failed to parse.
type ParseError = logqlerr.ParseError

var (
	NewParseError = logqlerr.NewParseError
	NewStageError = logqlerr.NewStageError
)

type PipelineError = logqlerr.PipelineError

var NewPipelineErr = logqlerr.NewPipelineErr

type LimitError = logqlerr.LimitError

var NewSeriesLimitError = logqlerr.NewSeriesLimitError
