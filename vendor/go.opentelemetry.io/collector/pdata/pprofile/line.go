// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pprofile // import "go.opentelemetry.io/collector/pdata/pprofile"

import "fmt"

// Equal checks equality with another LineSlice
func (l LineSlice) Equal(val LineSlice) bool {
	if l.Len() != val.Len() {
		return false
	}

	for i := range l.Len() {
		if !l.At(i).Equal(val.At(i)) {
			return false
		}
	}

	return true
}

// Equal checks equality with another Line
func (l Line) Equal(val Line) bool {
	return l.Column() == val.Column() &&
		l.FunctionIndex() == val.FunctionIndex() &&
		l.Line() == val.Line()
}

// switchDictionary updates the Line, switching its indices from one
// dictionary to another.
func (l Line) switchDictionary(src, dst ProfilesDictionary) error {
	if l.FunctionIndex() > 0 {
		if src.FunctionTable().Len() <= int(l.FunctionIndex()) {
			return fmt.Errorf("invalid function index %d", l.FunctionIndex())
		}

		fn := src.FunctionTable().At(int(l.FunctionIndex()))
		idx, err := SetFunction(dst.FunctionTable(), fn)
		if err != nil {
			return fmt.Errorf("couldn't set function: %w", err)
		}
		l.SetFunctionIndex(idx)
	}

	return nil
}
