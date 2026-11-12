package runtime

import "reflect"

// MethodLookup looks the method of one name up in a type, by reflect.Type.MethodByName with the name as a constant.
//
// The compiler tells the linker which names the calls of Method and MethodByName of reflect look up when it sees
// them as constants, and the linker keeps the methods of those names only. A call with any other argument makes
// the linker keep every exported method of every type of the program ( cmd/compile/internal/walk.usemethod ), so
// the methods are looked up by these functions, each with its name in its code, and never by a name in a
// variable.
type MethodLookup func(reflect.Type) (reflect.Method, bool)

// MarshalJSONMethod looks MarshalJSON up.
func MarshalJSONMethod(typ reflect.Type) (reflect.Method, bool) {
	return typ.MethodByName("MarshalJSON")
}

// MarshalTextMethod looks MarshalText up.
func MarshalTextMethod(typ reflect.Type) (reflect.Method, bool) {
	return typ.MethodByName("MarshalText")
}

// AppendTextMethod looks AppendText up.
func AppendTextMethod(typ reflect.Type) (reflect.Method, bool) {
	return typ.MethodByName("AppendText")
}

// UnmarshalJSONMethod looks UnmarshalJSON up.
func UnmarshalJSONMethod(typ reflect.Type) (reflect.Method, bool) {
	return typ.MethodByName("UnmarshalJSON")
}
