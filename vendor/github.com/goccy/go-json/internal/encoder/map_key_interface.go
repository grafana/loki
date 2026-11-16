//go:build go1.27 && goexperiment.jsonv2

package encoder

import (
	"encoding"
	"encoding/base64"
	jsonv1 "encoding/json"
	jsonv2 "encoding/json/v2"
	"math"
	"reflect"
	"strconv"
	"unsafe"

	"github.com/goccy/go-json/internal/errors"
	"github.com/goccy/go-json/internal/jsonstring"
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
//   - a slice of bytes is its base64.
//   - nil, a bool, a nil pointer, a struct, an array, another slice or a map is not a name; a complex number, a
//     channel or a function is an unsupported type.
//
// The keys are sorted by their names, as the keys of the maps whose keys are not strings are: OpMapKey sorts them
// after the last one is encoded ( see MapContext.SortEncodedKeys ).

// interfaceMapKeys is whether a map of an interface type as its key is encoded.
const interfaceMapKeys = true

var (
	emptyInterfaceType  = reflect.TypeOf((*any)(nil)).Elem()
	float64Type         = reflect.TypeOf(float64(0))
	textAppenderType    = reflect.TypeOf((*encoding.TextAppender)(nil)).Elem()
	jsonMarshalerType   = reflect.TypeOf((*jsonv1.Marshaler)(nil)).Elem()
	jsonMarshalerToType = reflect.TypeOf((*jsonv2.MarshalerTo)(nil)).Elem()
)

// appendInterfaceMapKey appends the name of the key of a map of an interface type, or of a type of pointers which
// points to itself ( see pointerMapKeyCode ), as a string. p is the address of the key as the map is read into the
// context: an interface{} for a key of an interface type, whatever the interface type is ( see
// newReflectCollector ), and the pointer for a key of pointers. It is the address of the interface itself for an
// interface which the pointers of a key lead to, which OpMarshalTextPtr follows.
func appendInterfaceMapKey(ctx *RuntimeContext, code *Opcode, b []byte, p unsafe.Pointer) ([]byte, error) {
	var x any
	var keyType reflect.Type
	switch {
	case code.Type == runtime.TypePtr(emptyInterfaceType):
		// an interface{}, which is the same in place and in the context.
		x, keyType = *(*any)(p), emptyInterfaceType
	case code.Op == OpMarshalTextPtr:
		keyType = runtime.TypeOfPtr(code.Type)
		x = interfaceAt(keyType, p)
	default:
		if keyType = runtime.TypeOfPtr(code.Type); keyType.Kind() == reflect.Pointer {
			x, keyType = pointerOf(code.Type, *(*unsafe.Pointer)(p)), nil
		} else {
			x = *(*any)(p)
		}
	}
	// a string or a value of a method, as most keys of interfaces are, is named without mapKeyName: ownKeyName,
	// written here, as it is not inlined.
	name, ok := x.(string)
	var err error
	if !ok && (*emptyInterface)(unsafe.Pointer(&x)).ptr != nil {
		name, ok, err = methodKeyName(x)
	}
	if !ok {
		name, err = mapKeyName(x, keyType)
	}
	if err != nil {
		return nil, err
	}
	// appendText, written here as AppendMarshalText writes it: it is not inlined.
	b, _ = jsonstring.AppendQuoted(StringEscaper(ctx), b, name)
	return b, nil
}

// NilMapKeyError is the error of a key whose pointers, which the opcode code of what they lead to follows, end at
// nil ( see pointerMapKeyCode ), as encoding/json reports it: the nil pointer is reported as the last of them.
func NilMapKeyError(code *Opcode) error {
	t := runtime.TypeOfPtr(code.Type)
	if t.Kind() != reflect.Pointer {
		t = reflect.PointerTo(t)
	}
	return notStringMapKey(reflect.Zero(t))
}

// pointerOf returns the pointer ptr of the type typ, held by an interface{} as it is.
func pointerOf(typ, ptr unsafe.Pointer) any {
	return *(*any)(unsafe.Pointer(&emptyInterface{typ: typ, ptr: ptr}))
}

// interfaceAt returns the value of the interface of the type typ at the address p, held by an interface{}.
func interfaceAt(typ reflect.Type, p unsafe.Pointer) any {
	if typ.NumMethod() == 0 {
		return *(*any)(p)
	}
	// an interface with methods, whose words are the ones of any of them: the conversion takes the type of its
	// itab, as reflect converts it.
	return *(*interface{ M() })(p)
}

// ownKeyName returns the name which the value of an interface x has by itself, and whether it has one: a string, or
// the text of its method, which a value or a pointer to one has. A nil pointer is named by the interface type ( see
// mapKeyName ).
func ownKeyName(x any) (string, bool, error) {
	if s, ok := x.(string); ok || (*emptyInterface)(unsafe.Pointer(&x)).ptr == nil {
		return s, ok, nil
	}
	return methodKeyName(x)
}

// mapKeyName returns the name of the key x, as an interface{} holds it: its pointers and its interfaces are followed,
// one by one, to the value which names it. iface is the interface type which x is the value of, whose own name it
// doesn't have ( see ownKeyName ), or nil when x is a pointer of the key.
func mapKeyName(x any, iface reflect.Type) (string, error) {
	var cycle keyCycle
	for {
		if iface != nil {
			if x == nil {
				return "", notStringMapKey(reflect.Zero(iface))
			}
			held := (*emptyInterface)(unsafe.Pointer(&x)).ptr
			t := reflect.TypeOf(x)
			switch k := t.Kind(); {
			case k == reflect.Interface:
				// a key of an interface type with methods, which the interface{} holds as the value of its type.
				x = interfaceAt(t, held)
				if name, ok, err := ownKeyName(x); ok {
					return name, err
				}
				continue
			case k == reflect.Pointer && held == nil:
				// The method of the marshaler which the interface type is, if any, is called with the nil pointer. A
				// MarshalJSON is not called for a name.
				switch {
				case iface.Implements(jsonMarshalerToType), iface.Implements(jsonMarshalerType):
					return "", notStringMapKey(reflect.ValueOf(x))
				case iface.Implements(textAppenderType):
					return appendTextName(x.(encoding.TextAppender))
				case iface.Implements(marshalTextType):
					return marshalTextName(x.(encoding.TextMarshaler))
				}
				return "", notStringMapKey(reflect.ValueOf(x))
			case k == reflect.Float64 && iface == emptyInterfaceType && t == float64Type:
				// interface{} writes a float64 as a number, which is not a name.
				v := reflect.ValueOf(x)
				if f := v.Float(); math.IsNaN(f) || math.IsInf(f, 0) {
					return "", nonFiniteMapKey(v, f)
				}
				return "", notStringMapKey(v)
			case k != reflect.Pointer && held != nil:
				// a value which is not addressable, whose own name it doesn't have.
				return kindKeyName(reflect.ValueOf(x))
			case k != reflect.Pointer:
				// a nil value of a type which is not a pointer, as a nil map, which its methods name.
				return valueKeyName(x, false)
			}
			iface = nil
		}
		// a pointer, which is followed to the pointer, the interface or the value it points to.
		t, ptr := reflect.TypeOf(x), (*emptyInterface)(unsafe.Pointer(&x)).ptr
		if ptr == nil {
			return "", notStringMapKey(reflect.ValueOf(x))
		}
		if cycle.reachedPointer(t, ptr) {
			return "", &errors.UnsupportedValueError{Value: reflect.ValueOf(x), Str: "encountered a cycle via " + t.String()}
		}
		switch elem := t.Elem(); elem.Kind() {
		case reflect.Pointer:
			x = pointerOf(runtime.TypePtr(elem), *(*unsafe.Pointer)(ptr))
		case reflect.Interface:
			x, iface = interfaceAt(elem, ptr), elem
			if name, ok, err := ownKeyName(x); ok {
				return name, err
			}
		default:
			return valueKeyName(x, true)
		}
	}
}

// valueKeyName returns the name of the value which x holds, or which x points to when byPointer: by the methods of
// x, or by the kind of the value.
func valueKeyName(x any, byPointer bool) (string, error) {
	if name, ok, err := methodKeyName(x); ok {
		return name, err
	}
	v := reflect.ValueOf(x)
	if byPointer {
		if v.Type().Name() != "" {
			// a pointer of a type of its own, as type P *T, which has no methods: the methods of the value are the
			// ones of the pointer to it.
			return valueKeyName(v.Elem().Addr().Interface(), true)
		}
		v = v.Elem()
	}
	return kindKeyName(v)
}

// methodKeyName returns the name of the value which x holds, or which x points to, by AppendText or MarshalText of
// x, and whether x has one of them. A value of a string kind, or which x points to, is its string, without its
// methods, as encoding/json names it from Go 1.27.2.
func methodKeyName(x any) (string, bool, error) {
	switch v := reflect.ValueOf(x); {
	case v.Kind() == reflect.String:
		return v.String(), true, nil
	case v.Kind() == reflect.Pointer && !v.IsNil() && v.Elem().Kind() == reflect.String:
		return v.Elem().String(), true, nil
	}
	var b []byte
	var err error
	switch m := x.(type) {
	case encoding.TextAppender:
		if b, err = m.AppendText(nil); err != nil {
			return "", true, methodError(m, textAppenderType, err, "AppendText")
		}
	case encoding.TextMarshaler:
		if b, err = m.MarshalText(); err != nil {
			return "", true, methodError(m, marshalTextType, err, "MarshalText")
		}
	default:
		return "", false, nil
	}
	// the text is the method's own, which is written before anything else runs: it is not copied.
	return unsafe.String(unsafe.SliceData(b), len(b)), true, nil
}

// kindKeyName returns the name of the value v, whose methods don't name it, by its kind.
func kindKeyName(v reflect.Value) (string, error) {
	switch k := v.Kind(); k {
	case reflect.Slice:
		// a slice of bytes is written as its base64, as a value is, which is a name; the one of bytes which have a
		// method of marshaling is an array, and a nil one is null.
		t := v.Type()
		if elem := reflect.PointerTo(t.Elem()); t.Elem().Kind() == reflect.Uint8 && !v.IsNil() &&
			!elem.Implements(jsonMarshalerType) && !elem.Implements(jsonMarshalerToType) &&
			!elem.Implements(marshalTextType) && !elem.Implements(textAppenderType) {
			return base64.StdEncoding.EncodeToString(v.Bytes()), nil
		}
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
		if k == reflect.Float32 {
			bits = 32
		}
		return string(appendFloatOfBits(nil, f, bits)), nil
	case reflect.Complex64, reflect.Complex128, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return "", &errors.UnsupportedTypeError{Type: v.Type()}
	}
	return "", notStringMapKey(v)
}

// appendTextName returns the text which m appends, as a name. The text is the method's own, which is written before
// anything else runs: it is not copied.
func appendTextName(m encoding.TextAppender) (string, error) {
	b, err := m.AppendText(nil)
	if err != nil {
		return "", methodError(m, textAppenderType, err, "AppendText")
	}
	return unsafe.String(unsafe.SliceData(b), len(b)), nil
}

// marshalTextName returns the text which m returns, as a name, which is not copied ( see appendTextName ).
func marshalTextName(m encoding.TextMarshaler) (string, error) {
	b, err := m.MarshalText()
	if err != nil {
		return "", methodError(m, marshalTextType, err, "MarshalText")
	}
	return unsafe.String(unsafe.SliceData(b), len(b)), nil
}

// methodError is the error err of the method of iface m, named method, which is reported of the type whose method it
// is: the one of the value which m points to, if the value has it, or the one of m.
func methodError(m any, iface reflect.Type, err error, method string) error {
	t := reflect.TypeOf(m)
	if t.Kind() == reflect.Pointer && t.Elem().Implements(iface) {
		t = t.Elem()
	}
	return errors.ErrMarshaler(t, err, method)
}

// notStringMapKey is the error of a key whose value is not written as a string, as encoding/json reports it.
func notStringMapKey(v reflect.Value) error {
	return &errors.UnsupportedValueError{Value: v, Str: "object member name must be a string"}
}

// nonFiniteMapKey is the error of a key of a float which is not finite, as encoding/json reports it.
func nonFiniteMapKey(v reflect.Value, f float64) error {
	return &errors.UnsupportedValueError{Value: v, Str: strconv.FormatFloat(f, 'g', -1, 64)}
}
