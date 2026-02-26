// Copyright 2025 The go-yaml Project Contributors
// SPDX-License-Identifier: Apache-2.0

// Error types for YAML parsing and emitting.
// Provides structured error reporting with line/column information.

package libyaml

import (
	"errors"
	"fmt"
	"strings"
)

type MarkedYAMLError struct {
	// optional context
	ContextMark    Mark
	ContextMessage string

	Mark    Mark
	Message string
}

func (e MarkedYAMLError) Error() string {
	var builder strings.Builder
	builder.WriteString("yaml: ")
	if len(e.ContextMessage) > 0 {
		fmt.Fprintf(&builder, "%s at %s: ", e.ContextMessage, e.ContextMark)
	}
	if len(e.ContextMessage) == 0 || e.ContextMark != e.Mark {
		fmt.Fprintf(&builder, "%s: ", e.Mark)
	}
	builder.WriteString(e.Message)
	return builder.String()
}

type ParserError MarkedYAMLError

func (e ParserError) Error() string {
	return MarkedYAMLError(e).Error()
}

type ScannerError MarkedYAMLError

func (e ScannerError) Error() string {
	return MarkedYAMLError(e).Error()
}

type ReaderError struct {
	Offset int
	Value  int
	Err    error
}

func (e ReaderError) Error() string {
	return fmt.Sprintf("yaml: offset %d: %s", e.Offset, e.Err)
}

func (e ReaderError) Unwrap() error {
	return e.Err
}

type EmitterError struct {
	Message string
}

func (e EmitterError) Error() string {
	return fmt.Sprintf("yaml: %s", e.Message)
}

type WriterError struct {
	Err error
}

func (e WriterError) Error() string {
	return fmt.Sprintf("yaml: %s", e.Err)
}

func (e WriterError) Unwrap() error {
	return e.Err
}

// ConstructError represents a single, non-fatal error that occurred during
// the constructing of a YAML document into a Go value.
type ConstructError struct {
	Err    error
	Line   int
	Column int
}

func (e *ConstructError) Error() string {
	return fmt.Sprintf("line %d: %s", e.Line, e.Err.Error())
}

func (e *ConstructError) Unwrap() error {
	return e.Err
}

// LoadErrors is returned when one or more fields cannot be properly decoded.
type LoadErrors struct {
	Errors []*ConstructError
}

func (e *LoadErrors) Error() string {
	var b strings.Builder
	b.WriteString("yaml: construct errors:")
	for _, err := range e.Errors {
		b.WriteString("\n  ")
		b.WriteString(err.Error())
	}
	return b.String()
}

// As implements errors.As for Go versions prior to 1.20 that don't support
// the Unwrap() []error interface. It allows [LoadErrors] to match against
// *ConstructError targets by returning the first error in the list.
func (e *LoadErrors) As(target any) bool {
	switch t := target.(type) {
	case **ConstructError:
		if len(e.Errors) == 0 {
			return false
		}
		*t = e.Errors[0]
		return true
	case **TypeError:
		var msgs []string
		for _, err := range e.Errors {
			msgs = append(msgs, err.Error())
		}
		*t = &TypeError{Errors: msgs}
		return true
	}
	return false
}

// Is implements errors.Is for Go versions prior to 1.20 that don't support
// the Unwrap() []error interface. It checks if any wrapped error matches
// the target error.
func (e *LoadErrors) Is(target error) bool {
	for _, err := range e.Errors {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}

// TypeError is an obsolete error type retained for compatibility.
//
// A TypeError is returned by Unmarshal when one or more fields in
// the YAML document cannot be properly decoded into the requested
// types. When this error is returned, the value is still
// unmarshaled partially.
//
// Deprecated: Use [LoadErrors] instead.
type TypeError struct {
	Errors []string
}

func (e *TypeError) Error() string {
	return fmt.Sprintf("yaml: unmarshal errors:\n  %s", strings.Join(e.Errors, "\n  "))
}

// YAMLError is an internal error wrapper type.
type YAMLError struct {
	Err error
}

func (e *YAMLError) Error() string {
	return e.Err.Error()
}
