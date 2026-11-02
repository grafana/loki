package decoder

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	stderrors "errors"
	"fmt"
	"math/bits"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
	"unsafe"

	"github.com/goccy/go-json/internal/errors"
	"github.com/goccy/go-json/internal/jsonnum"
	"github.com/goccy/go-json/internal/runtime"
)

// The decoders of the semantics of encoding/json/v2, which the v2 json package decodes by: a value is decoded by the
// decoder of v1 of its type where v2 decodes it as v1 does, and by a decoder of its own where v2 differs. The
// decoding stops at the first error. An error of a value is an errors.UnmarshalError, which skipTypeError returns
// instead of recording it, and which the decoders here make of the type errors which the decoders of v1 record
// ( see v2Error ). Any other error, as a syntax error, the v2 json package finds again in the input, as
// encoding/json/v2 reports it: the decoders here only find the text which v2 rejects and v1 decodes, the strings of
// invalid UTF-8 and the objects which have a name twice ( see errV2Text ).

var (
	// errV2Text is the error of the text of a value which v2 rejects: a string of invalid UTF-8, or an object which
	// has a name twice.
	errV2Text = stderrors.New("json: invalid text of encoding/json/v2")
	// errNilInterface is the error of a nil value of an interface type with methods, which no value can be
	// decoded into.
	errNilInterface = stderrors.New("cannot derive concrete type for nil interface with finite type set")
	// errV2DuplicateName is the error of an object of an interface{} which has a name twice, which
	// decodeNewStringAnyMap returns with the semantics of v2: they reject it, or decode the value of the name into
	// the value of its first one ( see v2InterfaceDecoder ).
	errV2DuplicateName = stderrors.New("json: duplicate name of encoding/json/v2")
	// errNoDefaultRepresentation is the error of a time.Duration, which v2 has no representation of without the
	// `format` option.
	errNoDefaultRepresentation = stderrors.New("no default representation")
	errArrayUnderflow          = stderrors.New("too few array elements")
	errArrayOverflow           = stderrors.New("too many array elements")
)

var timeDurationType = reflect.TypeOf(time.Duration(0))

// IsArrayLengthError reports whether err is the error of the length of a JSON array decoded into a Go array, which is
// at the end of the JSON array.
func IsArrayLengthError(err error) bool {
	return err == errArrayOverflow || err == errArrayUnderflow
}

// cachedV2Decoders are the decoders of v2, without and with StringifyNumbers.
var cachedV2Decoders [2]runtime.TypeCache[Decoder]

// v1DecoderOptions are the options of encoding/json which change the decoders of a type: a decoding with one of them
// takes its decoders from v1OptionDecoders, apart from the tables shared by every goroutine, which have no room for
// them, as few programs use them, and from the recent decoders of its context, which are the ones of its options
// ( see SetV2Flags ).
const v1DecoderOptions = V2CallMethodsWithLegacySemantics | V2FormatByteArrayAsArray |
	V2FormatBytesWithLegacySemantics | V2FormatDurationAsNano | V2ReportErrorsWithLegacySemantics |
	V2StringifyWithLegacySemantics

// flagsKey is what the decoders of a type for options are cached by in a sync.Map: the type, and the options of
// v1DecoderOptions, with V2StringifyNumbers: the decoders of the options of encoding/json ( see v1OptionDecoders ) and
// the ones of the functions of unmarshaling ( see UnmarshalFuncs ).
type flagsKey struct {
	typeptr uintptr
	flags   V2Flags
}

// v1OptionDecoders are the decoders of v2 of the options of encoding/json ( see v1DecoderOptions ).
var v1OptionDecoders sync.Map // map[flagsKey]Decoder

// decoderIn returns the decoder of the pointer type typ for the options flags from m, compiling it for the functions
// funcs, or none, if the type is new.
func decoderIn(m *sync.Map, typ unsafe.Pointer, flags V2Flags, funcs *UnmarshalFuncs) (Decoder, error) {
	key := flagsKey{typeptr: uintptr(typ), flags: flags & (v1DecoderOptions | V2StringifyNumbers)}
	if v, ok := m.Load(key); ok {
		return v.(Decoder), nil
	}
	c := newCompiler(true)
	c.stringifyNumbers, c.v1Options, c.funcs = flags&V2StringifyNumbers != 0, flags&v1DecoderOptions, funcs
	dec, err := compileHead(runtime.TypeOfPtr(typ), c)
	if err != nil {
		return nil, err
	}
	v, _ := m.LoadOrStore(key, dec)
	return v.(Decoder), nil
}

// CompileToGetDecoderV2 returns the decoder of the type for the semantics of encoding/json/v2 and the options flags,
// of which StringifyNumbers and the ones of v1DecoderOptions change the decoders, compiling it if the type is new.
func CompileToGetDecoderV2(typ unsafe.Pointer, flags V2Flags) (Decoder, error) {
	if flags&v1DecoderOptions != 0 {
		return decoderIn(&v1OptionDecoders, typ, flags, nil)
	}
	stringifyNumbers := flags&V2StringifyNumbers != 0
	cache := &cachedV2Decoders[0]
	if stringifyNumbers {
		cache = &cachedV2Decoders[1]
	}
	typeptr := uintptr(typ)
	if dec := cache.Load(typeptr); dec != nil {
		return *dec, nil
	}
	c := newCompiler(true)
	c.stringifyNumbers = stringifyNumbers
	dec, err := compileHead(runtime.TypeOfPtr(typ), c)
	if err != nil {
		return nil, err
	}
	return *cache.Store(typeptr, &dec), nil
}

// The marks of the keys of the decoders of v2 among the recent decoders of a context ( see DecoderOfV2 ), in the two
// lowest bits of the address of the type, which are 0, as a type is aligned to 4 bytes at least: v2DecoderMark for a
// decoder of v2, which is not the decoder of v1 of the type, and stringifyDecoderMark for one with StringifyNumbers
// too, which is another again.
const (
	v2DecoderMark        = 1
	stringifyDecoderMark = 2
)

// DecoderOfV2 returns the decoder of the semantics of encoding/json/v2 of the type, for the options and the
// functions of unmarshaling of ctx. The decoder of a call without functions is remembered with the decoders of the
// context as v1's DecoderOf remembers them, under the address of the type with the marks of the decoders of v2. A
// decoder in neither entry of its set is the one of the shared table, which takes the first entry, and the one decoded
// before it the second.
func (ctx *RuntimeContext) DecoderOfV2(typ unsafe.Pointer) (Decoder, error) {
	if funcs := ctx.v2Funcs(); funcs != nil {
		return funcs.decoderOf(typ, ctx.Option.V2)
	}
	flags := ctx.Option.V2
	if ctx.recentDecoders == nil {
		return CompileToGetDecoderV2(typ, flags)
	}
	key := uintptr(typ) | v2DecoderMark
	if flags&V2StringifyNumbers != 0 {
		key |= stringifyDecoderMark
	}
	set := &ctx.recentDecoders[(uint64(key)*runtime.TypeHashMultiplier)>>recentDecoderHashShift]
	if set[0].typeptr == key {
		return set[0].dec, nil
	}
	if set[1].typeptr == key {
		return set[1].dec, nil
	}
	dec, err := CompileToGetDecoderV2(typ, flags)
	if err != nil {
		return nil, err
	}
	set.keep(key, dec)
	return dec, nil
}

// SetV2Flags sets the options of v2 of ctx for a decoding, which every setting of them before a decoding is made by, as
// the others clear them after one: the recent decoders are emptied if they were compiled for other options of
// encoding/json which change the decoders, which their keys have no room for ( see recentV1Options ), once for the call
// rather than in every DecoderOfV2, by a call of its own, so that SetV2Flags is inlined into the callers.
func SetV2Flags(ctx *RuntimeContext, flags V2Flags) {
	ctx.Option.V2 = flags
	if v1Options := flags & v1DecoderOptions; v1Options != ctx.recentV1Options {
		emptyRecentDecoders(ctx, v1Options)
	}
}

// emptyRecentDecoders empties the recent decoders of ctx, if it has them, which are compiled for the options
// v1Options of encoding/json from now on.
func emptyRecentDecoders(ctx *RuntimeContext, v1Options V2Flags) {
	if ctx.recentDecoders != nil {
		*ctx.recentDecoders = [recentDecoderSets]recentDecoderSet{}
	}
	ctx.recentV1Options = v1Options
}

