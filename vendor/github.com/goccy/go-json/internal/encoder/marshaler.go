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
	// recv is the type of the receiver, for an error.
	recv reflect.Type
}

func (m *MarshalerCall) call(recv unsafe.Pointer) ([]byte, error) {
	f := *(*func(unsafe.Pointer) ([]byte, error))(unsafe.Pointer(&m))
	return f(recv)
}

func (m *MarshalerCall) callContext(recv unsafe.Pointer, ctx context.Context) ([]byte, error) {
	f := *(*func(unsafe.Pointer, context.Context) ([]byte, error))(unsafe.Pointer(&m))
	return f(recv, ctx)
}

// newMarshalerCall returns the call of the method of the type of the receiver, which implements the interface.
// It is decided when the type is compiled.
func newMarshalerCall(recv reflect.Type, iface reflect.Type) *MarshalerCall {
	if !recv.Implements(iface) {
		return nil
	}
	holder := recv
	if runtime.IfaceIndir(recv) {
		// the data word is the address of the value: the method of the pointer takes it.
		holder = reflect.PointerTo(recv)
	}
	method, ok := holder.MethodByName(iface.Method(0).Name)
	if !ok {
		return nil
	}
	return &MarshalerCall{fn: method.Func.Pointer(), nilIsNull: recv.Kind() == reflect.Ptr, recv: recv}
}
