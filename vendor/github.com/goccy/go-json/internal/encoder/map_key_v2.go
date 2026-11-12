package encoder

import (
	"bytes"
	"encoding"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"unsafe"

	"github.com/goccy/go-json/internal/errors"
	"github.com/goccy/go-json/internal/jsonstring"
)

// The keys of a map for the semantics of encoding/json/v2: a key is encoded as a value is, in the place of a name,
// where a number is a string, and a key which is not a string is an error. A key of a string, an integer or a
// float, which the VM writes, is a different name for every key of the map. The other keys are named here, and
// two keys of a map may have the same name: the names of a map are recorded to report it.

// v2MapKeyCode returns the code of the key of a map of the type typ.
func (c *Compiler) v2MapKeyCode(typ reflect.Type) (Code, error) {
	funcs := c.funcs.funcsOf(typ)
	// a time.Duration and a json.Number are named by appendKeyNameAfter, wherever they are.
	if len(funcs) == 0 && v2MethodOf(typ, noMethod) == noMethod && typ != timeDurationType && typ != jsonNumberType {
		switch typ.Kind() {
		case reflect.String:
			return c.stringCode(typ, false)
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			return c.mapKeyCode(typ)
		case reflect.Float32:
			return &FloatCode{typ: typ, bitSize: 32, isString: true}, nil
		case reflect.Float64:
			return &FloatCode{typ: typ, bitSize: 64, isString: true}, nil
		}
	}
	if typ.Kind() == reflect.Interface && typ.NumMethod() > 0 {
		return c.appendFuncCode(typ, func(ctx *RuntimeContext, b []byte, p unsafe.Pointer) ([]byte, error) {
			return appendMapKeyName(ctx, b, typ, methodInterfaceOf(typ, p), funcs)
		}), nil
	}
	return c.appendFuncCode(typ, func(ctx *RuntimeContext, b []byte, p unsafe.Pointer) ([]byte, error) {
		return appendMapKeyName(ctx, b, typ, p, funcs)
	}), nil
}

// methodInterfaceOf returns the address of the key at p of a map whose keys are of typ, an interface type with
// methods, as a value of typ: p has the words of the interface{} which the key is read into ( see
// newInterfaceKeyCollector ), not the ones of typ, whose first word is an itab.
func methodInterfaceOf(typ reflect.Type, p unsafe.Pointer) unsafe.Pointer {
	v := reflect.New(typ)
	if key := *(*any)(p); key != nil {
		v.Elem().Set(reflect.ValueOf(key))
	}
	return v.UnsafePointer()
}

// appendMapKeyName appends the name of the key at p, of typ, by the functions of marshaling, its method or its
// kind, and records it in the context of the map, failing if the map has the name already. The names of a map
// whose entries are sorted are checked as the entries are written in the order of the names ( see SameMapName ).
func appendMapKeyName(ctx *RuntimeContext, b []byte, typ reflect.Type, p unsafe.Pointer, funcs []*MarshalFunc) ([]byte, error) {
	start := len(b)
	out, err := b, ErrUseDefault
	ctx.KeyName = true // for a method or a function which writes to an encoder, at the place of a name
	if len(funcs) > 0 {
		out, err = appendByFuncs(ctx, b, p, typ, funcs)
	}
	byFunc := err != ErrUseDefault
	if err == ErrUseDefault {
		// a key, which is not addressable
		out, err = appendKeyName(ctx, b, reflect.NewAt(typ, p).Elem(), false)
	}
	ctx.KeyName = false
	if err == nil && out[start] != '"' {
		err = &errors.TextError{Err: errors.ErrNonStringName, GoType: typ}
	}
	if err != nil {
		if ctx.Option.Flag&UnorderedMapOption == 0 {
			// a name of a sorted map, which is encoded after the names before it, which are not entries yet ( see
			// MapContext.SortEncodedKeys ): the error is at the place of the first name, where encoding/json/v2
			// writes each name.
			err = &errors.OutputError{Out: b[:ctx.SortedKeysStart()], Err: err}
		}
		return b, err
	}
	if ctx.Option.Flag&(RawRewriteOption|AllowDuplicateNamesOption) == RawRewriteOption && bytes.IndexByte(out[start:], '\\') >= 0 {
		// a name of a raw value which the options of raw values may keep with its escapes, as PreserveRawStrings does,
		// which another name may be the same as by another escape: the output is checked by its names ( see
		// RescanNames ).
		ctx.RescanNames = true
	}
	if ctx.Option.Flag&(AllowDuplicateNamesOption|UnorderedMapOption) != UnorderedMapOption {
		return out, nil
	}
	m := ctx.mapContexts[ctx.mapDepth-1]
	if !m.namesKept {
		if m.names == nil {
			m.names = map[string]struct{}{}
		}
		clear(m.names)
		m.namesKept = true
	}
	name := string(out[start:])
	if _, ok := m.names[name]; ok {
		unquoted := jsonstring.AppendUnescaped(nil, []byte(name[1:len(name)-1]))
		err := &errors.TextError{Err: errors.ErrDuplicateName, Name: string(unquoted)}
		if byFunc || keyMethodOf(ctx, typ, noMethod, false) != noMethod {
			// the name was written by a function or a method, which the error is reported for.
			err.GoType = typ
		}
		return b, err
	}
	m.names[name] = struct{}{}
	return out, nil
}

