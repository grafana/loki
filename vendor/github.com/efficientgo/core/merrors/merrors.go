// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package merrors

import (
	"bytes"
	stderrors "errors"
	"fmt"
	"io"
)

// NilOrMultiError type allows combining multiple errors into one.
type NilOrMultiError struct {
	errs []error
}

// New returns NilOrMultiError with provided errors added if not nil.
func New(errs ...error) *NilOrMultiError {
	m := &NilOrMultiError{}
	m.Add(errs...)
	return m
}

// Add adds single or many errors to the error list. Each error is added only if not nil.
// If the error is a multiError type, the errors inside multiError are added to the main NilOrMultiError.
func (e *NilOrMultiError) Add(errs ...error) {
	for _, err := range errs {
		if err == nil {
			continue
		}
		if merr, ok := err.(multiError); ok {
			e.errs = append(e.errs, merr.errs...)
			continue
		}
		e.errs = append(e.errs, err)
	}
}

// Err returns the error list as an Error (also implements error) or nil if it is empty.
func (e NilOrMultiError) Err() Error {
	if len(e.errs) == 0 {
		return nil
	}
	return multiError(e)
}

// Error is extended error interface that allows to use returned read-only multi error in more advanced ways.
type Error interface {
	error

	// Errors returns underlying errors.
	Errors() []error

	// As finds the first error in multiError slice of error chains that matches target, and if so, sets
	// target to that error value and returns true. Otherwise, it returns false.
	//
	// An error matches target if the error's concrete value is assignable to the value
	// pointed to by target, or if the error has a method As(interface{}) bool such that
	// As(target) returns true. In the latter case, the As method is responsible for
	// setting target.
	As(target interface{}) bool
	// Is returns true if any error in multiError's slice of error chains matches the given target or
	// if the target is of multiError type.
	//
	// An error is considered to match a target if it is equal to that target or if
	// it implements a method Is(error) bool such that Is(target) returns true.
	Is(target error) bool
	// Count returns the number of multi error' errors that match the given target.
	// Matching is defined as in Is method.
	Count(target error) int
}

// multiError implements the error and Error interfaces, and it represents NilOrMultiError (in other words []error) with at least one error inside it.
// NOTE: This type is useful to make sure that NilOrMultiError is not accidentally used for err != nil check.
type multiError struct {
	errs []error
}

// Errors returns underlying errors.
func (e multiError) Errors() []error {
	return e.errs
}

// Error returns a concatenated string of the contained errors.
func (e multiError) Error() string {
	var buf bytes.Buffer

	if len(e.errs) > 1 {
		fmt.Fprintf(&buf, "%d errors: ", len(e.errs))
	}
	for i, err := range e.errs {
		if i != 0 {
			buf.WriteString("; ")
		}
		buf.WriteString(err.Error())
	}

	return buf.String()
}

// As finds the first error in multiError slice of error chains that matches target, and if so, sets
// target to that error value and returns true. Otherwise, it returns false.
//
// An error matches target if the error's concrete value is assignable to the value
// pointed to by target, or if the error has a method As(interface{}) bool such that
// As(target) returns true. In the latter case, the As method is responsible for
// setting target.
func (e multiError) As(target interface{}) bool {
	if t, ok := target.(*multiError); ok {
		*t = e
		return true
	}

	for _, err := range e.errs {
		if stderrors.As(err, target) {
			return true
		}
	}
	return false
}

// Is returns true if any error in multiError's slice of error chains matches the given target or
// if the target is of multiError type.
//
// An error is considered to match a target if it is equal to that target or if
// it implements a method Is(error) bool such that Is(target) returns true.
func (e multiError) Is(target error) bool {
	if m, ok := target.(multiError); ok {
		if len(m.errs) != len(e.errs) {
			return false
		}
		for i := 0; i < len(e.errs); i++ {
			if !stderrors.Is(m.errs[i], e.errs[i]) {
				return false
			}
		}
		return true
	}
	for _, err := range e.errs {
		if stderrors.Is(err, target) {
			return true
		}
	}
	return false
}

// Count returns the number of all multi error' errors that match the given target (including nested multi errors).
// Matching is defined as in Is method.
func (e multiError) Count(target error) (count int) {
	for _, err := range e.errs {
		if inner, ok := AsMulti(err); ok {
			count += inner.Count(target)
			continue
		}

		if stderrors.Is(err, target) {
			count++
		}
	}
	return count
}

// AsMulti casts error to multi error read only interface. It returns multi error and true if error matches multi error as
// defined by As method. If returns false if no multi error can be found.
func AsMulti(err error) (Error, bool) {
	m := multiError{}
	if !stderrors.As(err, &m) {
		return nil, false
	}
	return m, true
}

// Merge merges multiple Error to single one, but joining all errors together.
// NOTE: Nested multi errors are not merged.
func Merge(errs []Error) Error {
	e := multiError{}
	for _, err := range errs {
		e.errs = append(e.errs, err.Errors()...)
	}
	return e
}

// PrettyPrint prints the same information as multiError.Error() method but with newlines and indentation targeted
// for humans.
func PrettyPrint(w io.Writer, err Error) error {
	return prettyPrint(w, "\t", err)
}

func prettyPrint(w io.Writer, indent string, merr Error) error {
	if len(merr.Errors()) > 1 {
		if _, err := fmt.Fprintf(w, "%d errors:\n", len(merr.Errors())); err != nil {
			return err
		}
	}
	for i, err := range merr.Errors() {
		if i != 0 {
			if _, err := w.Write([]byte("\n")); err != nil {
				return err
			}
		}

		if merr2, ok := AsMulti(err); ok {
			if err := prettyPrint(w, indent+"\t", merr2); err != nil {
				return nil
			}
			continue
		}

		if _, err := w.Write([]byte(indent + err.Error())); err != nil {
			return err
		}
	}
	return nil
}
