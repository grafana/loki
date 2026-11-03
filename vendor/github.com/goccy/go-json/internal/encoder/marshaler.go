package encoder

import (
	"context"
	"reflect"
	"unsafe"

	"github.com/goccy/go-json/internal/runtime"
)

// A method of a marshaler is called directly, by the code of the method, instead of through the interface:
// making the interface value, asserting the interface and calling through it cost more than the call.
//
// A call through an interface passes the data word of the interface value as the receiver: the value itself
// if the type is stored directly in an interface value ( a pointer, a map, ... ), or the address of the value.
// The opcodes of a marshaler carry that very word. The method which takes the word is found by reflect: the
// method of the type itself for a type stored directly, or the method of the pointer to the type otherwise,
// which is the method on the pointer or the wrapper of the method on the value. reflect.Value.Pointer gives the
// code of a function. A func value is a pointer to a closure whose first word is the code, and the first
// argument of a func value is passed as the receiver of a method is, so the code is called as a func value.

// MarshalerCall is the method of a marshaler, as it is called with the data word of the interface value.
type MarshalerCall struct {
	// fn is the code of the method: it is the first word, so that a pointer to MarshalerCall is a func value.
	fn uintptr
	// nilIsNull is whether a nil receiver is encoded as null without a call, which is so for a pointer.
	nilIsNull bool
	// trusted is whether the output of MarshalJSON is valid and compact, as the one of a type of the standard
	// library is, or of a type which has it only by embedding one ( see runtime.IsStdMarshalerType and
	// runtime.PromotedStdMethod ): it is not checked.
	trusted bool
	// v2Raw is whether the method is MarshalJSON of a value of the v2 semantics at its address, which
	// AppendMarshalJSON calls by fn, as the encoder of v1 calls it, and whose output is a raw value of the v2
	// semantics ( see appendV2Raw ): recv is then the type of the value. appendValue, which does the same by a
	// call more, is set as well, so that AppendMarshalJSON looks at v2Raw only behind its check of appendValue.
	v2Raw bool
	// recv is the type of the receiver, for an error.
	recv reflect.Type
	// appendOutput writes what the method of a type of the standard library returns without calling it, by the
	// appending method of the type: the output of MarshalJSON, valid and compact ( see stdJSONAppender ), or the
	// text of MarshalText, by AppendText. It returns false, and writes nothing, when the method is to be called,
	// as for an error, which is then the one of the method.
	appendOutput func(b []byte, recv unsafe.Pointer) ([]byte, bool)
	// appendValue, if it is set, writes the value at the address recv instead of a method: the representation of
	// a type which the compiler gives by a function ( see Compiler.appendFuncCode ).
	appendValue AppendFunc
	// marshalTo, if it is set, returns what MarshalJSONTo of the receiver writes, which the encoder of v1 writes as
	// the output of MarshalJSON, or appends the value after b by the representation after the method, if the method
	// declined it, and reports so ( see Compiler.marshalToCall ).
	marshalTo func(ctx *RuntimeContext, b []byte, recv unsafe.Pointer) (out []byte, appended bool, err error)
}

// AppendFunc appends the JSON value of the value at p, the address of the value.
type AppendFunc func(ctx *RuntimeContext, b []byte, p unsafe.Pointer) ([]byte, error)

func (m *MarshalerCall) call(recv unsafe.Pointer) ([]byte, error) {
	f := *(*func(unsafe.Pointer) ([]byte, error))(unsafe.Pointer(&m))
	return f(recv)
}

func (m *MarshalerCall) callContext(recv unsafe.Pointer, ctx context.Context) ([]byte, error) {
	f := *(*func(unsafe.Pointer, context.Context) ([]byte, error))(unsafe.Pointer(&m))
	return f(recv, ctx)
}

// marshalerInterface is an interface of a marshaler, with the lookup of its method by the name of the method as a
// constant ( see runtime.MethodLookup ): a method is never looked up by a name in a variable, so that the linker can
// drop the methods which no one calls.
type marshalerInterface struct {
	typ    reflect.Type
	method runtime.MethodLookup
}

