package encoder

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync"
	"time"
	"unsafe"

	ierrors "github.com/goccy/go-json/internal/errors"
	"github.com/goccy/go-json/internal/jsonfields"
	"github.com/goccy/go-json/internal/jsonstring"
	"github.com/goccy/go-json/internal/runtime"
)

// The semantics of encoding/json/v2, which the compiler gives the opcodes of a type in the v2 mode: they are the
// ones of v1 but where v2 differs.

var (
	timeDurationType = reflect.TypeFor[time.Duration]()
	timeTimeType     = reflect.TypeFor[time.Time]()
)

var (
	errYearRange     = errors.New("year outside of range [0,9999]")
	errTimezoneRange = errors.New("timezone hour outside of range [0,23]")
	// errNoDuration is the error of a time.Duration, which has no representation of its own until
	// encoding/json/v2 decides one.
	errNoDuration = errors.New("no default representation")
)

// appendTime appends the time.Time at p in RFC 3339 with nanosecond precision, which can't represent a year
// before 0 or after 9999, nor a time zone of 24 hours or more.
// Its error is the one of MarshalJSON, as encoding/json reports it with ReportErrorsWithLegacySemantics, which the v2
// json package reports as the SemanticError of the time at its place otherwise.
func appendTime(_ *RuntimeContext, b []byte, p unsafe.Pointer) ([]byte, error) {
	t := (*time.Time)(p)
	if year := t.Year(); year < 0 || year > 9999 {
		return b, &ierrors.MethodError{GoType: timeTimeType, Err: errYearRange, Kind: ierrors.MethodMarshalJSON}
	}
	if _, offset := t.Zone(); offset <= -24*60*60 || offset >= 24*60*60 {
		return b, &ierrors.MethodError{GoType: timeTimeType, Err: errTimezoneRange, Kind: ierrors.MethodMarshalJSON}
	}
	b = append(b, '"')
	b = t.AppendFormat(b, time.RFC3339Nano)
	return append(b, '"'), nil
}

