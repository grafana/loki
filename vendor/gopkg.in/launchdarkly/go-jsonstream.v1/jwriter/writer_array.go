package jwriter

import "encoding/json"

// ArrayState is a decorator that manages the state of a JSON array that is in the process of being
// written.
//
// Calling Writer.Array() or ObjectState.Array() creates an ArrayState. Until ArrayState.End() is
// called, writing any value to either the ArrayState or the Writer will cause commas to be added
// between values as needed.
type ArrayState struct {
	w             *Writer
	previousState writerState
}

// Null is equivalent to writer.Null().
func (arr *ArrayState) Null() {
	if arr.w != nil {
		arr.w.Null()
	}
}

// Bool is equivalent to writer.Bool(value).
func (arr *ArrayState) Bool(value bool) {
	if arr.w != nil {
		arr.w.Bool(value)
	}
}

// Int is equivalent to writer.Int(value).
func (arr *ArrayState) Int(value int) {
	if arr.w != nil {
		arr.w.Int(value)
	}
}

// Float64 is equivalent to writer.Float64(value).
func (arr *ArrayState) Float64(value float64) {
	if arr.w != nil {
		arr.w.Float64(value)
	}
}

// String is equivalent to writer.String(value).
func (arr *ArrayState) String(value string) {
	if arr.w != nil {
		arr.w.String(value)
	}
}

// Array is equivalent to calling writer.Array(), to create a nested array.
func (arr *ArrayState) Array() ArrayState {
	if arr.w != nil {
		return arr.w.Array()
	}
	return ArrayState{}
}

// Object is equivalent to calling writer.Object(), to create a nested object.
func (arr *ArrayState) Object() ObjectState {
	if arr.w != nil {
		return arr.w.Object()
	}
	return ObjectState{}
}

// Raw is equivalent to calling writer.Raw().
func (arr *ArrayState) Raw(value json.RawMessage) {
	if arr.w != nil {
		arr.w.Raw(value)
	}
}

// End writes the closing delimiter of the array.
func (arr *ArrayState) End() {
	if arr.w == nil || arr.w.err != nil {
		return
	}
	arr.w.AddError(arr.w.tw.Delimiter(']'))
	arr.w.state = arr.previousState
	arr.w = nil
}
