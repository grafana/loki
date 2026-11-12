//go:build go1.24

package encoder

import (
	"reflect"
	"time"
	"unsafe"
)

// The output of time.Time.MarshalJSON, time.Time being the type of the standard library which is encoded the
// most, is known: the text of AppendText quoted. It is written by AppendText, into the buffer, without the
// allocation of the output. The other types of the standard library are called, and their outputs are not checked
// ( see MarshalerCall.trusted ).
//
// time is the one package of the standard library which is imported for its types ( the others are found by their
// paths, see runtime.IsStdMarshalerType ): it is linked into every program which uses go-json anyway, as context
// imports it, and AppendText called directly costs less than called by its code, as a method found by reflect is.

var (
	timeType    = reflect.TypeOf(time.Time{})
	timePtrType = reflect.PointerTo(timeType)
)

// stdJSONAppender returns the function which writes the output of MarshalJSON of the type of the standard library,
// or nil if it is called. The receiver is the data word of the interface value ( see MarshalerCall ): the address
// of a time.Time, for a time.Time and for a *time.Time alike.
func stdJSONAppender(recv reflect.Type) func([]byte, unsafe.Pointer) ([]byte, bool) {
	if recv != timeType && recv != timePtrType {
		return nil
	}
	return appendTimeJSON
}

// appendTimeJSON writes what time.Time.MarshalJSON returns: the text of AppendText quoted, which has nothing to
// escape. AppendText fails when MarshalJSON does, whose error is then the one of the call of MarshalJSON.
func appendTimeJSON(b []byte, p unsafe.Pointer) ([]byte, bool) {
	out, err := (*time.Time)(p).AppendText(append(b, '"'))
	if err != nil {
		return b, false
	}
	return append(out, '"'), true
}
