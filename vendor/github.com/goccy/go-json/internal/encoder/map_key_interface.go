//go:build go1.27 && goexperiment.jsonv2

package encoder

import (
	"encoding"
	jsonv1 "encoding/json"
	jsonv2 "encoding/json/v2"
	"math"
	"reflect"
	"strconv"
	"unsafe"

	"github.com/goccy/go-json/internal/errors"
	"github.com/goccy/go-json/internal/runtime"
)

// The keys of a map of an interface type, which encoding/json encodes when it is built on encoding/json/v2
// ( GOEXPERIMENT=jsonv2, the default since Go 1.27; this file uses the API of Go 1.27 ): the name of a key is
// made from its dynamic value as a value in the place of a name is, and a key whose value is not written as a
// string is an error.
//
//   - a string, an integer, a float32 and a float64 are their text; a float64 held by a key of interface{}
//     is not, as interface{} writes it as a number.
//   - a value with AppendText or MarshalText of its value, or of its pointer through a pointer, is its text;
//     MarshalJSON is not called for a name.
//   - a pointer is the name of its value.
//   - nil, a bool, a nil pointer, a struct, an array, a slice or a map is not a name; a complex number, a
//     channel or a function is an unsupported type.
//
// The keys are sorted by their names, as the keys of the maps whose keys are not strings are ( see OpMapEnd ).

// interfaceMapKeys is whether a map of an interface type as its key is encoded.
const interfaceMapKeys = true

var (
	emptyInterfaceType  = reflect.TypeOf((*any)(nil)).Elem()
	float64Type         = reflect.TypeOf(float64(0))
	textAppenderType    = reflect.TypeOf((*encoding.TextAppender)(nil)).Elem()
	jsonMarshalerType   = reflect.TypeOf((*jsonv1.Marshaler)(nil)).Elem()
	jsonMarshalerToType = reflect.TypeOf((*jsonv2.MarshalerTo)(nil)).Elem()
)

// appendInterfaceMapKey appends the name of the key of a map of an interface type as a string. p is the address
// of the key as the map is read into the context: an interface{}, whatever the interface type of the key is
// ( see newReflectCollector ).
func appendInterfaceMapKey(ctx *RuntimeContext, code *Opcode, b []byte, p unsafe.Pointer) ([]byte, error) {
	keyType := runtime.TypeOfPtr(code.Type)
	name, err := interfaceMapKeyName(keyType, reflect.NewAt(emptyInterfaceType, p).Elem())
	if err != nil {
		return nil, err
	}
	return appendText(ctx, b, name), nil
}

// interfaceMapKeyName returns the name of the key, which is a value of the interface type keyType.
func interfaceMapKeyName(keyType reflect.Type, key reflect.Value) (string, error) {
	// A key of an interface type with methods is set to the interface{} as the value of its interface type,
	// which holds the dynamic value.
	v := key
	for v.Kind() == reflect.Interface {
		if v.IsNil() {
			return "", notStringMapKey(v)
		}
		v = v.Elem()
	}
	if v.Kind() == reflect.Pointer && v.IsNil() {
		// The method of the marshaler which the interface type of the key is, if any, is called with the nil
		// pointer. A MarshalJSON is not called for a name.
		switch {
		case keyType.Implements(jsonMarshalerToType), keyType.Implements(jsonMarshalerType):
			return "", notStringMapKey(v)
		case keyType.Implements(textAppenderType):
			return appendTextName(v.Interface().(encoding.TextAppender))
		case keyType.Implements(marshalTextType):
			return marshalTextName(v.Interface().(encoding.TextMarshaler))
		}
		return "", notStringMapKey(v)
	}
	if keyType == emptyInterfaceType && v.Type() == float64Type {
		// interface{} writes a float64 as a number, which is not a name.
		if f := v.Float(); math.IsNaN(f) || math.IsInf(f, 0) {
			return "", nonFiniteMapKey(v, f)
		}
		return "", notStringMapKey(v)
	}
	return mapKeyNameOf(v, false)
}

// mapKeyNameOf returns the name of the value of a key. byPointer is whether the value is the one of a pointer,
// whose methods on the pointer are called.
func mapKeyNameOf(v reflect.Value, byPointer bool) (string, error) {
	t := v.Type()
	if t.Kind() != reflect.Pointer {
		switch {
		case t.Implements(textAppenderType):
			return appendTextName(v.Interface().(encoding.TextAppender))
		case byPointer && reflect.PointerTo(t).Implements(textAppenderType):
			return appendTextName(v.Addr().Interface().(encoding.TextAppender))
		case t.Implements(marshalTextType):
			return marshalTextName(v.Interface().(encoding.TextMarshaler))
		case byPointer && reflect.PointerTo(t).Implements(marshalTextType):
			return marshalTextName(v.Addr().Interface().(encoding.TextMarshaler))
		}
	}
	switch t.Kind() {
	case reflect.String:
		return v.String(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return strconv.FormatUint(v.Uint(), 10), nil
	case reflect.Float32, reflect.Float64:
		f := v.Float()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return "", nonFiniteMapKey(v, f)
		}
		bits := 64
		if t.Kind() == reflect.Float32 {
			bits = 32
		}
		return string(appendFloatOfBits(nil, f, bits)), nil
	case reflect.Pointer:
		if v.IsNil() {
			return "", notStringMapKey(v)
		}
		return mapKeyNameOf(v.Elem(), true)
	case reflect.Complex64, reflect.Complex128, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return "", &errors.UnsupportedTypeError{Type: t}
	}
	return "", notStringMapKey(v)
}

// appendFloatOfBits appends the float as AppendFloat32 or AppendFloat64 does.
func appendFloatOfBits(b []byte, f float64, bits int) []byte {
	if bits == 32 {
		return AppendFloat32(nil, b, float32(f))
	}
	return AppendFloat64(nil, b, f)
}

func appendTextName(m encoding.TextAppender) (string, error) {
	b, err := m.AppendText(nil)
	if err != nil {
		return "", errors.ErrMarshaler(reflect.TypeOf(m), err, "AppendText")
	}
	return string(b), nil
}

func marshalTextName(m encoding.TextMarshaler) (string, error) {
	b, err := m.MarshalText()
	if err != nil {
		return "", errors.ErrMarshaler(reflect.TypeOf(m), err, "MarshalText")
	}
	return string(b), nil
}

// notStringMapKey is the error of a key whose value is not written as a string, as encoding/json reports it.
func notStringMapKey(v reflect.Value) error {
	return &errors.UnsupportedValueError{Value: v, Str: "object member name must be a string"}
}

// nonFiniteMapKey is the error of a key of a float which is not finite, as encoding/json reports it.
func nonFiniteMapKey(v reflect.Value, f float64) error {
	return &errors.UnsupportedValueError{Value: v, Str: strconv.FormatFloat(f, 'g', -1, 64)}
}