// v2Code returns the code of a value of typ by the semantics of encoding/json/v2, and true, if they differ from
// the ones of v1 for the type: false lets the type be compiled as v1 compiles it. A type with a method of a
// marshaler is not given to it. addressable is whether the value is addressable, which a method of a pointer
// receiver is called for only with CallMethodsWithLegacySemantics ( see v2MethodOfAt ).
func (c *Compiler) v2Code(typ reflect.Type, addressable bool) (Code, bool) {
	// the type of a default representation, which the functions and the method c.v2DefaultAfter declined, is
	// written by what is after them, once: a value of the type in it is written as any other ( see
	// appendDefault ).
	after, isDefault := noMethod, typ == c.v2DefaultType
	switch {
	case c.nextIsEmbedded:
		// the fields of an embedded struct are members of the struct which embeds it, which no function or
		// method writes.
		return nil, false
	case c.stringTag:
		// the value of the option, whose code is the one of a field of the option: a value which it doesn't apply to
		// is written without it, and so are the values in it, as the option applies to the value of its depth only
		code, applies := c.stringTagCode(typ, addressable)
		switch {
		case code != nil:
			return code, true
		case !applies:
			after := allMethods
			if isDefault {
				after = c.v2DefaultAfter
			}
			return c.appendFuncCode(typ, appendWithoutStringTag(typ, after, addressable)), true
		}
	}
	switch {
	case isDefault:
		after = c.v2DefaultAfter
		c.v2DefaultType = nil
	default:
		if code, ok := c.v2FuncsCode(typ, addressable); ok {
			return code, true
		}
	}
	switch {
	case typ == timeDurationType && c.v1Options&modeFormatDurationAsNano == 0:
		// a time.Duration has no representation, but its nanoseconds as an int64 with FormatDurationAsNano, which
		// its kind gives
		return c.appendFuncCode(typ, appendError(typ, errNoDuration)), true
	case typ == timeTimeType:
		return c.appendFuncCode(typ, appendTime), true
	}
	if code, ok := c.v2MethodCode(typ, after, addressable); ok {
		return code, true
	}
	switch typ.Kind() {
	case reflect.Interface:
		if c.v1Options&modeCallMethodsWithLegacySemantics == 0 {
			break
		}
		if m := v2MethodOfInterface(typ); m != noMethod {
			return c.appendFuncCode(typ, appendV1Interface(typ, m)), true
		}
	case reflect.Struct:
		if fields := v2FieldsOf(typ); fields.Err != nil && c.v1Options&modeReportErrorsWithLegacySemantics == 0 {
			// with ReportErrorsWithLegacySemantics, the members which jsonfields found are written, as encoding/json
			// writes them
			return c.appendFuncCode(typ, appendError(fields.Err.GoType, fields.Err.Err)), true
		} else if fields.FormatErr != nil {
			return c.appendFuncCode(typ, appendError(fields.FormatErr.GoType, fields.FormatErr.Err)), true
		}
	case reflect.Complex64, reflect.Complex128, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		// a value of a type which has no representation fails when it is marshaled, not when its type is
		// compiled: an empty map of it is {}.
		return c.appendFuncCode(typ, appendError(typ, nil)), true
	case reflect.Slice:
		if elem := typ.Elem(); elem.Kind() == reflect.Uint8 && elem.PkgPath() != "" {
			if !v2BytesAsArray(typ, c.v1Options&modeFormatByteArrayAsArray != 0,
				c.v1Options&modeFormatBytesWithLegacySemantics != 0) {
				// the base64 of its bytes, as of a []byte, with FormatBytesWithLegacySemantics
				code, _ := c.bytesCode(typ, false)
				return code, true
			}
			// a slice of a named byte type is a list of its values, of their own representation: only []byte is
			// base64.
			code, err := c.sliceCode(typ)
			if err != nil {
				return nil, false
			}
			return code, true
		}
	case reflect.Array:
		if typ.Elem().Kind() == reflect.Uint8 && !v2BytesAsArray(typ, c.v1Options&modeFormatByteArrayAsArray != 0,
			c.v1Options&modeFormatBytesWithLegacySemantics != 0) {
			n := typ.Len()
			return c.appendFuncCode(typ, func(ctx *RuntimeContext, b []byte, p unsafe.Pointer) ([]byte, error) {
				return AppendByteSlice(ctx, b, unsafe.Slice((*byte)(p), n)), nil
			}), true
		}
	case reflect.Ptr:
		// with CallMethodsWithLegacySemantics, the opcodes of its type are the ones of a value given by itself,
		// which is not addressable: a value which a method writes is written by the code of the value of the
		// pointer, addressable, which is the code of the method.
		if kind := typ.Elem().Kind(); (kind == reflect.Map || kind == reflect.Slice) &&
			(c.v1Options&modeCallMethodsWithLegacySemantics == 0 || !c.v2HasMethod(typ.Elem())) {
			// A nil map or slice is {} or [], and a nil pointer is null: the opcodes of a map or a slice which a
			// pointer is followed to can't tell the one from the other, so the value is encoded by the opcodes
			// of its type, as a value of interface{} is, after the pointer.
			return &PtrCode{typ: typ, value: &InterfaceCode{typ: typ.Elem(), static: true}, ptrNum: 1}, true
		}
	}
	return nil, false
}

// v2BytesAsArray reports whether typ, a slice or an array of a byte kind, is written as a JSON array of its elements
// rather than as the base64 string of its bytes, by the options of encoding/json which say it: a [N]byte with
// FormatByteArrayAsArray, and a slice or an array of a named byte type, but with FormatBytesWithLegacySemantics for
// one of a type which has no method of marshaling.
func v2BytesAsArray(typ reflect.Type, byteArrayAsArray, formatBytesWithLegacySemantics bool) bool {
	elem := typ.Elem()
	switch {
	case typ.Kind() == reflect.Array && byteArrayAsArray:
		return true
	case elem.PkgPath() == "":
		return false
	}
	return !formatBytesWithLegacySemantics || v2MethodOf(elem, noMethod) != noMethod
}

