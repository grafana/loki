package plumbing

import (
	"errors"
	"fmt"
)

// ErrorSkipValidation can be returned in the Client's Validate interface to prevent the error from stopping the pipeline execution
var ErrorSkipValidation = errors.New("skipping step validation")

var (
	ErrorMissingArgument = errors.New("argument requested but not provided")
)

type PipelineError struct {
	Err         string
	Description string
}

func (p *PipelineError) Error() string {
	return fmt.Sprintf("%s: %s", p.Err, p.Description)
}

func NewPipelineError(err string, desc string) *PipelineError {
	return &PipelineError{
		Err:         err,
		Description: desc,
	}
}

type ErrorStack struct {
	Errors []error
}

func (e *ErrorStack) Push(err error) {
	e.Errors = append(e.Errors, err)
}

// Peek returns the error at the end of the stack without removing it.
func (e *ErrorStack) Peek() error {
	if len(e.Errors) == 0 {
		return nil
	}

	return e.Errors[len(e.Errors)-1]
}

// Pop returns the error at the end of the stack and removes it.
func (e *ErrorStack) Pop() error {
	if len(e.Errors) == 0 {
		return nil
	}

	err := e.Errors[len(e.Errors)-1]

	e.Errors = e.Errors[:len(e.Errors)-1]
	return err
}
