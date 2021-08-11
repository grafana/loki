// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package errutil

import (
	"bytes"
	"fmt"
)

// The MultiError type implements the error interface, and contains the
// Errors used to construct it.
type MultiError []error

// Add adds the error to the error list if it is not nil.
func (es *MultiError) Add(err error) {
	if err == nil {
		return
	}
	if merr, ok := err.(NonNilMultiError); ok {
		*es = append(*es, merr...)
	} else {
		*es = append(*es, err)
	}
}

// Err returns the error list as an error or nil if it is empty.
func (es MultiError) Err() error {
	if len(es) == 0 {
		return nil
	}
	return NonNilMultiError(es)
}

type NonNilMultiError MultiError

// Returns a concatenated string of the contained errors.
func (es NonNilMultiError) Error() string {
	var buf bytes.Buffer

	if len(es) > 1 {
		fmt.Fprintf(&buf, "%d errors: ", len(es))
	}

	for i, err := range es {
		if i != 0 {
			buf.WriteString("; ")
		}
		buf.WriteString(err.Error())
	}

	return buf.String()
}