// appendFuncCode returns the code which writes a value of typ by fn, which is given the address of the value, as
// a method on the pointer is.
func (c *Compiler) appendFuncCode(typ reflect.Type, fn AppendFunc) *MarshalJSONCode {
	return &MarshalJSONCode{typ: typ, appendValue: fn, isAddrForMarshaler: true}
}

// hasNoRepresentation reports whether a value of the kind has no JSON representation: a complex number, a channel, a
// function or an unsafe.Pointer.
func hasNoRepresentation(kind reflect.Kind) bool {
	switch kind {
	case reflect.Complex64, reflect.Complex128, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return true
	}
	return false
}

// appendError returns the function which fails for a value of typ with err.
func appendError(typ reflect.Type, err error) AppendFunc {
	return func(_ *RuntimeContext, b []byte, _ unsafe.Pointer) ([]byte, error) {
		return b, &ierrors.SemanticError{GoType: typ, Err: err}
	}
}

// v2Fields are the JSON object members of the struct types, which v2FieldsOf found.
var v2Fields sync.Map // map[reflect.Type]*jsonfields.Fields

// v2FieldsOf returns the JSON object members of the struct type t.
func v2FieldsOf(t reflect.Type) *jsonfields.Fields {
	if fields, ok := v2Fields.Load(t); ok {
		return fields.(*jsonfields.Fields)
	}
	fields, _ := v2Fields.LoadOrStore(t, jsonfields.Of(t))
	return fields.(*jsonfields.Fields)
}

// v2Object is the JSON object of a struct of the v2 semantics, while the struct and the structs embedded in it are
// compiled.
type v2Object struct {
	// members are the members of the object, by the key of their indexes ( see indexKey ).
	members map[string]*jsonfields.Field
	// embeds are the embedded structs which a member is in, by the key of their indexes.
	embeds map[string]bool
	// path is the index of the struct being compiled in the struct of the object.
	path []int
}

// indexKey is the key of the index of a field by reflect.Type.FieldByIndex.
func indexKey(index []int) string {
	return fmt.Sprint(index)
}

func newV2Object(fields *jsonfields.Fields) *v2Object {
	obj := &v2Object{members: map[string]*jsonfields.Field{}, embeds: map[string]bool{}}
	for i := range fields.List {
		f := &fields.List[i]
		obj.members[indexKey(f.Index)] = f
		for n := 1; n < len(f.Index); n++ {
			obj.embeds[indexKey(f.Index[:n])] = true
		}
	}
	return obj
}

// v2StructTags returns the tags of the fields of the struct type typ which are written to the JSON object of the
// v2 semantics, in their order: the members, and the embedded structs which members are in, whose indexes are
// in embeds. An embedded struct is a part of the object being compiled, at v2EmbedPath; another struct is the
// object of its own. The returned function ends the compile of the struct.
func (c *Compiler) v2StructTags(typ reflect.Type, embedded bool) (runtime.StructTags, map[*runtime.StructTag][]int, func()) {
	saved := c.v2Object
	var obj *v2Object
	if embedded && saved != nil {
		obj = &v2Object{members: saved.members, embeds: saved.embeds, path: c.v2EmbedPath}
	} else {
		obj = newV2Object(v2FieldsOf(typ))
	}
	c.v2Object = obj
	var tags runtime.StructTags
	embeds := map[*runtime.StructTag][]int{}
	for i := range typ.NumField() {
		path := append(slices.Clip(obj.path), i)
		key := indexKey(path)
		sf := typ.Field(i)
		if m, ok := obj.members[key]; ok {
			tags = append(tags, &runtime.StructTag{
				Key:         m.Name,
				QuotedKey:   m.QuotedName,
				IsTaggedKey: m.HasName,
				IsOmitEmpty: m.OmitEmpty,
				IsOmitZero:  m.OmitZero || c.omitZeroStructFields,
				IsString:    m.String,
				Field:       sf,
			})
		} else if obj.embeds[key] {
			tag := &runtime.StructTag{Key: sf.Name, Field: sf}
			tags = append(tags, tag)
			embeds[tag] = path
		}
	}
	return tags, embeds, func() { c.v2Object = saved }
}