// appendKeyName appends the name of the value v, which reflect can take the address of, as a JSON string: it is
// addressable if it is the value which a pointer key points to ( see keyMethodOf ).
func appendKeyName(ctx *RuntimeContext, b []byte, v reflect.Value, addressable bool) ([]byte, error) {
	return appendKeyNameAfter(ctx, b, v, noMethod, addressable)
}

// keyMethodOf returns the method of marshaling which names a key of the type t, addressable or not, after the method
// after, as v2MethodOf does. With CallMethodsWithLegacySemantics, a key is named as encoding/json names it: a string
// by itself, and a key of another kind by AppendText or MarshalText only, the methods after methodMarshalJSON, of a
// value receiver unless the key is addressable.
func keyMethodOf(ctx *RuntimeContext, t reflect.Type, after int, addressable bool) int {
	if ctx.Option.Flag&CallMethodsWithLegacySemanticsOption == 0 {
		return v2MethodOf(t, after)
	}
	if t.Kind() == reflect.String {
		return noMethod
	}
	return methodOfAt(t, max(after, methodMarshalJSON), addressable)
}

// appendKeyNameAfter appends the name of the value v, addressable or not ( see appendKeyName ), by its method after
// the method after or by its kind.
func appendKeyNameAfter(ctx *RuntimeContext, b []byte, v reflect.Value, after int, addressable bool) ([]byte, error) {
	t := v.Type()
	if t == timeTimeType {
		// written as a value of time.Time is, as encoding/json/v2 writes a time, not by its methods
		return appendTime(ctx, b, v.Addr().UnsafePointer())
	}
	switch m := keyMethodOf(ctx, t, after, addressable); m {
	case methodMarshalJSONTo:
		// the encoder, at the place of a name, takes a string only.
		out, err := V2Hooks.MarshalTo(ctx, b, t, v.Addr().Interface())
		if err == ErrUseDefault {
			return appendKeyNameAfter(ctx, b, v, methodMarshalJSONTo, addressable)
		}
		return out, err
	case methodMarshalJSON:
		raw, err := v.Addr().Interface().(json.Marshaler).MarshalJSON()
		if err != nil {
			return b, &errors.MethodError{GoType: t, Err: unsupportedError(err, errors.MethodMarshalJSON), Kind: errors.MethodMarshalJSON}
		}
		out, err := AppendRaw(ctx, b, raw)
		if err != nil {
			return b, &errors.MethodError{GoType: t, Err: err, Kind: errors.MethodMarshalJSON}
		}
		if out[len(b)] != '"' {
			return b, &errors.TextError{Err: errors.ErrNonStringName, GoType: t}
		}
		return out, nil
	case methodAppendText, methodMarshalText:
		return appendTextOf(ctx, b, t, m, v.Addr().Interface())
	}
	switch {
	case t == timeDurationType && ctx.Option.Flag&FormatDurationAsNanoOption == 0:
		// the nanoseconds of the int64 with FormatDurationAsNano, which its kind writes
		return b, &errors.SemanticError{GoType: t, Err: errNoDuration}
	case t == jsonNumberType:
		out, err := AppendNumberString(ctx, append(b, '"'), json.Number(v.String()))
		if err != nil {
			return b, err
		}
		return append(out, '"'), nil
	}
	switch t.Kind() {
	case reflect.String:
		return appendName(ctx, b, v.String())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		b = append(b, '"')
		return append(appendIntValue(b, v.Int()), '"'), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		b = append(b, '"')
		return append(appendUintValue(b, v.Uint()), '"'), nil
	case reflect.Float32, reflect.Float64:
		f := v.Float()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			if ctx.Option.Flag&ReportErrorsWithLegacySemanticsOption != 0 {
				// the error of encoding/json, with the float
				return b, &errors.UnsupportedValueError{Value: v, Str: strconv.FormatFloat(f, 'g', -1, 64)}
			}
			return b, &errors.SemanticError{GoType: t, Err: errUnsupportedValue(f)}
		}
		bits := 64
		if t.Kind() == reflect.Float32 {
			bits = 32
		}
		b = append(b, '"')
		return append(appendFloatOfBits(b, f, bits), '"'), nil
	case reflect.Pointer, reflect.Interface:
		if ctx.Option.Flag&CallMethodsWithLegacySemanticsOption != 0 {
			switch {
			case v.Kind() == reflect.Pointer && v.IsNil() && (t.Implements(marshalTextType) || t.Implements(appendTextType)):
				// a nil pointer of MarshalText or AppendText, which encoding/json names "" with
				// CallMethodsWithLegacySemantics
				return append(b, `""`...), nil
			case v.Kind() == reflect.Interface && !v.IsNil() && v.Elem().Kind() == reflect.Pointer && v.Elem().IsNil():
				// a nil pointer held by an interface value of MarshalText or AppendText, which encoding/json names by
				// the method of the interface type, called for the nil pointer, as it writes such a value
				if out, ok, err := appendNilTextKey(ctx, b, v); ok {
					return out, err
				}
			}
		}
		// the pointers and the interfaces are followed here, one by one, up to a value which is named otherwise: by
		// its functions, its method or its kind.
		var cycle keyCycle
		for {
			if v.IsNil() {
				return b, &errors.TextError{Err: errors.ErrNonStringName, GoType: v.Type()}
			}
			elem := v.Elem()
			if v.Kind() == reflect.Interface {
				// the dynamic value, in a variable of its own, which is addressable.
				copied := reflect.New(elem.Type()).Elem()
				copied.Set(elem)
				elem = copied
			}
			// named by the functions of its own type first, as a key of that type is.
			if funcs := ctx.Option.Funcs.funcsOf(elem.Type()); len(funcs) > 0 {
				if out, err := appendByFuncs(ctx, b, elem.Addr().UnsafePointer(), elem.Type(), funcs); err != ErrUseDefault {
					return out, err
				}
			}
			if k := elem.Kind(); k != reflect.Pointer && k != reflect.Interface || v2MethodOf(elem.Type(), noMethod) != noMethod {
				// the value which a pointer points to is addressable, not the copy of the one an interface holds
				return appendKeyName(ctx, b, elem, v.Kind() == reflect.Pointer)
			}
			if cycle.reached(elem) {
				return b, &errors.SemanticError{GoType: t, Err: errors.ErrCycle}
			}
			v = elem
		}
	case reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 &&
			!v2BytesAsArray(t, ctx.Option.Flag&FormatByteArrayAsArrayOption != 0, ctx.Option.Flag&FormatBytesWithLegacySemanticsOption != 0) {
			src := unsafe.Slice((*byte)(v.Addr().UnsafePointer()), t.Len())
			b = append(b, '"')
			return append(base64.StdEncoding.AppendEncode(b, src), '"'), nil
		}
	case reflect.Complex64, reflect.Complex128, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return b, &errors.SemanticError{GoType: t}
	}
	return b, &errors.TextError{Err: errors.ErrNonStringName, GoType: t}
}