var (
	marshalJSONInterface        = &marshalerInterface{typ: marshalJSONType, method: runtime.MarshalJSONMethod}
	marshalJSONContextInterface = &marshalerInterface{typ: marshalJSONContextType, method: runtime.MarshalJSONMethod}
	marshalTextInterface        = &marshalerInterface{typ: marshalTextType, method: runtime.MarshalTextMethod}
	appendTextInterface         = &marshalerInterface{typ: appendTextType, method: runtime.AppendTextMethod}
)

// newMarshalerCall returns the call of the method of the type of the receiver, which implements the interface.
// It is decided when the type is compiled.
func newMarshalerCall(recv reflect.Type, iface *marshalerInterface) *MarshalerCall {
	fn, ok := methodCode(recv, iface)
	if !ok {
		return nil
	}
	m := &MarshalerCall{fn: fn, nilIsNull: recv.Kind() == reflect.Ptr, recv: recv}
	if runtime.IsStdMarshalerType(recv) {
		switch iface {
		case marshalJSONInterface:
			m.trusted = true
			m.appendOutput = stdJSONAppender(recv)
		case marshalTextInterface:
			m.appendOutput = textAppenderOf(recv)
		}
	} else if origin, offset, inline, ok := runtime.PromotedStdMethod(recv, iface.method); ok {
		// the method is the one of the embedded value of a type of the standard library, which the wrapper of the
		// type calls: its output is trusted as that of the type, and written by its appending method from the
		// embedded value if it is in the value, whose address is then the data word: a pointer, or a type which
		// is not stored directly in an interface value.
		switch iface {
		case marshalJSONInterface:
			m.trusted = true
			if inline && (recv.Kind() == reflect.Pointer || runtime.IfaceIndir(recv)) {
				m.appendOutput = embeddedAppender(stdJSONAppender(origin), offset)
			}
		case marshalTextInterface:
			if inline && (recv.Kind() == reflect.Pointer || runtime.IfaceIndir(recv)) {
				m.appendOutput = embeddedAppender(textAppenderOf(origin), offset)
			}
		}
	}
	return m
}

// embeddedAppender returns the appending function of the value embedded at the offset of the receiver, whose data
// word is the address of the value which embeds it, or nil if the function is.
func embeddedAppender(appendOutput func([]byte, unsafe.Pointer) ([]byte, bool), offset uintptr) func([]byte, unsafe.Pointer) ([]byte, bool) {
	if appendOutput == nil {
		return nil
	}
	return func(b []byte, recv unsafe.Pointer) ([]byte, bool) {
		return appendOutput(b, unsafe.Add(recv, offset))
	}
}

// methodCode returns the code of the method of the interface of the receiver type, which takes the data word of
// the interface value.
func methodCode(recv reflect.Type, iface *marshalerInterface) (uintptr, bool) {
	if !recv.Implements(iface.typ) {
		return 0, false
	}
	holder := recv
	if runtime.IfaceIndir(recv) {
		// the data word is the address of the value: the method of the pointer takes it.
		holder = reflect.PointerTo(recv)
	}
	method, ok := iface.method(holder)
	if !ok {
		return 0, false
	}
	return method.Func.Pointer(), true
}

// textAppender is encoding.TextAppender, which is of Go 1.24: the types of the standard library have it from
// that release on.
type textAppender interface {
	AppendText(b []byte) ([]byte, error)
}

var appendTextType = reflect.TypeOf((*textAppender)(nil)).Elem()

// textAppenderOf returns the function which appends the text of a value of the type of the standard library by
// AppendText, which appends what MarshalText returns, or nil if the type has no AppendText. AppendText fails
// when MarshalText does: MarshalText is then called for its error.
func textAppenderOf(recv reflect.Type) func([]byte, unsafe.Pointer) ([]byte, bool) {
	fn, ok := methodCode(recv, appendTextInterface)
	if !ok {
		return nil
	}
	code := &fn // a func value is a pointer to its code, as MarshalerCall is
	appendText := *(*func(unsafe.Pointer, []byte) ([]byte, error))(unsafe.Pointer(&code))
	return func(b []byte, p unsafe.Pointer) ([]byte, bool) {
		out, err := appendText(p, b)
		if err != nil {
			return b, false
		}
		return out, true
	}
}