// v2HiddenNames returns the names of the members of typ, a recursive struct embedded at path in the object being
// compiled, which the object doesn't write from it: its code is compiled when the recursive codes are linked, as
// the object of its own ( see StructCode.hiddenNames ).
func (c *Compiler) v2HiddenNames(typ reflect.Type, path []int) []string {
	written := map[string]bool{}
	for _, m := range c.v2Object.members {
		if len(m.Index) > len(path) && slices.Equal(m.Index[:len(path)], path) {
			written[m.Name] = true
		}
	}
	var hidden []string
	for _, m := range v2FieldsOf(typ).List {
		if !written[m.Name] {
			hidden = append(hidden, m.Name)
		}
	}
	return hidden
}

var errInvalidStringTag = errors.New("invalid use of `string` tag option")

// v2FieldValueCode returns the code of the value of a field of the v2 semantics, addressable if the struct is, and
// true, if the options of its tag make it differ from the code of its type. The options of the field are changed for
// its opcodes: the `string` option is kept for a number, which the opcodes of its type write in a string, and dropped
// for any other value, which the code of the option writes ( see stringTagCode ), or the code of its type if the
// option doesn't apply to it.
func (c *Compiler) v2FieldValueCode(field *StructFieldCode, addressable bool) (Code, bool) {
	tag := *field.tag
	field.tag = &tag
	// with OmitEmptyWithLegacySemantics, the value is omitted if it is false, 0, nil or of no length, as v1 omits it,
	// which the opcodes of omitempty decide by the kind of the field ( see emptyKindOf ), before it is written.
	if tag.IsOmitEmpty && c.v1Options&modeOmitEmptyWithLegacySemantics == 0 {
		// the value is omitted if it would be null, "", {} or []: some kinds are known to be empty or not by the
		// value ( the check of omitempty of v1 ), the others by what they write ( unwriteEmpty ).
		switch kind := field.typ.Kind(); {
		case kind == reflect.Ptr && !c.v2HasMethod(field.typ) && c.v2NeverEmpty(field.typ.Elem()):
			// a nil pointer is null, and the value it points to is never empty.
		case kind == reflect.Ptr && !c.v2HasMethod(field.typ) && !c.v2HasMethod(field.typ.Elem()) &&
			field.typ.Elem().Kind() == reflect.String && !tag.IsString:
			// a nil pointer is null, and the string it points to is empty only if it is "".
			field.omitEmptyString = true
		case (kind == reflect.Ptr || kind == reflect.Interface) && len(c.funcs.funcsOf(field.typ)) == 0:
			// a nil value is null, unless a function writes it, as a function of the pointer to it does: with
			// functions, it is omitted by what is written, as a value of a method is.
			field.unwriteEmpty = true
		case field.typ == rawMessageType && v2MethodOf(field.typ, noMethod) == methodMarshalJSON &&
			len(c.funcs.funcsOf(field.typ)) == 0:
			// a nil json.RawMessage is null, which its method writes without a call ( see v2MethodCode ): it is
			// omitted by the check of nil, the others by what they write.
			field.emptyNil = true
			field.unwriteEmpty = true
		case c.v2HasMethod(field.typ):
			tag.IsOmitEmpty = false
			field.unwriteEmpty = true
		case c.v2NeverEmpty(field.typ):
			tag.IsOmitEmpty = false
		case kind == reflect.String || kind == reflect.Map || kind == reflect.Slice || kind == reflect.Array:
			// empty if its length is 0.
		default:
			tag.IsOmitEmpty = false
			field.unwriteEmpty = true
		}
	}
	// the check of nil writes null instead of calling a marshaler, which only a nil pointer is.
	field.isNilCheck = tag.IsOmitEmpty || field.typ.Kind() == reflect.Ptr
	if !tag.IsString {
		return nil, false
	}
	code, applies := c.stringTagCode(field.typ, addressable)
	if applies && code == nil {
		return nil, false
	}
	tag.IsString = false
	return code, code != nil
}

