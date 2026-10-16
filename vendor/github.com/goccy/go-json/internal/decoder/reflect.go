package decoder

import (
	"reflect"
	"time"
	"unsafe"

	"github.com/goccy/go-json/internal/runtime"
)

// The decoders allocate and assign values through the reflect package, never through the internal
// functions of the runtime: the pointers to the type descriptors they need are computed once, when
// a decoder is compiled.

// valueAt returns the addressable reflect.Value of the value at p, whose pointer type is ptrType
// ( the type descriptor of *T for a value of T ). It is reflect.NewAt(T, p).Elem() without the lookup
// of the pointer type which reflect.NewAt does for every call.
func valueAt(ptrType, p unsafe.Pointer) reflect.Value {
	return reflect.ValueOf(*(*any)(unsafe.Pointer(&emptyInterface{typ: ptrType, ptr: p}))).Elem()
}

// ptrTypeOf returns the type descriptor of *typ.
func ptrTypeOf(typ reflect.Type) unsafe.Pointer {
	return runtime.TypePtr(reflect.PointerTo(typ))
}

// newValue allocates a zero value of typ.
func newValue(typ reflect.Type) unsafe.Pointer {
	return reflect.New(typ).UnsafePointer()
}

// allocatorOf returns the function which allocates a zero value of typ, for a decoder which allocates one for
// every value it decodes, as the one of a pointer does. For the kinds of the basic types it is new of the basic
// type of the kind, whose layout, pointers included, is the one of typ whatever its name; for time.Time, it is
// new of it. They cost less than reflect.New, which looks up the pointer type of typ at every call. It is nil
// for any other type, whose pointer is decoded by ptrDecoder, which allocates by newValue ( see basicPtrDecoder ).
func allocatorOf(typ reflect.Type) func() unsafe.Pointer {
	if typ == reflect.TypeOf(time.Time{}) {
		return func() unsafe.Pointer { return unsafe.Pointer(new(time.Time)) }
	}
	switch typ.Kind() {
	case reflect.Bool:
		return func() unsafe.Pointer { return unsafe.Pointer(new(bool)) }
	case reflect.Int:
		return func() unsafe.Pointer { return unsafe.Pointer(new(int)) }
	case reflect.Int8:
		return func() unsafe.Pointer { return unsafe.Pointer(new(int8)) }
	case reflect.Int16:
		return func() unsafe.Pointer { return unsafe.Pointer(new(int16)) }
	case reflect.Int32:
		return func() unsafe.Pointer { return unsafe.Pointer(new(int32)) }
	case reflect.Int64:
		return func() unsafe.Pointer { return unsafe.Pointer(new(int64)) }
	case reflect.Uint:
		return func() unsafe.Pointer { return unsafe.Pointer(new(uint)) }
	case reflect.Uint8:
		return func() unsafe.Pointer { return unsafe.Pointer(new(uint8)) }
	case reflect.Uint16:
		return func() unsafe.Pointer { return unsafe.Pointer(new(uint16)) }
	case reflect.Uint32:
		return func() unsafe.Pointer { return unsafe.Pointer(new(uint32)) }
	case reflect.Uint64:
		return func() unsafe.Pointer { return unsafe.Pointer(new(uint64)) }
	case reflect.Uintptr:
		return func() unsafe.Pointer { return unsafe.Pointer(new(uintptr)) }
	case reflect.Float32:
		return func() unsafe.Pointer { return unsafe.Pointer(new(float32)) }
	case reflect.Float64:
		return func() unsafe.Pointer { return unsafe.Pointer(new(float64)) }
	case reflect.Complex64:
		return func() unsafe.Pointer { return unsafe.Pointer(new(complex64)) }
	case reflect.Complex128:
		return func() unsafe.Pointer { return unsafe.Pointer(new(complex128)) }
	case reflect.String:
		return func() unsafe.Pointer { return unsafe.Pointer(new(string)) }
	}
	return nil
}

// zeroBase is the address of the data of an empty slice which is not nil.
var zeroBase [0]byte