// appendNilTextKey appends the name of the interface value v, which holds a nil pointer, by the method of the
// interface type of v, called for the nil pointer, and reports true, if it is MarshalText or AppendText ( see
// v2MethodOfInterface ) of the pointer: a method of the value it points to can't be called for it.
func appendNilTextKey(ctx *RuntimeContext, b []byte, v reflect.Value) ([]byte, bool, error) {
	held, recv := v.Elem().Type(), v.Interface()
	m := v2MethodOfInterface(v.Type())
	if m != methodAppendText && m != methodMarshalText || held.Elem().Implements(marshalerTypeOf(m)) {
		return b, false, nil
	}
	out, err := appendTextOf(ctx, b, held, m, recv)
	return out, true, err
}

// appendTextOf appends the text of recv by its method m, AppendText or MarshalText, as a JSON string, with the
// errors of a value of typ.
func appendTextOf(ctx *RuntimeContext, b []byte, typ reflect.Type, m int, recv any) ([]byte, error) {
	if m == methodAppendText {
		return appendMethodText(ctx, b, typ, errors.MethodAppendText, nil, func(_ unsafe.Pointer, dst []byte) ([]byte, error) {
			return recv.(textAppender).AppendText(dst)
		})
	}
	return appendMethodText(ctx, b, typ, errors.MethodMarshalText, nil, func(_ unsafe.Pointer, dst []byte) ([]byte, error) {
		text, err := recv.(encoding.TextMarshaler).MarshalText()
		return append(dst, text...), err
	})
}

// appendName appends the name as a JSON string, which must be valid UTF-8 unless it is allowed.
func appendName(ctx *RuntimeContext, b []byte, name string) ([]byte, error) {
	if out, ok := jsonstring.AppendQuoted(textEscaper(ctx), b, name); ok {
		return out, nil
	} else if out, err := InvalidUTF8(ctx, b, out); err != nil {
		return b, err
	} else {
		return out, nil
	}
}

func appendIntValue(b []byte, v int64) []byte { return strconv.AppendInt(b, v, 10) }

func appendUintValue(b []byte, v uint64) []byte { return strconv.AppendUint(b, v, 10) }

func errUnsupportedValue(f float64) error { return fmt.Errorf("unsupported value: %v", f) }