// stringTagCode returns the code of a value of typ of the `string` option, addressable or not, as encoding/json/v2
// writes it: nil for a number, after the pointers to it, whose nil is null, which is written in a string, and for a
// value which a method or a function writes in the compile mode of the option, whose options they are given ( see
// appendStringTag ); the error of the option for a value of another kind. It reports false if the option doesn't
// apply, which the value is written without.
//
// With StringifyWithLegacySemantics, the option applies to a bool, whose JSON value is written in a string, and to a
// string, whose JSON string is, through one pointer only: a pointer to a pointer is the error of the option.
//
// With ReportErrorsWithLegacySemantics, a value of another kind is not an error: the option doesn't apply to it, as
// encoding/json writes it, but to the value which an interface value holds, without StringifyWithLegacySemantics.
func (c *Compiler) stringTagCode(typ reflect.Type, addressable bool) (Code, bool) {
	elem := typ
	ptrNum := 0
	// the walk ends where it comes back to a pointer which it passed, in pointers to pointers without an end, as type
	// P *P, which hold null or themselves
	var passed []reflect.Type
	for elem.Kind() == reflect.Ptr && !c.v2HasMethod(elem) && !slices.Contains(passed, elem) {
		passed = append(passed, elem)
		elem = elem.Elem()
		ptrNum++
	}
	stringify := c.v1Options&modeStringifyWithLegacySemantics != 0
	reportErrors := c.v1Options&modeReportErrorsWithLegacySemantics != 0
	// the value which a pointer points to is addressable
	addressable = addressable || ptrNum > 0
	// with StringifyWithLegacySemantics, a pointer to a pointer, as a pointer of a cycle of pointers is, which no
	// function of the type of the field writes, whatever functions its pointers have, as encoding/json/v2 reads the
	// type of the field
	ptrToPtr := typ.Kind() == reflect.Ptr && typ.Elem().Kind() == reflect.Ptr && !c.v2HasMethod(typ)
	var code Code
	switch {
	case stringify && ptrToPtr && reportErrors:
		return nil, false
	case stringify && ptrToPtr:
		return c.appendFuncCode(typ, appendError(typ, errInvalidStringTag)), true
	case c.v2HasMethodAt(elem, addressable) && elem != timeTimeType,
		reportErrors && !stringify && elem.Kind() == reflect.Interface:
		if c.stringTag {
			return nil, true
		}
		code = c.appendFuncCode(elem, appendStringTag(elem, addressable))
	case elem.Kind() == reflect.Ptr:
		// a pointer of a cycle of pointers, which the walk came back to: the option doesn't apply to it, as
		// encoding/json/v2 writes null at the end of the pointers
		return nil, false
	case elem == timeDurationType && c.v1Options&modeFormatDurationAsNano == 0:
		code = c.appendFuncCode(elem, appendError(elem, errNoDuration))
	case isNumberKind(elem.Kind()) || elem == jsonNumberType:
		return nil, true
	case stringify && elem.Kind() == reflect.Bool:
		code = c.appendFuncCode(elem, appendStringifiedBool)
	case stringify && elem.Kind() == reflect.String:
		code = c.appendFuncCode(elem, appendStringifiedString(elem))
	case hasNoRepresentation(elem.Kind()):
		// the error of the type, which has no representation whatever its options are
		code = c.appendFuncCode(elem, appendError(elem, nil))
	case reportErrors:
		return nil, false
	default:
		code = c.appendFuncCode(elem, appendError(elem, errInvalidStringTag))
	}
	if ptrNum > 0 {
		code = &PtrCode{typ: typ, value: code, ptrNum: uint8(ptrNum)}
	}
	return code, true
}

// appendStringifiedBool appends the bool at p in a JSON string, as the `string` option writes it with
// StringifyWithLegacySemantics.
func appendStringifiedBool(_ *RuntimeContext, b []byte, p unsafe.Pointer) ([]byte, error) {
	if *(*bool)(p) {
		return append(b, `"true"`...), nil
	}
	return append(b, `"false"`...), nil
}

