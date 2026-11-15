//go:build !go1.27 || !goexperiment.jsonv2

package encoder

import (
	"encoding"
	"reflect"
	"unsafe"

	"github.com/goccy/go-json/internal/errors"
	"github.com/goccy/go-json/internal/runtime"
)

// interfaceMapKeys is whether every map of an interface type as its key is encoded: encoding/json which is not
// built on encoding/json/v2 refuses the type of such a map, but the one of an interface type which has
// MarshalText, whose keys are the texts of their values ( see map_key_interface.go ).
const interfaceMapKeys = false

var emptyInterfaceType = reflect.TypeOf((*any)(nil)).Elem()

// appendInterfaceMapKey appends the name of the key of a map of an interface type which has MarshalText, or of a
// type of pointers which points to itself ( see pointerMapKeyCode ), as a string: the text of the value, as
// encoding/json makes it. p is the address of the key as the map is read into the context: an interface{} for a key
// of an interface type, whatever the interface type is ( see newReflectCollector ), and the pointer for a key of
// pointers. It is the address of the interface itself for an interface which the pointers of a key lead to, which
// OpMarshalTextPtr follows.
func appendInterfaceMapKey(ctx *RuntimeContext, code *Opcode, b []byte, p unsafe.Pointer) ([]byte, error) {
	keyType := runtime.TypeOfPtr(code.Type)
	v := reflect.NewAt(emptyInterfaceType, p).Elem()
	switch {
	case code.Op == OpMarshalTextPtr:
		v = reflect.NewAt(keyType, p).Elem()
	case keyType.Kind() == reflect.Pointer:
		// the pointers are followed, one by one, to nil or back to one of them: a nil one has no text, as
		// encoding/json of Go 1.27 names it.
		v = reflect.NewAt(keyType, p).Elem()
		var cycle keyCycle
		for v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return nil, &errors.UnsupportedValueError{Value: v, Str: "nil key of " + keyType.String()}
			}
			if cycle.reached(v) {
				return nil, &errors.UnsupportedValueError{Value: v, Str: "encountered a cycle via " + v.Type().String()}
			}
			v = v.Elem()
		}
	}
	for v.Kind() == reflect.Interface && !v.IsNil() {
		v = v.Elem()
	}
	m, ok := v.Interface().(encoding.TextMarshaler)
	if !ok {
		// a nil key, which has no text.
		return nil, &errors.UnsupportedValueError{Value: v, Str: "nil key of " + keyType.String()}
	}
	text, err := m.MarshalText()
	if err != nil {
		return nil, errors.ErrMarshaler(v.Type(), err, "MarshalText")
	}
	return appendText(ctx, b, *(*string)(unsafe.Pointer(&text))), nil
}

// NilMapKeyError is the error of a key whose pointers, which the opcode code of what they lead to follows, end at
// nil ( see pointerMapKeyCode ): the nil pointer is reported as the last of them.
func NilMapKeyError(code *Opcode) error {
	t := runtime.TypeOfPtr(code.Type)
	if t.Kind() != reflect.Pointer {
		t = reflect.PointerTo(t)
	}
	return &errors.UnsupportedValueError{Value: reflect.Zero(t), Str: "nil key of " + t.String()}
}
