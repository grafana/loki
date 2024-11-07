package syntax

import (
	"errors"
	"fmt"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logql/log"
)

var (
	ErrParse         = errors.New("failed to parse the log query")
	ErrPipeline      = errors.New("failed execute pipeline")
	ErrParseMatchers = errors.New("only label matchers are supported")
)

// ParseError is what is returned when we failed to parse.
type ParseError struct {
	msg       string
	line, col int
}

func (p ParseError) Error() string {
	if p.col == 0 && p.line == 0 {
		return fmt.Sprintf("parse error : %s", p.msg)
	}
	return fmt.Sprintf("parse error at line %d, col %d: %s", p.line, p.col, p.msg)
}

// Is allows to use errors.Is(err,ErrParse) on this error.
func (p ParseError) Is(target error) bool {
	return target == ErrParse
}

func NewParseError(msg string, line, col int) ParseError {
	return ParseError{
		msg:  msg,
		line: line,
		col:  col,
	}
}

func NewStageError(expr string, err error) ParseError {
	return ParseError{
		msg:  fmt.Sprintf(`stage '%s' : %s`, expr, err),
		line: 0,
		col:  0,
	}
}

type PipelineError struct {
	metric    labels.Labels
	errorType string
}

func NewPipelineErr(metric labels.Labels) *PipelineError {
	return &PipelineError{
		metric:    metric,
		errorType: metric.Get(log.ErrorLabel),
	}
}

func (e PipelineError) Error() string {
	return fmt.Sprintf(
		"pipeline error: '%s' for series: '%s'.\n"+
			"Use a label filter to intentionally skip this error. (e.g | __error__!=\"%s\").\n"+
			"To skip all potential errors you can match empty errors.(e.g __error__=\"\")\n"+
			"The label filter can also be specified after unwrap. (e.g | unwrap latency | __error__=\"\" )\n",
		e.errorType, e.metric, e.errorType)
}

// Is allows to use errors.Is(err,ErrPipeline) on this error.
func (e PipelineError) Is(target error) bool {
	return target == ErrPipeline
}
