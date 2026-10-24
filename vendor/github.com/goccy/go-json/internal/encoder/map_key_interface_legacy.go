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

// appendInterfaceMapKey appends the name of the key of a map of an interface type which has MarshalText, as a
// string: the text of its dynamic value, as encoding/json makes it. p is the address of the key as the map is
// read into the context: an interface{}, whatever the interface type of the key is ( see newReflectCollector ).
func appendInterfaceMapKey(ctx *RuntimeContext, code *Opcode, b []byte, p unsafe.Pointer) ([]byte, error) {
	v := reflect.NewAt(emptyInterfaceType, p).Elem()
	for v.Kind() == reflect.Interface && !v.IsNil() {
		v = v.Elem()
	}
	m, ok := v.Interface().(encoding.TextMarshaler)
	if !ok {
		// a nil key, which has no text.
		return nil, &errors.UnsupportedValueError{Value: v, Str: "nil key of " + runtime.TypeOfPtr(code.Type).String()}
	}
	text, err := m.MarshalText()
	if err != nil {
		return nil, errors.ErrMarshaler(v.Type(), err, "MarshalText")
	}
	return appendText(ctx, b, *(*string)(unsafe.Pointer(&text))), nil
}