// compileV2 returns the decoder of a value of typ by the semantics of v2.
func (c *compiler) compileV2(typ reflect.Type, structName, fieldName string) (Decoder, error) {
	if dec, exists := c.decoders[uintptr(runtime.TypePtr(typ))]; exists {
		return dec, nil
	}
	// the first method which decodes a value: with CallMethodsWithLegacySemantics, a key of a map is decoded by
	// UnmarshalText or its kind, as encoding/json doesn't call UnmarshalJSONFrom and UnmarshalJSON for a key
	first := methodUnmarshalJSONFrom
	if c.mapKeys && c.v1Options&V2CallMethodsWithLegacySemantics != 0 {
		first = methodUnmarshalText
	}
	if funcs := c.funcs.funcsOf(typ); len(funcs) > 0 {
		d := &v2FuncDecoder{typ: typ, funcs: funcs}
		return d, c.compileNextV2(d, &d.next, typ, first, structName, fieldName)
	}
	return c.compileDefaultV2(typ, first, structName, fieldName)
}

// compileNextV2 compiles into next the decoder of typ which dec leaves a value to, by the methods of typ from first
// on, or by its kind ( see compileDefaultV2 ): dec stays the decoder of typ in c.decoders, so that the values of typ
// within the value are decoded by it too. Only a type which may have values of itself is put there: a decoder of a
// kind of v1 looks itself up there ( see compileKind ).
func (c *compiler) compileNextV2(dec Decoder, next *Decoder, typ reflect.Type, first int, structName, fieldName string) error {
	key := uintptr(runtime.TypePtr(typ))
	switch typ.Kind() {
	case reflect.Struct, reflect.Map, reflect.Slice, reflect.Array, reflect.Pointer:
		if _, exists := c.decoders[key]; !exists {
			c.decoders[key] = dec
		}
	}
	d, err := c.compileDefaultV2(typ, first, structName, fieldName)
	*next = d
	return err
}

