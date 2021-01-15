package syntax

import (
	"errors"
	"fmt"
)

// Those errors are useful for comparing error returned by the engine.
// e.g. errors.Is(err,logql.ErrParse) let you know if this is a ast parsing error.
var (
	ErrParse    = errors.New("failed to parse the log query")
	ErrPipeline = errors.New("failed execute pipeline")
	ErrLimit    = errors.New("limit reached while evaluating the query")
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

func newParseError(msg string, line, col int) ParseError {
	return ParseError{
		msg:  msg,
		line: line,
		col:  col,
	}
}
