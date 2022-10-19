package jwriter

import (
	"encoding/json"
)

// Writer is a high-level API for writing JSON data sequentially.
//
// It is designed to make writing custom marshallers for application types as convenient as
// possible. The general usage pattern is as follows:
//
// - There is one method for each JSON data type.
//
// - For writing array or object structures, the Array and Object methods return a struct that
// keeps track of additional writer state while that structure is being written.
//
// - If any method encounters an error (for instance, if an underlying io.Writer returns an error
// when using NewStreamingWriter), or if an error is explicitly raised with AddError, the Writer
// permanently enters a failed state and remembers that error; all subsequent method calls for
// producing output will be ignored.
type Writer struct {
	tw    tokenWriter
	err   error
	state writerState
}

// writerState keeps track of semantic state such as whether we're within an array. This has
// stack-like behavior, but to avoid allocating an actual stack, we use ArrayState and
// ObjectState to hold previous values of this struct.
type writerState struct {
	inArray       bool
	arrayHasItems bool
}

// Bytes returns the full contents of the output buffer.
func (w *Writer) Bytes() []byte {
	return w.tw.Bytes()
}

// Error returns the first error, if any, that occurred during output generation. If there have
// been no errors, it returns nil.
//
// As soon as any operation fails at any level, either in the JSON encoding or in writing to an
// underlying io.Writer, the Writer remembers the error and will generate no further output.
func (w *Writer) Error() error {
	return w.err
}

// AddError sets the error state if an error has not already been recorded.
func (w *Writer) AddError(err error) {
	if w.err == nil {
		w.err = err
	}
}

// Flush writes any remaining in-memory output to the underlying io.Writer, if this is a streaming
// writer created with NewStreamingWriter. It has no effect otherwise.
func (w *Writer) Flush() error {
	return w.tw.Flush()
}

// Null writes a JSON null value to the output.
func (w *Writer) Null() {
	if w.beforeValue() {
		w.AddError(w.tw.Null())
	}
}

// Bool writes a JSON boolean value to the output.
func (w *Writer) Bool(value bool) {
	if w.beforeValue() {
		w.AddError(w.tw.Bool(value))
	}
}

// BoolOrNull is a shortcut for calling Bool(value) if isDefined is true, or else
// Null().
func (w *Writer) BoolOrNull(isDefined bool, value bool) {
	if isDefined {
		w.Bool(value)
	} else {
		w.Null()
	}
}

// Int writes a JSON numeric value to the output.
func (w *Writer) Int(value int) {
	if w.beforeValue() {
		w.AddError(w.tw.Int(value))
	}
}

// IntOrNull is a shortcut for calling Int(value) if isDefined is true, or else
// Null().
func (w *Writer) IntOrNull(isDefined bool, value int) {
	if isDefined {
		w.Int(value)
	} else {
		w.Null()
	}
}

// Float64 writes a JSON numeric value to the output.
func (w *Writer) Float64(value float64) {
	if w.beforeValue() {
		w.AddError(w.tw.Float64(value))
	}
}

// Float64OrNull is a shortcut for calling Float64(value) if isDefined is true, or else
// Null().
func (w *Writer) Float64OrNull(isDefined bool, value float64) {
	if isDefined {
		w.Float64(value)
	} else {
		w.Null()
	}
}

// String writes a JSON string value to the output, adding quotes and performing any necessary escaping.
func (w *Writer) String(value string) {
	if w.beforeValue() {
		w.AddError(w.tw.String(value))
	}
}

// StringOrNull is a shortcut for calling String(value) if isDefined is true, or else
// Null().
func (w *Writer) StringOrNull(isDefined bool, value string) {
	if isDefined {
		w.String(value)
	} else {
		w.Null()
	}
}

// Raw writes a pre-encoded JSON value to the output as-is. Its format is assumed to be correct; this
// operation will not fail unless it is not permitted to write a value at this point.
func (w *Writer) Raw(value json.RawMessage) {
	if value == nil {
		w.Null()
	} else if w.beforeValue() {
		w.AddError(w.tw.Raw(value))
	}
}

// Array begins writing a JSON array to the output. It returns an ArrayState that provides the array
// formatting; you must call ArrayState.End() when finished.
func (w *Writer) Array() ArrayState {
	if w.beforeValue() {
		if err := w.tw.Delimiter('['); err != nil {
			w.err = err
			return ArrayState{}
		}
		previousState := w.state
		w.state = writerState{inArray: true}
		return ArrayState{w: w, previousState: previousState}
	}
	return ArrayState{}
}

// Object begins writing a JSON object to the output. It returns an ObjectState that provides the
// object formatting; you must call ObjectState.End() when finished.
func (w *Writer) Object() ObjectState {
	if w.beforeValue() {
		if err := w.tw.Delimiter('{'); err != nil {
			w.err = err
			return ObjectState{}
		}
		previousState := w.state
		w.state = writerState{inArray: false}
		return ObjectState{w: w, previousState: previousState}
	}
	return ObjectState{}
}

func (w *Writer) beforeValue() bool {
	if w.err != nil {
		return false
	}
	if w.state.inArray {
		if w.state.arrayHasItems {
			if err := w.tw.Delimiter(','); err != nil {
				w.AddError(err)
				return false
			}
		} else {
			w.state.arrayHasItems = true
		}
	}
	return true
}