// appendStringifiedString returns the function which appends the string at p, of typ, as a JSON string in a JSON
// string, as the `string` option writes it with StringifyWithLegacySemantics: the string is escaped by the options,
// and its invalid UTF-8 is the error of a value of typ, unless allowed; the JSON string is quoted where JSON needs
// it only.
func appendStringifiedString(typ reflect.Type) AppendFunc {
	return func(ctx *RuntimeContext, b []byte, p unsafe.Pointer) ([]byte, error) {
		quoted, ok := jsonstring.AppendQuoted(textEscaper(ctx), ctx.MarshalBuf[:0], *(*string)(p))
		if !ok {
			var err error
			if quoted, err = InvalidUTF8(ctx, nil, quoted); err != nil {
				return b, &ierrors.SemanticError{GoType: typ, Err: err}
			}
		}
		ctx.MarshalBuf = quoted[:0]
		out, _ := jsonstring.AppendQuoted(jsonstring.EscaperOf(uint(TextEscapeOption)), b, *(*string)(unsafe.Pointer(&quoted)))
		return out, nil
	}
}

// appendStringTag returns the function which appends a value of typ, addressable or not, which a method or a function
// writes, or which an interface value of typ holds, in the compile mode of the `string` option, whose options a
// method or a function is given, as the value written after them is ( see stringTagCode ).
func appendStringTag(typ reflect.Type, addressable bool) AppendFunc {
	return func(ctx *RuntimeContext, b []byte, p unsafe.Pointer) ([]byte, error) {
		flags := ctx.Option.Flag
		ctx.Option.Flag |= StringTagOption
		out, err := appendDefault(ctx, b, typ, p, allMethods, addressable)
		ctx.Option.Flag = flags
		return out, err
	}
}

// appendWithoutStringTag returns the function which appends a value of typ, addressable or not, after the method
// after ( see appendDefault ), in the compile mode of the `string` option, which doesn't apply to it, without the
// option: the values in an object or an array are written as without it, as encoding/json writes them with
// ReportErrorsWithLegacySemantics.
func appendWithoutStringTag(typ reflect.Type, after int, addressable bool) AppendFunc {
	return func(ctx *RuntimeContext, b []byte, p unsafe.Pointer) ([]byte, error) {
		flags := ctx.Option.Flag
		ctx.Option.Flag &^= StringTagOption
		out, err := appendDefault(ctx, b, typ, p, after, addressable)
		ctx.Option.Flag = flags
		return out, err
	}
}

// v2NeverEmpty reports whether a value of typ is known to be written as none of the empty values of omitempty:
// a boolean, a number, or an object with a member which is always written. The value of a type which a method or
// a function writes may be any.
func (c *Compiler) v2NeverEmpty(typ reflect.Type) bool {
	if c.v2HasMethod(typ) {
		// MarshalJSON of time.Time, or of a type which has it by embedding a time.Time, writes a time.
		return typ != c.v2DefaultType && len(c.funcs.funcsOf(typ)) == 0 &&
			v2MethodOf(typ, noMethod) == methodMarshalJSON && addrJSONAppender(typ) != nil
	}
	switch kind := typ.Kind(); {
	case kind == reflect.Bool || isNumberKind(kind), typ == jsonNumberType:
		// a json.Number is a number, 0 if it is "".
		return true
	case kind == reflect.Struct && !c.omitZeroStructFields:
		fields := v2FieldsOf(typ)
		if fields.Err != nil {
			return false
		}
		for _, f := range fields.List {
			// a member of an embedded struct is not written if a pointer to the struct is nil.
			if !f.OmitEmpty && !f.OmitZero && len(f.Index) == 1 {
				return true
			}
		}
	}
	return false
}

// isNumberKind reports whether a value of the kind is a JSON number.
func isNumberKind(kind reflect.Kind) bool {
	switch kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return true
	}
	return false
}