// compileDefaultV2 returns the decoder of a value of typ without the functions of unmarshaling: by the time types,
// by the first of its methods of unmarshaling from first on, or by its kind. A method which may decline a value
// leaves it to the decoder of the methods after it.
func (c *compiler) compileDefaultV2(typ reflect.Type, first int, structName, fieldName string) (Decoder, error) {
	switch {
	case typ == timeDurationType && c.v1Options&V2FormatDurationAsNano == 0:
		// a time.Duration has no representation, but its nanoseconds as an int64 with FormatDurationAsNano, which
		// its kind gives
		return &v2InvalidDecoder{typ: typ, err: errNoDefaultRepresentation}, nil
	case typ == timeType:
		if c.stringTag {
			return &v2InvalidDecoder{typ: typ, err: errInvalidStringTag}, nil
		}
		return v2TimeDecoder{}, nil
	}
	switch v2MethodOf(typ, first) {
	case methodUnmarshalJSONFrom:
		d := &v2UnmarshalFromDecoder{typ: typ}
		return d, c.compileNextV2(d, &d.next, typ, methodUnmarshalJSON, structName, fieldName)
	case methodUnmarshalJSON:
		return &v2UnmarshalJSONDecoder{typ: typ, ptrType: runtime.TypePtr(reflect.PointerTo(typ))}, nil
	case methodUnmarshalText:
		return &v2UnmarshalTextDecoder{typ: typ, ptrType: runtime.TypePtr(reflect.PointerTo(typ)), zero: newZeroer(typ)}, nil
	}
	stringify := c.stringTag && c.v1Options&V2StringifyWithLegacySemantics != 0
	if c.stringTag && typ.Kind() != reflect.Ptr && !isNumberKind(typ.Kind()) && !hasNoRepresentation(typ.Kind()) {
		if stringify && typ != jsonNumberType && (typ.Kind() == reflect.Bool || typ.Kind() == reflect.String) {
			// the option applies to a bool and a string too with StringifyWithLegacySemantics
			return &v2StringifiedDecoder{typ: typ}, nil
		}
		// the option applies to a number, through pointers
		return &v2InvalidDecoder{typ: typ, err: errInvalidStringTag}, nil
	}
	switch kind := typ.Kind(); kind {
	case reflect.Ptr:
		if stringify {
			return c.compileStringifiedPtrV2(typ, structName, fieldName)
		}
		return compilePtr(typ, structName, fieldName, c)
	case reflect.Slice:
		if IsV2Bytes(typ, c.v1Options) {
			return c.compileBytesV2(typ, structName, fieldName)
		}
		return compileSlice(typ, structName, fieldName, c)
	case reflect.Array:
		if IsV2Bytes(typ, c.v1Options) {
			return c.compileBytesV2(typ, structName, fieldName)
		}
		return c.compileArrayV2(typ, structName, fieldName)
	case reflect.Interface:
		return c.newV2InterfaceDecoder(typ), nil
	case reflect.String:
		d := newStringDecoder(structName, fieldName)
		d.typ = typ
		return v2StringDecoder{d}, nil
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		if c.stringifyNumbers && kind != reflect.Bool {
			return newV2StringNumberDecoder(typ), nil
		}
		// the decoder of v1, which decodes null as the zero value and returns a type error by the semantics of v2
		// ( see numberError )
		return compileKind(typ, structName, fieldName, c)
	case reflect.Struct:
		return c.compileStructV2(typ)
	case reflect.Map:
		return c.compileMapV2(typ, structName, fieldName)
	}
	// a complex number, a channel, a function or an unsafe.Pointer, which has no representation, whatever the options
	// are ( see hasNoRepresentation )
	return &v2InvalidDecoder{typ: typ}, nil
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

// IsV2Bytes reports whether typ, a slice or an array, is of bytes for the options flags, whose value is a base64
// string: a []byte or a [N]byte, but a [N]byte with FormatByteArrayAsArray, and a slice or an array of a named byte
// type with FormatBytesWithLegacySemantics, which is otherwise a JSON array of its values.
func IsV2Bytes(typ reflect.Type, flags V2Flags) bool {
	elem := typ.Elem()
	if elem.Kind() != reflect.Uint8 || typ.Kind() == reflect.Array && flags&V2FormatByteArrayAsArray != 0 {
		return false
	}
	return elem.PkgPath() == "" || flags&V2FormatBytesWithLegacySemantics != 0
}

// compileBytesV2 returns the decoder of typ, a slice or an array of bytes ( see IsV2Bytes ), which a JSON array is
// decoded into element by element with FormatBytesWithLegacySemantics, as into any other slice or array.
func (c *compiler) compileBytesV2(typ reflect.Type, structName, fieldName string) (Decoder, error) {
	d := newV2BytesDecoder(typ)
	if c.v1Options&V2FormatBytesWithLegacySemantics == 0 {
		return d, nil
	}
	var elements Decoder
	var err error
	if typ.Kind() == reflect.Slice {
		elements, err = compileSlice(typ, structName, fieldName, c)
	} else {
		elements, err = c.compileArrayV2(typ, structName, fieldName)
	}
	if err != nil {
		return nil, err
	}
	return &v2FormatBytesWithLegacySemanticsDecoder{bytes: d, elements: elements}, nil
}

// V2TypeError returns the type error which a decoder of v1 recorded, as the error of v2 ( see v2Error ), or nil if
// none did.
func (ctx *RuntimeContext) V2TypeError() error {
	if ctx.typeError == nil {
		return nil
	}
	return ctx.v2Error()
}

// v2Error returns the type error which a decoder of v1 recorded, as the error of v2, which stops the decoding: the
// error of a number which is not a value of its type reports the number. The type error is cleared.
func (ctx *RuntimeContext) v2Error() error {
	p := ctx.typeError
	ctx.typeError = nil
	if p.plain != nil {
		return p.plain
	}
	e := &errors.UnmarshalError{GoType: p.typ, Start: p.start, End: p.end, Err: p.err}
	if p.literal {
		e.HasValue = true
		e.Err = numberCause(ctx.Buf[p.start:p.end], p.typ)
	}
	return e
}

// numberCause returns the cause of the error of the number num, which is not a value of typ, as strconv returns it:
// strconv.ErrSyntax for a number which is not an integer, or negative, of an integer type, and strconv.ErrRange for a
// number out of the range of the type.
func numberCause(num []byte, typ reflect.Type) error {
	switch typ.Kind() {
	case reflect.Float32, reflect.Float64:
		return strconv.ErrRange
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if num[0] == '-' {
			return strconv.ErrSyntax
		}
	}
	if bytes.ContainsAny(num, ".eE") {
		return strconv.ErrSyntax
	}
	return strconv.ErrRange
}

// validV2Text reports whether the strings of text, a JSON text valid by the grammar, have the text which v2 allows:
// valid UTF-8, and no escape of a surrogate which is not a pair.
func validV2Text(text []byte) bool {
	return utf8.Valid(text) && !hasLoneSurrogate(text)
}

// hasLoneSurrogate reports whether the escapes of b, a part of a JSON text valid by the grammar, have a surrogate
// which is not a pair: a high surrogate which no low surrogate follows, or a low surrogate which no high surrogate
// precedes.
func hasLoneSurrogate(b []byte) bool {
	for {
		// a backslash is in a string only, where it starts an escape; an escape of \u has six bytes.
		i := bytes.IndexByte(b, '\\')
		if i < 0 {
			return false
		}
		if b[i+1] != 'u' {
			b = b[i+2:]
			continue
		}
		code := hexValue(b[i+2 : i+6])
		b = b[i+6:]
		switch {
		case code >= 0xdc00 && code < 0xe000:
			return true
		case code >= 0xd800 && code < 0xdc00:
			if len(b) < 6 || b[0] != '\\' || b[1] != 'u' {
				return true
			}
			if low := hexValue(b[2:6]); low < 0xdc00 || low >= 0xe000 {
				return true
			}
			b = b[6:]
		}
	}
}

// hexValue returns the number of the four hexadecimal digits of b.
func hexValue(b []byte) rune {
	var r rune
	for _, c := range b {
		switch {
		case c <= '9':
			c -= '0'
		case c <= 'F':
			c -= 'A' - 10
		default:
			c -= 'a' - 10
		}
		r = r<<4 | rune(c)
	}
	return r
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

// v2StringDecoder is the decoder of a string of v2: null is "", and a string must be valid UTF-8, without an escape
// of a surrogate which is not a pair, unless V2AllowInvalidUTF8 ( see decodeStringV2 ).
type v2StringDecoder struct{ *stringDecoder }

func (d v2StringDecoder) Decode(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	var (
		s    string
		next int64
		err  error
	)
	buf := ctx.Buf
	start := skipWhiteSpace(buf, cursor)
	if buf[start] == '"' {
		s, next, err = decodeStringV2(d.stringDecoder, ctx, start+1)
	} else {
		var ok bool
		if s, next, ok, err = d.decodeStringValue(ctx, start); err == nil && ok {
			err = replacedRuneError(ctx, s, start, next)
		} else if err == nil && buf[start] == 'n' && ctx.Option.V2&V2MergeWithLegacySemantics != 0 {
			// null, which leaves the string as it is with MergeWithLegacySemantics
			return next, nil
		}
	}
	if err != nil {
		return 0, err
	}
	**(**string)(unsafe.Pointer(&p)) = s
	return next, nil
}

// decodeStringV2 decodes the string whose bytes start at start, after its quote, as decodeStringField decodes it, for
// a string of v2. A string of bytes which are not control characters and of escapes which are not \u, whose words are
// read as decodeStringField reads them, is decoded here if its bytes are valid UTF-8, which the loops here look
// for: it has no text which v2 rejects. Any other string is decoded by decodeStringField, whose errors are the ones
// of the string, and which replaces invalid UTF-8 and an escape of a surrogate which is not a pair by U+FFFD, which
// is an error unless the input has it ( see replacedRuneError ). So is a string which goes on past the words read
// where the CPU has a SIMD scan.
func decodeStringV2(d *stringDecoder, ctx *RuntimeContext, start int64) (string, int64, error) {
	buf := ctx.Buf
	b := (*sliceHeader)(unsafe.Pointer(&buf)).data
	buflen := int64(len(buf))
	cursor := start
	wordsEnd := buflen
	if hasStringSIMD {
		wordsEnd = min(buflen, start+64)
	}
	// The words of a plain ASCII string, which most are, are looked at by this loop, which keeps nothing else, as
	// the first loop of decodeStringField.
	for cursor+8 <= wordsEnd {
		w := load64(buf, cursor)
		if stop := keyEndBytes(w) | w&msb; stop != 0 {
			cursor += int64(bits.TrailingZeros64(stop) / 8)
			if char(b, cursor) == '"' {
				return ctx.makeString(buf[start:cursor]), cursor + 1, nil
			}
			break
		}
		cursor += 8
	}
	firstEscape := int64(-1)
	nonASCII := false
	// ascii are the top bits which stop the words at a byte which is not ASCII, until the first one is met.
	ascii := uint64(msb)
	for {
		c := char(b, cursor)
		switch {
		case c == '"':
			literal, next := buf[start:cursor], cursor+1
			if nonASCII && !utf8.Valid(literal) {
				return decodeStringV1(d, ctx, start)
			}
			if firstEscape < 0 {
				return ctx.makeString(literal), next, nil
			}
			// the literal is valid UTF-8, and has no \u escape, so its decoding is valid UTF-8 too: it is not
			// validated again
			return ctx.unescapeString(literal, stringInfo{firstEscape: int(firstEscape)}), next, nil
		case c == '\\':
			switch char(b, cursor+1) {
			case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
				if firstEscape < 0 {
					firstEscape = cursor - start
				}
				cursor += 2
			default:
				// an escape \u, or an invalid one
				return decodeStringV1(d, ctx, start)
			}
		case c >= 0x80:
			nonASCII, ascii = true, 0
			cursor++
		case c >= 0x20 && wordsEnd == buflen:
			// the words stopped where they may not be read, near the end of the buffer, not at a byte to look at
			cursor++
		default:
			// a control character, the end of the input, or the end of the words where the CPU has a SIMD scan
			return decodeStringV1(d, ctx, start)
		}
		for cursor+8 <= wordsEnd {
			w := load64(buf, cursor)
			if stop := keyEndBytes(w) | w&ascii; stop != 0 {
				cursor += int64(bits.TrailingZeros64(stop) / 8)
				break
			}
			cursor += 8
		}
	}
}

// decodeStringV1 decodes the string whose bytes start at start, after its quote, by decodeStringField, which replaces
// invalid UTF-8 and an escape of a surrogate which is not a pair by U+FFFD, which is an error ( see
// replacedRuneError ).
func decodeStringV1(d *stringDecoder, ctx *RuntimeContext, start int64) (string, int64, error) {
	s, next, _, err := d.decodeStringField(ctx, start)
	if err != nil {
		return "", 0, err
	}
	if err := replacedRuneError(ctx, s, start-1, next); err != nil {
		return "", 0, err
	}
	return s, next, nil
}

// replacedRuneError returns errV2Text if the string s, which the decoders of v1 decoded from the string from start up
// to next in the input, has a U+FFFD which they made of invalid UTF-8 or of an escape of a surrogate which is not a
// pair, unless V2AllowInvalidUTF8, and nil otherwise. A U+FFFD of the input is valid.
func replacedRuneError(ctx *RuntimeContext, s string, start, next int64) error {
	if ctx.Option.V2&V2AllowInvalidUTF8 == 0 && strings.IndexByte(s, 0xef) >= 0 && !validV2Text(ctx.Buf[start:next]) {
		return errV2Text
	}
	return nil
}

// v2StringNumberDecoder is the decoder of a number of v2 with StringifyNumbers: the number is in a JSON string, by
// the grammar of the JSON numbers, and null is its zero value.
type v2StringNumberDecoder struct {
	typ  reflect.Type
	kind reflect.Kind
	str  *stringDecoder
}

func newV2StringNumberDecoder(typ reflect.Type) *v2StringNumberDecoder {
	return &v2StringNumberDecoder{typ: typ, kind: typ.Kind(), str: newStringDecoder("", "")}
}

func (d *v2StringNumberDecoder) Decode(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	buf := ctx.Buf
	cursor = skipWhiteSpace(buf, cursor)
	switch buf[cursor] {
	case 'n':
		if err := validateNull(buf, cursor); err != nil {
			return 0, err
		}
		if ctx.Option.V2&V2MergeWithLegacySemantics == 0 {
			clear(unsafe.Slice((*byte)(p), d.typ.Size()))
		}
		return cursor + 4, nil
	case '"':
	default:
		return ctx.skipTypeError(cursor, depth, d.typ)
	}
	num, next, err := d.str.decodeByte(buf, cursor)
	if err != nil {
		return 0, err
	}
	// with StringifyWithLegacySemantics, the number is read by the syntax of Go numbers, of which the JSON numbers are
	// a part, and a quoted null is null, as v1 reads them: the option is looked at for a number of no JSON number only
	if !jsonnum.IsValid(num) && ctx.Option.V2&V2StringifyWithLegacySemantics == 0 {
		return 0, &errors.UnmarshalError{GoType: d.typ, Start: cursor, End: next, HasValue: true, Err: strconv.ErrSyntax}
	}
	if err := setNumber(p, d.kind, num); err != nil {
		if ctx.Option.V2&V2StringifyWithLegacySemantics != 0 && string(num) == "null" {
			if ctx.Option.V2&V2MergeWithLegacySemantics == 0 {
				clear(unsafe.Slice((*byte)(p), d.typ.Size()))
			}
			return next, nil
		}
		return 0, &errors.UnmarshalError{GoType: d.typ, Start: cursor, End: next, HasValue: true, Err: err}
	}
	return next, nil
}

// v2StringifiedDecoder is the decoder of a bool or a string of a field of the `string` option with
// StringifyWithLegacySemantics: the JSON string "true" or "false" of a bool, and the JSON string of a JSON string of a
// string. null and the JSON string "null" are the zero value.
type v2StringifiedDecoder struct {
	typ reflect.Type
}

func (d *v2StringifiedDecoder) Decode(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	buf := ctx.Buf
	cursor = skipWhiteSpace(buf, cursor)
	switch buf[cursor] {
	case 'n':
		if err := validateNull(buf, cursor); err != nil {
			return 0, err
		}
		d.zero(ctx, p)
		return cursor + 4, nil
	case '"':
	default:
		return ctx.skipTypeError(cursor, depth, d.typ)
	}
	var s string
	end, err := v2StringDecoder{skipStringDecoder}.Decode(ctx, cursor, depth, unsafe.Pointer(&s))
	if err != nil {
		return 0, err
	}
	switch {
	case s == "null":
		d.zero(ctx, p)
	case d.typ.Kind() == reflect.Bool && (s == "true" || s == "false"):
		*(*bool)(p) = s == "true"
	case d.typ.Kind() == reflect.Bool:
		return 0, &errors.UnmarshalError{GoType: d.typ, Start: cursor, End: end, HasValue: true, Err: strconv.ErrSyntax}
	default:
		text, err := V2Hooks.AppendUnquote(nil, s)
		if err != nil {
			return 0, &errors.UnmarshalError{GoType: d.typ, Start: cursor, Err: err}
		}
		*(*string)(p) = string(text)
	}
	return end, nil
}

// zero sets the bool or the string at p to its zero value for null, but with MergeWithLegacySemantics.
func (d *v2StringifiedDecoder) zero(ctx *RuntimeContext, p unsafe.Pointer) {
	if ctx.Option.V2&V2MergeWithLegacySemantics != 0 {
		return
	}
	if d.typ.Kind() == reflect.Bool {
		*(*bool)(p) = false
	} else {
		*(*string)(p) = ""
	}
}

func (d *v2StringifiedDecoder) DecodePath(ctx *RuntimeContext, cursor, depth int64) ([][]byte, int64, error) {
	return nil, 0, fmt.Errorf("json: v2 decoder does not support decode path")
}

// compileStringifiedPtrV2 returns the decoder of a pointer of a field of the `string` option with
// StringifyWithLegacySemantics ( see v2StringifiedPtrDecoder ).
func (c *compiler) compileStringifiedPtrV2(typ reflect.Type, structName, fieldName string) (Decoder, error) {
	d := &v2StringifiedPtrDecoder{}
	if typ.Elem().Kind() == reflect.Ptr {
		// the option applies to one pointer only: a pointer to a pointer is its error, before the value is read
		d.ptr = &v2InvalidDecoder{typ: typ, err: errInvalidStringTag}
		return d, nil
	}
	var err error
	d.ptr, err = compilePtr(typ, structName, fieldName, c)
	return d, err
}

// v2StringifiedPtrDecoder is the decoder of a pointer of a field of the `string` option with
// StringifyWithLegacySemantics: null is nil, and any other value is decoded by ptr, the decoder of the pointer, after
// which the JSON string "null" sets the pointer nil, as encoding/json/v2 sets it after the value it points to is
// decoded. A pointer to a pointer is the error of the option, of ptr.
type v2StringifiedPtrDecoder struct {
	ptr Decoder
}

func (d *v2StringifiedPtrDecoder) Decode(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	buf := ctx.Buf
	cursor = skipWhiteSpace(buf, cursor)
	if buf[cursor] == 'n' {
		if err := validateNull(buf, cursor); err != nil {
			return 0, err
		}
		*(*unsafe.Pointer)(p) = nil
		return cursor + 4, nil
	}
	end, err := d.ptr.Decode(ctx, cursor, depth, p)
	if err != nil {
		return 0, err
	}
	// the JSON string "null", also of escapes, unlike encoding/json/v2, which tells it by the value as the input has
	// it only
	if raw := buf[cursor:end]; string(raw) == `"null"` || raw[0] == '"' && bytes.IndexByte(raw, '\\') >= 0 && d.isNull(ctx, cursor, depth) {
		*(*unsafe.Pointer)(p) = nil
	}
	return end, nil
}

// isNull reports whether the JSON string at cursor, of escapes, is "null".
func (d *v2StringifiedPtrDecoder) isNull(ctx *RuntimeContext, cursor, depth int64) bool {
	var s string
	_, err := v2StringDecoder{skipStringDecoder}.Decode(ctx, cursor, depth, unsafe.Pointer(&s))
	return err == nil && s == "null"
}

func (d *v2StringifiedPtrDecoder) DecodePath(ctx *RuntimeContext, cursor, depth int64) ([][]byte, int64, error) {
	return nil, 0, fmt.Errorf("json: v2 decoder does not support decode path")
}

func (d *v2StringNumberDecoder) DecodePath(ctx *RuntimeContext, cursor, depth int64) ([][]byte, int64, error) {
	return nil, 0, fmt.Errorf("json: v2 decoder does not support decode path")
}

// setNumber sets the number at p, of the numeric kind, to num, a JSON number, and returns the cause of the error of
// a number which is not a value of the kind, as strconv returns it, which leaves the value as it is.
func setNumber(p unsafe.Pointer, kind reflect.Kind, num []byte) error {
	s := unsafe.String(unsafe.SliceData(num), len(num))
	var err error
	switch kind {
	case reflect.Float32, reflect.Float64:
		bits := 64
		if kind == reflect.Float32 {
			bits = 32
		}
		var f float64
		if f, err = strconv.ParseFloat(s, bits); err == nil {
			if kind == reflect.Float32 {
				*(*float32)(p) = float32(f)
			} else {
				*(*float64)(p) = f
			}
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		var i int64
		if i, err = strconv.ParseInt(s, 10, intBits(kind)); err == nil {
			switch kind {
			case reflect.Int:
				*(*int)(p) = int(i)
			case reflect.Int8:
				*(*int8)(p) = int8(i)
			case reflect.Int16:
				*(*int16)(p) = int16(i)
			case reflect.Int32:
				*(*int32)(p) = int32(i)
			default:
				*(*int64)(p) = i
			}
		}
	default:
		var u uint64
		if u, err = strconv.ParseUint(s, 10, intBits(kind)); err == nil {
			switch kind {
			case reflect.Uint:
				*(*uint)(p) = uint(u)
			case reflect.Uint8:
				*(*uint8)(p) = uint8(u)
			case reflect.Uint16:
				*(*uint16)(p) = uint16(u)
			case reflect.Uint32:
				*(*uint32)(p) = uint32(u)
			case reflect.Uintptr:
				*(*uintptr)(p) = uintptr(u)
			default:
				*(*uint64)(p) = u
			}
		}
	}
	if err != nil {
		return stderrors.Unwrap(err)
	}
	return nil
}

// intBits returns the bits of an integer of the kind.
func intBits(kind reflect.Kind) int {
	switch kind {
	case reflect.Int8, reflect.Uint8:
		return 8
	case reflect.Int16, reflect.Uint16:
		return 16
	case reflect.Int32, reflect.Uint32:
		return 32
	case reflect.Int, reflect.Uint, reflect.Uintptr:
		return strconv.IntSize
	}
	return 64
}

// v2InvalidDecoder is the decoder of a type which no JSON value is decoded into by v2, null included: its error,
// whose cause is err, is the one of the type, before the value is read. err is nil for a type which has no
// representation ( see hasNoRepresentation ), which fails after its value with ReportErrorsWithLegacySemantics, as
// encoding/json reads it, but for null, which is decoded into nothing.
type v2InvalidDecoder struct {
	typ reflect.Type
	err error
}

func (d *v2InvalidDecoder) Decode(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	cursor = skipWhiteSpace(ctx.Buf, cursor)
	if d.err == nil && ctx.Option.V2&V2ReportErrorsWithLegacySemantics != 0 {
		if ctx.Buf[cursor] == 'n' {
			return cursor + 4, validateNull(ctx.Buf, cursor)
		}
		return 0, &errors.UnmarshalError{GoType: d.typ, Start: cursor}
	}
	return 0, &errors.UnmarshalError{GoType: d.typ, Start: cursor, NoKind: true, Err: d.err}
}

func (d *v2InvalidDecoder) DecodePath(ctx *RuntimeContext, cursor, depth int64) ([][]byte, int64, error) {
	return nil, 0, fmt.Errorf("json: v2 decoder does not support decode path")
}

// zeroer sets a value of a type to its zero value.
type zeroer struct {
	ptrType unsafe.Pointer
	size    uintptr
	// pointers is whether the type has pointers, whose zero value is set with the write barriers of the GC.
	pointers bool
}

func newZeroer(typ reflect.Type) zeroer {
	return zeroer{ptrType: ptrTypeOf(typ), size: typ.Size(), pointers: hasPointers(typ)}
}

func (z zeroer) zero(p unsafe.Pointer) {
	if z.pointers {
		valueAt(z.ptrType, p).SetZero()
		return
	}
	clear(unsafe.Slice((*byte)(p), z.size))
}

// hasPointers reports whether a value of typ has a pointer.
func hasPointers(typ reflect.Type) bool {
	switch typ.Kind() {
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128:
		return false
	case reflect.Array:
		return typ.Len() > 0 && hasPointers(typ.Elem())
	case reflect.Struct:
		for i := range typ.NumField() {
			if hasPointers(typ.Field(i).Type) {
				return true
			}
		}
		return false
	}
	return true
}

// mergesInto reports whether a value of typ is decoded by v2 into the value it has, which a value of v2 must not be
// when it is an element of a slice: a pointer, which is decoded into what it points to, a value of an interface,
// a slice, a map, a struct, or an array of them.
func mergesInto(typ reflect.Type) bool {
	switch typ.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Slice, reflect.Map, reflect.Struct:
		return true
	case reflect.Array:
		return mergesInto(typ.Elem())
	}
	return false
}

// v2SliceDecoder is the decoder of a slice of v2: the elements are decoded as sliceDecoder decodes them, into the
// array of the slice, whose elements are set to their zero values before, as v2 decodes an element into its zero
// value. An element which the decoding overwrites anyway, as a number or a string is, is not set.
type v2SliceDecoder struct {
	*sliceDecoder
	zero bool
}

func newV2SliceDecoder(d *sliceDecoder) *v2SliceDecoder {
	return &v2SliceDecoder{sliceDecoder: d, zero: mergesInto(d.elemType)}
}

func (d *v2SliceDecoder) Decode(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	cursor = skipWhiteSpace(ctx.Buf, cursor)
	if d.zero && ctx.Buf[cursor] == '[' && (*sliceHeader)(p).cap > 0 && ctx.Option.V2&V2MergeWithLegacySemantics == 0 {
		s := valueAt(d.slicePtrType, p)
		s.Slice(0, s.Cap()).Clear()
	}
	return d.sliceDecoder.Decode(ctx, cursor, depth, p)
}

// compileArrayV2 returns the decoder of the array type typ, which is not of bytes.
func (c *compiler) compileArrayV2(typ reflect.Type, structName, fieldName string) (Decoder, error) {
	d := &v2ArrayDecoder{typ: typ, array: newZeroer(typ), elem: newZeroer(typ.Elem()), alen: typ.Len()}
	dec, err := compileElem(typ, d, structName, fieldName, c)
	if err != nil {
		return nil, err
	}
	d.valueDecoder = c.goOnDecoder(dec)
	return d, nil
}

// v2ArrayDecoder is the decoder of an array of v2: an element is decoded into its zero value, and the JSON array
// must have as many elements as the array. null is the zero value.
type v2ArrayDecoder struct {
	typ          reflect.Type
	valueDecoder Decoder
	// array and elem set the array and an element to their zero values.
	array zeroer
	elem  zeroer
	alen  int
}

func (d *v2ArrayDecoder) Decode(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	buf := ctx.Buf
	cursor = skipWhiteSpace(buf, cursor)
	switch buf[cursor] {
	case 'n':
		if err := validateNull(buf, cursor); err != nil {
			return 0, err
		}
		if ctx.Option.V2&V2MergeWithLegacySemantics == 0 {
			d.array.zero(p)
		}
		return cursor + 4, nil
	case '[':
	default:
		return ctx.skipTypeError(cursor, depth, d.typ)
	}
	depth++
	if depth > maxDecodeNestingDepth {
		return 0, errors.ErrExceededMaxDepth(buf[cursor], cursor)
	}
	cursor = skipWhiteSpace(buf, cursor+1)
	idx := 0
	var lengthErr error
	// an element is decoded into its zero value, but into the element with MergeWithLegacySemantics
	zeroElems := ctx.Option.V2&V2MergeWithLegacySemantics == 0
	if buf[cursor] != ']' {
		for {
			if idx < d.alen {
				ep := unsafe.Add(p, uintptr(idx)*d.elem.size)
				if zeroElems {
					d.elem.zero(ep)
				}
				c, err := d.valueDecoder.Decode(ctx, cursor, depth, ep)
				if err != nil {
					return 0, err
				}
				cursor = c
			} else {
				// an element past the length is a value of the input, checked as the options of the call say, as
				// the array fails at its end without UnmarshalArrayFromAnyLength
				c, err := ctx.skipValueV2(cursor, depth)
				if err != nil {
					return 0, err
				}
				cursor = c
				lengthErr = errArrayOverflow
			}
			idx++
			cursor = skipWhiteSpace(buf, cursor)
			if buf[cursor] == ']' {
				break
			}
			if buf[cursor] != ',' {
				return 0, errors.ErrInvalidCharacter(buf[cursor], "array", cursor)
			}
			cursor = skipWhiteSpace(buf, cursor+1)
		}
	}
	for ; idx < d.alen; idx++ {
		d.elem.zero(unsafe.Add(p, uintptr(idx)*d.elem.size))
		lengthErr = errArrayUnderflow
	}
	if lengthErr != nil && ctx.Option.V2&V2UnmarshalArrayFromAnyLength == 0 {
		// the error of the length is at the end of the array, which tells it
		return 0, &errors.UnmarshalError{GoType: d.typ, Start: cursor, Err: lengthErr}
	}
	return cursor + 1, nil
}

func (d *v2ArrayDecoder) DecodePath(ctx *RuntimeContext, cursor, depth int64) ([][]byte, int64, error) {
	return nil, 0, fmt.Errorf("json: v2 decoder does not support decode path")
}

// v2BytesDecoder is the decoder of a []byte or a [N]byte of v2, whose bytes are a base64 string, by the standard
// encoding with padding: the array of a []byte is reused, and the bytes of a [N]byte must be N. null is the zero
// value.
type v2BytesDecoder struct {
	typ reflect.Type
	// alen is the length of a [N]byte, or -1 for a []byte.
	alen int
	str  *stringDecoder
}

func newV2BytesDecoder(typ reflect.Type) *v2BytesDecoder {
	d := &v2BytesDecoder{typ: typ, alen: -1, str: newStringDecoder("", "")}
	if typ.Kind() == reflect.Array {
		d.alen = typ.Len()
	}
	return d
}

func (d *v2BytesDecoder) Decode(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	buf := ctx.Buf
	cursor = skipWhiteSpace(buf, cursor)
	start := cursor
	switch buf[cursor] {
	case 'n':
		if err := validateNull(buf, cursor); err != nil {
			return 0, err
		}
		if d.alen < 0 {
			*(*[]byte)(p) = nil
		} else if ctx.Option.V2&V2MergeWithLegacySemantics == 0 {
			clear(unsafe.Slice((*byte)(p), d.alen))
		}
		return cursor + 4, nil
	case '"':
	default:
		return ctx.skipTypeError(cursor, depth, d.typ)
	}
	text, end, err := d.str.decodeByte(buf, cursor)
	if err != nil {
		return 0, err
	}
	var dst []byte
	if d.alen < 0 {
		dst = (*(*[]byte)(p))[:0]
	} else {
		dst = unsafe.Slice((*byte)(p), d.alen)[:0]
	}
	b, err := base64.StdEncoding.AppendDecode(dst, text)
	if err == nil && len(text) != base64.StdEncoding.EncodedLen(len(b)) && ctx.Option.V2&V2ParseBytesWithLooseRFC4648 == 0 {
		// the decoding skips a line feed and a carriage return, which RFC 4648 doesn't allow, unless
		// ParseBytesWithLooseRFC4648
		i := bytes.IndexAny(text, "\r\n")
		err = fmt.Errorf("illegal character %s at offset %d", strconv.QuoteRune(rune(text[i])), i)
	}
	if err == nil && d.alen >= 0 {
		dst = dst[:d.alen]
		clear(dst[copy(dst, b):])
		if len(b) != d.alen && ctx.Option.V2&V2UnmarshalArrayFromAnyLength == 0 {
			err = fmt.Errorf("decoded length of %d mismatches array length of %d", len(b), d.alen)
		}
	}
	if err != nil {
		return 0, &errors.UnmarshalError{GoType: d.typ, Start: start, Err: err}
	}
	if d.alen < 0 {
		if b == nil {
			b = []byte{}
		}
		*(*[]byte)(p) = b
	}
	return end, nil
}

func (d *v2BytesDecoder) DecodePath(ctx *RuntimeContext, cursor, depth int64) ([][]byte, int64, error) {
	return nil, 0, fmt.Errorf("json: v2 decoder does not support decode path")
}

// v2FormatBytesWithLegacySemanticsDecoder is the decoder of a slice or an array of bytes with
// FormatBytesWithLegacySemantics: a JSON array is decoded by the decoder of its elements, and any other value as bytes.
type v2FormatBytesWithLegacySemanticsDecoder struct {
	bytes    *v2BytesDecoder
	elements Decoder
}

func (d *v2FormatBytesWithLegacySemanticsDecoder) Decode(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	if ctx.Buf[skipWhiteSpace(ctx.Buf, cursor)] == '[' {
		return d.elements.Decode(ctx, cursor, depth, p)
	}
	return d.bytes.Decode(ctx, cursor, depth, p)
}

func (d *v2FormatBytesWithLegacySemanticsDecoder) DecodePath(ctx *RuntimeContext, cursor, depth int64) ([][]byte, int64, error) {
	return nil, 0, fmt.Errorf("json: v2 decoder does not support decode path")
}

var anySliceType = reflect.TypeOf([]any(nil))

// v2InterfaceDecoder is the decoder of an interface value of v2. null sets it to nil. A nil value of an interface type
// without methods is set to the value of the JSON value, decoded as v1 decodes it, and a nil value of an interface type
// with methods can't be decoded into. A value which an interface value holds is decoded into: a copy of it is
// decoded by its own decoder, which the interface value is set to, but a map[string]interface{}, which an object is
// merged into, and a []interface{} ( see decodeMerging ).
type v2InterfaceDecoder struct {
	typ     reflect.Type
	ptrType unsafe.Pointer
	empty   bool
	fast    *interfaceDecoder
	// fromAny is whether a function of unmarshaling takes an interface{} or a type of the values of a nil
	// interface{}: a value is then decoded by the decoders of the functions, as a value of its type ( see
	// decodeByKind ), and never by decodeMerging, which calls no function.
	fromAny bool
}

// anyValueTypes are the types of the values of a nil interface{}, by the first byte of the JSON value.
var anyValueTypes = func() [256]reflect.Type {
	var types [256]reflect.Type
	types['t'], types['f'] = boolType, boolType
	types['"'] = stringType
	types['-'] = float64Type
	for c := '0'; c <= '9'; c++ {
		types[c] = float64Type
	}
	types['{'] = interfaceMapType
	types['['] = anySliceType
	return types
}()

var boolType = reflect.TypeOf(false)

func (c *compiler) newV2InterfaceDecoder(typ reflect.Type) *v2InterfaceDecoder {
	d := &v2InterfaceDecoder{
		typ: typ, ptrType: ptrTypeOf(typ), empty: typ.NumMethod() == 0, fast: newEmptyInterfaceDecoder("", ""),
	}
	for _, t := range []reflect.Type{emptyInterfaceType, boolType, stringType, float64Type, interfaceMapType, anySliceType} {
		d.fromAny = d.fromAny || len(c.funcs.funcsOf(t)) > 0
	}
	return d
}

func (d *v2InterfaceDecoder) Decode(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	buf := ctx.Buf
	cursor = skipWhiteSpace(buf, cursor)
	if (*[2]unsafe.Pointer)(p)[0] != nil && ctx.Option.V2&V2MergeWithLegacySemantics != 0 {
		if !d.holdsPointer(p) {
			*(*[2]unsafe.Pointer)(p) = [2]unsafe.Pointer{}
		} else if buf[cursor] == 'n' && runtime.TypeOfPtr(heldType(p, d.empty)).Elem().Kind() == reflect.Pointer {
			// null into a pointer to a pointer, which sets the pointer it points to nil, rather than the interface
			// value
			if err := validateNull(buf, cursor); err != nil {
				return 0, err
			}
			*(*unsafe.Pointer)(*interfaceData(p)) = nil
			return cursor + 4, nil
		}
	}
	if buf[cursor] == 'n' {
		if err := validateNull(buf, cursor); err != nil {
			return 0, err
		}
		*(*[2]unsafe.Pointer)(p) = [2]unsafe.Pointer{}
		return cursor + 4, nil
	}
	// the first word of an interface value is its type, or its itab, which is nil for a nil value
	if (*[2]unsafe.Pointer)(p)[0] != nil {
		return d.decodeHeld(ctx, cursor, depth, p)
	}
	if !d.empty {
		return 0, &errors.UnmarshalError{GoType: d.typ, Start: cursor, NoKind: true, Err: errNilInterface}
	}
	if d.fromAny {
		return d.decodeByKind(ctx, cursor, depth, p)
	}
	next, err := d.fast.decodeEmptyInterface(ctx, cursor, depth, p)
	// with ReportErrorsWithLegacySemantics, the type error may be the one of a value before, which the decoding went
	// on after, which doesn't make the value decoded again and is kept
	reportErrors := ctx.Option.V2&V2ReportErrorsWithLegacySemantics != 0
	if (err != nil || ctx.typeError != nil && !reportErrors) && ctx.Option.V2&V2AllowDuplicateNames != 0 {
		if c := buf[cursor]; c == '{' || c == '[' {
			// The value may have a name twice, whose second value is decoded into the first one, which v1 doesn't
			// do, and whose error may come before the one v1 found: the value is decoded again, by the merging of
			// v2, which meets its errors in the order of the input. The error which v1 recorded in the value, at
			// it or after it, is dropped; the one of a value before, with ReportErrorsWithLegacySemantics, is kept.
			if e := ctx.typeError; e != nil && e.start >= cursor {
				ctx.typeError = nil
			}
			*(*[2]unsafe.Pointer)(p) = [2]unsafe.Pointer{}
			return d.decodeMerging(ctx, cursor, depth, p)
		}
	}
	if ctx.typeError != nil && !reportErrors {
		// a number out of the range of a float64, which v1 records and decodes on from: it comes before any other
		// error of the value, which is the error of the decoding, but with ReportErrorsWithLegacySemantics, which goes
		// on after it as v1 does
		return 0, ctx.v2Error()
	}
	if err != nil {
		return 0, err
	}
	if ctx.Option.V2&V2AllowInvalidUTF8 == 0 && !validV2Text(buf[cursor:next]) {
		return 0, errV2Text
	}
	return next, nil
}

// decodeByKind decodes the value at cursor into the nil interface value at p as a value of the type of its kind,
// by the decoder of the functions of unmarshaling, which the interface value is set to.
func (d *v2InterfaceDecoder) decodeByKind(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	typ := anyValueTypes[ctx.Buf[cursor]]
	if typ == nil {
		return 0, syntaxErrorAt(ctx.Buf, cursor, whereValue)
	}
	v := reflect.New(typ)
	dec, err := ctx.DecoderOfV2(runtime.TypePtr(v.Type()))
	if err != nil {
		return 0, err
	}
	next, err := dec.Decode(ctx, cursor, depth, v.UnsafePointer())
	valueAt(d.ptrType, p).Set(v.Elem())
	return next, err
}

// decodeHeld decodes the value at cursor into the value which the interface value at p holds, as encoding/json/v2
// decodes it: into a copy of the value, which the interface value is set to. A pointer is the data word of the
// interface value, which it is decoded into in place of the copy: the interface value is set as the copy would set it.
func (d *v2InterfaceDecoder) decodeHeld(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	if typ := heldType(p, d.empty); runtime.TypeOfPtr(typ).Kind() == reflect.Pointer {
		if ptr := *interfaceData(p); ptr != nil && ctx.v2Funcs() == nil {
			// the value which the pointer points to, decoded through it by the decoder of its pointer type, as the
			// decoder of a pointer to the pointer decodes a pointer which is not nil: without the type of a pointer to
			// the pointer, which reflect makes by a lookup, as the program has none. The functions of unmarshaling
			// are given the pointer to the pointer too, as encoding/json/v2 gives it, by the decoder of that pointer.
			dec, err := ctx.DecoderOfV2(typ)
			if err != nil {
				return 0, err
			}
			return dec.Decode(ctx, cursor, depth, ptr)
		}
		dec, err := ctx.DecoderOfV2(ptrTypeOf(runtime.TypeOfPtr(typ)))
		if err != nil {
			return 0, err
		}
		return dec.Decode(ctx, cursor, depth, unsafe.Pointer(interfaceData(p)))
	}
	iface := valueAt(d.ptrType, p)
	held := iface.Elem()
	if !d.fromAny && (held.Type() == interfaceMapType || held.Type() == anySliceType) {
		return d.decodeMerging(ctx, cursor, depth, p)
	}
	v := reflect.New(held.Type())
	v.Elem().Set(held)
	dec, err := ctx.DecoderOfV2(runtime.TypePtr(v.Type()))
	if err != nil {
		return 0, err
	}
	next, err := dec.Decode(ctx, cursor, depth, v.UnsafePointer())
	iface.Set(v.Elem())
	return next, err
}

// holdsPointer reports whether the interface value at p, which holds a value, holds a pointer which is not nil: the
// only value which MergeWithLegacySemantics decodes into, through the pointer, while any other is dropped before the
// value is decoded.
func (d *v2InterfaceDecoder) holdsPointer(p unsafe.Pointer) bool {
	return runtime.TypeOfPtr(heldType(p, d.empty)).Kind() == reflect.Pointer && *interfaceData(p) != nil
}

// heldType returns the type of the value which the interface value at p holds, which is not nil: the first word of
// an interface{}, if empty, or else the second word of the itab, the first word of an interface value of an interface
// type with methods.
func heldType(p unsafe.Pointer, empty bool) unsafe.Pointer {
	typ := *(*unsafe.Pointer)(p)
	if !empty {
		typ = (*[2]unsafe.Pointer)(typ)[1]
	}
	return typ
}

// interfaceData returns the address of the data word of the interface value at p, of an interface type with methods
// or not: its second word, after its type or its itab.
func interfaceData(p unsafe.Pointer) *unsafe.Pointer {
	return &(*[2]unsafe.Pointer)(p)[1]
}

// decodeMerging decodes the object or the array at cursor into the interface{} at p, which holds nothing, a
// map[string]interface{}, which the object is merged into, or a []interface{}, whose array is reused: the value of a
// name the map has is decoded into the value of the name, as the value of a name which the object has twice, which
// V2AllowDuplicateNames lets it have, is decoded into the value of the first one.
func (d *v2InterfaceDecoder) decodeMerging(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	buf := ctx.Buf
	cursor = skipWhiteSpace(buf, cursor)
	held := *(*any)(p)
	switch buf[cursor] {
	case '{':
		m, ok := held.(map[string]any)
		if held != nil && !ok {
			return ctx.skipTypeError(cursor, depth, reflect.TypeOf(held))
		}
		if m == nil {
			m = map[string]any{}
			*(*any)(p) = m
		}
		return d.decodeObjectInto(ctx, cursor, depth, m)
	case '[':
		s, ok := held.([]any)
		if held != nil && !ok {
			return ctx.skipTypeError(cursor, depth, reflect.TypeOf(held))
		}
		next, err := d.decodeArrayInto(ctx, cursor, depth, &s)
		*(*any)(p) = s
		return next, err
	}
	if held != nil {
		return ctx.skipTypeError(cursor, depth, reflect.TypeOf(held))
	}
	return d.Decode(ctx, cursor, depth, p)
}

// decodeElem decodes the value at cursor of an object or an array which decodeMerging decodes, into the interface{}
// at p, which holds the value of its name in the map, or nothing: an object or an array into nothing is decoded by
// decodeMerging too, so that the input is read once, whatever names it has twice.
func (d *v2InterfaceDecoder) decodeElem(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	cursor = skipWhiteSpace(ctx.Buf, cursor)
	var next int64
	var err error
	if c := ctx.Buf[cursor]; (c == '{' || c == '[') && *(*any)(p) == nil {
		next, err = d.decodeMerging(ctx, cursor, depth, p)
	} else {
		next, err = d.Decode(ctx, cursor, depth, p)
	}
	if err != nil {
		// with ReportErrorsWithLegacySemantics, the decoding goes on after the value, as v2GoOnDecoder goes on: a
		// value of another kind than the one it is merged into, as an object into a []any, fails
		return ctx.goOnAfter(err, cursor, depth)
	}
	return next, nil
}

// decodeObjectInto decodes the object at cursor into m, merging the value of a name into the value of the name
// which m has.
func (d *v2InterfaceDecoder) decodeObjectInto(ctx *RuntimeContext, cursor, depth int64, m map[string]any) (int64, error) {
	buf := ctx.Buf
	depth++
	if depth > maxDecodeNestingDepth {
		return 0, errors.ErrExceededMaxDepth(buf[cursor], cursor)
	}
	cursor = skipWhiteSpace(buf, cursor+1)
	if buf[cursor] == '}' {
		return cursor + 1, nil
	}
	var names nameSet
	for {
		if buf[cursor] != '"' {
			return 0, syntaxErrorAt(buf, cursor, whereKey)
		}
		key, c, _, err := d.fast.stringDecoder.decodeString(ctx, cursor)
		if err != nil {
			return 0, err
		}
		if ctx.Option.V2&V2AllowInvalidUTF8 == 0 && strings.IndexByte(key, 0xef) >= 0 && !validV2Text(buf[cursor:c]) {
			return 0, errV2Text
		}
		if ctx.Option.V2&V2AllowDuplicateNames == 0 && !names.addCopy(ctx.V2Call(), nil, unsafeBytes(key)) {
			return 0, errV2Text
		}
		cursor = skipWhiteSpace(buf, c)
		if buf[cursor] != ':' {
			return 0, syntaxErrorAt(buf, cursor, whereAfterKey)
		}
		// the value of the name which m has, which is decoded into, but with MergeWithLegacySemantics, which replaces
		// it
		var value any
		if ctx.Option.V2&V2MergeWithLegacySemantics == 0 {
			value = m[key]
		}
		c, err = d.decodeElem(ctx, cursor+1, depth, unsafe.Pointer(&value))
		m[key] = value
		if err != nil {
			return 0, err
		}
		cursor = skipWhiteSpace(buf, c)
		switch buf[cursor] {
		case '}':
			names.end(ctx.v2)
			return cursor + 1, nil
		case ',':
			cursor = skipWhiteSpace(buf, cursor+1)
		default:
			return 0, syntaxErrorAt(buf, cursor, whereAfterPair)
		}
	}
}

// decodeArrayInto decodes the array at cursor into *s from its first element, which is set to nil before.
func (d *v2InterfaceDecoder) decodeArrayInto(ctx *RuntimeContext, cursor, depth int64, s *[]any) (int64, error) {
	buf := ctx.Buf
	depth++
	if depth > maxDecodeNestingDepth {
		return 0, errors.ErrExceededMaxDepth(buf[cursor], cursor)
	}
	*s = (*s)[:0]
	if *s == nil {
		*s = []any{}
	}
	cursor = skipWhiteSpace(buf, cursor+1)
	if buf[cursor] == ']' {
		return cursor + 1, nil
	}
	for {
		*s = append(*s, nil)
		c, err := d.decodeElem(ctx, cursor, depth, unsafe.Pointer(&(*s)[len(*s)-1]))
		if err != nil {
			return 0, err
		}
		cursor = skipWhiteSpace(buf, c)
		switch buf[cursor] {
		case ']':
			return cursor + 1, nil
		case ',':
			cursor++
		default:
			return 0, syntaxErrorAt(buf, cursor, whereAfterElement)
		}
	}
}

func (d *v2InterfaceDecoder) DecodePath(ctx *RuntimeContext, cursor, depth int64) ([][]byte, int64, error) {
	return nil, 0, fmt.Errorf("json: v2 decoder does not support decode path")
}

// maxListedNames is the number of the names of an object which a nameSet compares a name with: past it, they are
// kept in a map, so that an object of many names is not quadratic in its names.
const maxListedNames = 32

// nameSet is the names of an object, among which add finds a name that the object has twice: the n entries of the
// names of the call from the entry first ( see callNames ), and the copies from copies on, whose bits are in mask,
// and past maxListedNames, the keys of set too. A name whose bit is not in mask is not compared with the names, as
// most names are not. The zero nameSet has no name.
type nameSet struct {
	first, n, copies int
	mask             nameMask
	set              map[string]struct{}
}

// nameMask is the bits of the hashes of the names of an object ( see nameHash ), one of 256 for each name: the top
// eight bits of the hash, of which the top two choose the word and the next six the bit in the word.
type nameMask [4]uint64

// bit returns the word of the mask which has the bit of the hash h, and the bit.
func (m *nameMask) bit(h uint64) (*uint64, uint64) {
	return &m[h>>62], uint64(1) << (h >> 56 & 63)
}

// maxWordsName is the length of the longest name of the input whose hash is made of its words where the input has
// it, two words of eight bytes ( see inputNameWords ), if the input has room for them from the start of the name.
const maxWordsName = 16

// add adds the name buf[start:end] of the input, which is not written while the object is decoded, and reports
// whether the set didn't have it.
func (s *nameSet) add(call *V2Call, buf []byte, start, end int64) bool {
	return s.insert(call, buf, buf[start:end], start)
}

// addListed adds the name buf[start:end] of the input as add adds it, if it can without a call, and reports whether
// it did: a name of up to maxWordsName bytes, where buf has room for two words from start, of an object of fewer than
// maxListedNames names, whose bit is not in the mask, in an entry of the array of the call which has room. It makes
// no call, so that it has no frame and doesn't save its arguments, as insert does for the calls on its other paths:
// the loops which add most names call it before add.
func (s *nameSet) addListed(call *V2Call, buf []byte, start, end int64) bool {
	n := end - start
	if n > maxWordsName || start+maxWordsName > int64(cap(buf)) || s.set != nil || s.n >= maxListedNames {
		return false
	}
	a, b := inputNameWords(buf, start, n)
	h := mixNameWords(a, b, n)
	t := &call.names
	entries := t.entries
	w, bit := s.mask.bit(h)
	if *w&bit != 0 || len(entries) >= cap(entries) {
		return false
	}
	*w |= bit
	if s.n == 0 {
		s.first, s.copies = len(entries), len(t.copies)
	}
	// within the capacity, which the compiler sees, so that the slice and the index need no call of a panic, and the
	// length of the entries of the call written alone: their array is the same, without the write barrier of its
	// pointer
	entries = entries[:len(entries)+1]
	entries[len(entries)-1] = nameEntry{hash: h, at: start, n: n}
	(*sliceHeader)(unsafe.Pointer(&t.entries)).len = len(entries)
	s.n++
	return true
}

// inputNameWords returns the words of the name buf[at:at+n] of the input of up to maxWordsName bytes, where buf has
// room for two words from at, as nameHash makes them of its bytes: the first word without the bytes after the name,
// and the last one for more than eight bytes.
func inputNameWords(buf []byte, at, n int64) (uint64, uint64) {
	a, b := load64(buf, at), uint64(0)
	if n < 8 {
		// the n low bytes of the word, which load64 reads little-endian: the mask of the shift, which is less than
		// 64, tells the compiler so
		a &= 1<<(uint(n)*8&63) - 1
	} else if n > 8 {
		// the last eight bytes, which overlap the first word if the name is shorter than 16
		b = load64(buf, at+n-8)
	}
	return a, b
}

// addCopy adds a copy of the name, and reports whether the set didn't have it: buf is the input of the names which
// add added to the set, if any.
func (s *nameSet) addCopy(call *V2Call, buf, name []byte) bool {
	return s.insert(call, buf, name, -1)
}

// insert adds the name, at the offset at of buf, the input, or copied if at is negative, and reports whether the set
// didn't have it. The hash of a name of the input of up to 16 bytes is made here of its words, as nameHash makes it,
// where buf has room for two words from at. The name is compared with the names of its bit by their hashes, and with
// the ones of its hash by their bytes. The entries of the call are written after the ones of the objects which its
// object is in, which the keys of set are: they are not written while the object is open, as the objects in it end
// before its next name, and an array of copies which the call leaves for a larger one is not written again.
func (s *nameSet) insert(call *V2Call, buf, name []byte, at int64) bool {
	t := &call.names
	if s.n == 0 {
		s.first, s.copies = len(t.entries), len(t.copies)
	}
	var h uint64
	if n := int64(len(name)); at >= 0 && n <= maxWordsName && at+maxWordsName <= int64(cap(buf)) {
		a, b := inputNameWords(buf, at, n)
		h = mixNameWords(a, b, n)
	} else {
		h = nameHash(name)
	}
	if s.set != nil {
		if _, exists := s.set[unsafeString(name)]; exists {
			return false
		}
	} else if w, bit := s.mask.bit(h); *w&bit == 0 {
		*w |= bit
	} else {
		for i := range t.entries[s.first:] {
			if e := &t.entries[s.first+i]; e.hash == h && e.n == int64(len(name)) && string(t.name(e, buf)) == string(name) {
				return false
			}
		}
	}
	if at < 0 {
		at = -1 - int64(len(t.copies))
		t.copies = append(t.copies, name...)
	}
	t.entries = append(t.entries, nameEntry{hash: h, at: at, n: int64(len(name))})
	s.n++
	switch {
	case s.set != nil:
		s.set[unsafeString(t.name(&t.entries[len(t.entries)-1], buf))] = struct{}{}
	case s.n > maxListedNames:
		s.set = make(map[string]struct{}, 2*s.n)
		for i := range t.entries[s.first:] {
			s.set[unsafeString(t.name(&t.entries[s.first+i], buf))] = struct{}{}
		}
	}
	return true
}

// end drops the names of the set from the names of the call, and its map, at the end of its object.
func (s *nameSet) end(call *V2Call) {
	if s.n != 0 {
		call.names.truncate(s.first, s.copies)
		s.set = nil
	}
}

// unsafeString is the string of the bytes of b, which are not written while it is used.
func unsafeString(b []byte) string { return unsafe.String(unsafe.SliceData(b), len(b)) }

// unsafeBytes is the bytes of the string s, which are not written.
func unsafeBytes(s string) []byte { return unsafe.Slice(unsafe.StringData(s), len(s)) }

// nameHash returns the hash of name, whose top eight bits are its bit in a nameMask: of its first word, of
// its first eight bytes, or of all of them for a name of less than eight bytes, mixed with the words between it and
// the last one, and of its last word, of its last eight bytes, for a name of more than eight bytes ( see
// mixNameWords ). The words of a name of up to maxWordsName bytes are all of its bytes.
func nameHash(name []byte) uint64 {
	n := len(name)
	var a, b uint64
	if n >= 8 {
		a = binary.LittleEndian.Uint64(name)
		if n > 8 {
			b = binary.LittleEndian.Uint64(name[n-8:])
			for i := 8; i < n-8; i += 8 {
				a = (a ^ binary.LittleEndian.Uint64(name[i:])) * 0x9E3779B97F4A7C15
			}
		}
	} else {
		// the bytes as one word, of the words of four bytes at both ends of the name, or of two, without the bytes
		// which the second word has of the first one
		switch {
		case n >= 4:
			a = uint64(binary.LittleEndian.Uint32(name)) | uint64(binary.LittleEndian.Uint32(name[n-4:]))>>(8*(8-n))<<32
		case n >= 2:
			a = uint64(binary.LittleEndian.Uint16(name)) | uint64(binary.LittleEndian.Uint16(name[n-2:]))>>(8*(4-n))<<16
		case n == 1:
			a = uint64(name[0])
		}
	}
	return mixNameWords(a, b, int64(n))
}

// mixNameWords returns the hash of a name of n bytes of the words a and b ( see nameHash ).
func mixNameWords(a, b uint64, n int64) uint64 {
	h := (a ^ bits.RotateLeft64(b, 29) ^ uint64(n)<<58) * 0x9E3779B97F4A7C15
	return h ^ h>>31
}

// callNames are the names of no field, and the other names which are kept, of the objects of a call which are being
// decoded, as encoding/json/jsontext keeps the names of the objects which are open: the entries of the names of an
// object, after the ones of the objects which it is in ( see nameSet ). A name is kept by its place in the input, or
// by a copy in copies, for a name which the input doesn't hold as it is, or which a stream may move: the copies keep
// nothing of the input. An error ends the objects around the object of the error too, but in the value of a method of
// a stream, whose error the method may ignore: the names of the objects which an error ended are dropped at the end
// of the value of a stream which they are in ( see Stream.DecodeV2After ), or when the stream or the context is
// released. A call which no error ended leaves none.
type callNames struct {
	entries []nameEntry
	copies  []byte
}

// nameEntry is a name of an object: its hash, and its n bytes, at the offset at of the input, or at the offset -1-at
// of the copies.
type nameEntry struct {
	hash uint64
	at   int64
	n    int64
}

// name returns the name of e, whose input is buf.
func (t *callNames) name(e *nameEntry, buf []byte) []byte {
	if e.at < 0 {
		at := -1 - e.at
		return t.copies[at : at+e.n]
	}
	return buf[e.at : e.at+e.n]
}

// mark returns the numbers of the entries and of the copies of the names of the call, which truncate goes back to.
func (t *callNames) mark() (int, int) { return len(t.entries), len(t.copies) }

// truncate drops the entries from entries on, and the copies from copies on. Back to no name, it drops the arrays
// which a value of many names or long names made large, over the sizes over which an encoder of jsontext drops the
// arrays of its names ( see putEncoder ), so that a context in the pool doesn't pin them: then no object has a name
// in them.
func (t *callNames) truncate(entries, copies int) {
	const large = 64 << 10
	if entries == 0 && (cap(t.copies) > large || cap(t.entries) > large/int(unsafe.Sizeof(nameEntry{}))) {
		*t = callNames{}
		return
	}
	t.entries = t.entries[:entries]
	t.copies = t.copies[:copies]
}
