package runtime

import (
	"reflect"
	"unsafe"
)

type emptyInterface struct {
	typ unsafe.Pointer
	ptr unsafe.Pointer
}

// TypePtr returns the pointer to the type descriptor of the type.
// It is the type word of an interface value which holds a value of the type,
// and it is what the functions of the runtime package take as a type.
func TypePtr(t reflect.Type) unsafe.Pointer {
	return (*emptyInterface)(unsafe.Pointer(&t)).ptr
}

// TypeOfPtr converts the pointer to the type descriptor to reflect.Type.
// reflect.TypeOf reads only the type word of the interface value, so the data word is left nil.
func TypeOfPtr(typ unsafe.Pointer) reflect.Type {
	return reflect.TypeOf(*(*any)(unsafe.Pointer(&emptyInterface{typ: typ})))
}

// IfaceIndir reports whether a value of the type is stored indirectly in an interface value:
// the data word of the interface points to a copy of the value, instead of being the value itself.
//
// Which types are stored directly is decided by the compiler and the rule depends on the Go version
// ( e.g. a struct of a zero-sized field and a pointer is direct since Go 1.26 ), so the rule is not
// reimplemented here. Instead, a value whose first word is a known pointer is converted to an interface
// value, and the data word tells how it was stored.
func IfaceIndir(typ reflect.Type) bool {
	if typ.Size() != unsafe.Sizeof(unsafe.Pointer(nil)) {
		// only a pointer-sized value fits in the data word.
		return true
	}
	probe := unsafe.Pointer(new(uintptr))
	v := reflect.New(typ)
	*(*unsafe.Pointer)(v.UnsafePointer()) = probe
	iface := v.Elem().Interface()
	return (*emptyInterface)(unsafe.Pointer(&iface)).ptr != probe
}
