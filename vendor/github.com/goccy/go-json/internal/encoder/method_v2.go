package encoder

import (
	"encoding/json"
	stderrors "errors"
	"reflect"
	"sync"
	"unsafe"

	"github.com/goccy/go-json/internal/errors"
	"github.com/goccy/go-json/internal/runtime"
)

// The methods of marshaling of the semantics of encoding/json/v2. The method of the pointer to a type is called for
// a value of the type, at its address, as a value is addressable for them, but for a value which is not addressable
// with CallMethodsWithLegacySemantics ( see v2MethodOfAt ). The first of them which a type has writes its value:
// MarshalJSONTo, MarshalJSON, AppendText, MarshalText.

// V2Hooks are what the v2 json package gives the encoder: the functions which take jsontext, which the encoder
// doesn't import.
var V2Hooks struct {
	// MarshalerToType is the interface of MarshalJSONTo.
	MarshalerToType reflect.Type
	// FormatRaw appends the raw value which a method or a function returned, checked and formatted as the options
	// of the context want it, which AppendFormattedRaw doesn't append ( see AppendRaw ): a SyntacticError of
	// jsontext for a value which is not valid, at its place in the output.
	FormatRaw func(ctx *RuntimeContext, b, raw []byte) ([]byte, error)
	// AppendRawMembers appends the members of the JSON object raw, a jsontext.Value embedded as a fallback, after
	// b, each followed by a comma: insertName records a name, and reports whether it may be written, if it is not
	// nil. Its errors are the ones of the v2 json package, at their places in the output.
	AppendRawMembers func(ctx *RuntimeContext, b, raw []byte, insertName func(name []byte) bool) ([]byte, error)
	// MarshalTo calls MarshalJSONTo of recv, the pointer to a value of typ, with an encoder which writes after the
	// output b.
	MarshalTo func(ctx *RuntimeContext, b []byte, typ reflect.Type, recv any) ([]byte, error)
	// MarshalToOf returns the function which calls MarshalJSONTo of the value of typ at an address, as MarshalTo
	// does, with the method found once for the type, or appendDefault if it declines ( see ErrUseDefault ).
	MarshalToOf func(typ reflect.Type, appendDefault AppendFunc) AppendFunc
}

// ErrUseDefault is the error of a method or a function of marshaling which declined to write the value: the
// value is written as if it didn't have it.
var ErrUseDefault = stderrors.New("use the default representation")

// implementsAddr reports whether a value of the type t, which is addressable, has the methods of iface.
func implementsAddr(t, iface reflect.Type) bool {
	return t.Implements(iface) || reflect.PointerTo(t).Implements(iface)
}

// allMethods is the method after which appendDefault appends a value: none, its functions included.
const allMethods = -1

// The methods of marshaling, in the order of their precedence.
const (
	noMethod = iota
	methodMarshalJSONTo
	methodMarshalJSON
	methodAppendText
	methodMarshalText
)

// v2MethodOf returns the method of marshaling which writes a value of the type, after the method after, or
// noMethod. A pointer or an interface value has none: the value it points to or holds has, which is addressable.
func v2MethodOf(typ reflect.Type, after int) int {
	if typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Interface {
		return noMethod
	}
	switch {
	case after < methodMarshalJSONTo && V2Hooks.MarshalerToType != nil && implementsAddr(typ, V2Hooks.MarshalerToType):
		return methodMarshalJSONTo
	case after < methodMarshalJSON && implementsAddr(typ, marshalJSONType):
		return methodMarshalJSON
	case after < methodAppendText && implementsAddr(typ, appendTextType):
		return methodAppendText
	case after < methodMarshalText && implementsAddr(typ, marshalTextType):
		return methodMarshalText
	}
	return noMethod
}

// v2MethodOfAt is v2MethodOf of a value which is addressable or not: with CallMethodsWithLegacySemantics, a method of
// a pointer receiver is not called for a value which is not addressable, as encoding/json doesn't call it, and the
// method after it or the kind writes the value.
func (c *Compiler) v2MethodOfAt(typ reflect.Type, after int, addressable bool) int {
	return methodOfAt(typ, after, addressable || c.v1Options&modeCallMethodsWithLegacySemantics == 0)
}

// methodOfAt is v2MethodOf of a value which is addressable or not: a method of a pointer receiver is passed over for
// a value which is not, for the method after it.
func methodOfAt(typ reflect.Type, after int, addressable bool) int {
	m := v2MethodOf(typ, after)
	for !addressable && m != noMethod && !typ.Implements(marshalerTypeOf(m)) {
		m = v2MethodOf(typ, m)
	}
	return m
}

// marshalerTypeOf returns the interface type of the method of marshaling m.
func marshalerTypeOf(m int) reflect.Type {
	switch m {
	case methodMarshalJSONTo:
		return V2Hooks.MarshalerToType
	case methodMarshalJSON:
		return marshalJSONType
	case methodAppendText:
		return appendTextType
	}
	return marshalTextType
}

// v2MethodOfInterface returns the first method of marshaling which the interface type typ has, or noMethod.
func v2MethodOfInterface(typ reflect.Type) int {
	for m := methodMarshalJSONTo; m <= methodMarshalText; m++ {
		if iface := marshalerTypeOf(m); iface != nil && typ.Implements(iface) {
			return m
		}
	}
	return noMethod
}

// appendV1Interface returns the function which appends the value of the interface type typ at p, of the method
// of marshaling m, with CallMethodsWithLegacySemantics: a nil pointer which it holds is written by the method, called
// for the nil pointer, as encoding/json calls it, if the method is of the pointer; any other value as the one of an
// interface{}.
func appendV1Interface(typ reflect.Type, m int) AppendFunc {
	return func(ctx *RuntimeContext, b []byte, p unsafe.Pointer) ([]byte, error) {
		v := reflect.NewAt(typ, p).Elem()
		if v.IsNil() {
			return append(b, "null"...), nil
		}
		held := v.Elem()
		if held.Kind() != reflect.Pointer || !held.IsNil() || held.Type().Elem().Implements(marshalerTypeOf(m)) {
			// a value, or a nil pointer whose method is of the value it points to, which can't be called for it:
			// written as any other
			value := held.Interface()
			return appendValueOf(ctx, b, emptyInterfaceType, unsafe.Pointer(&value))
		}
		recv := v.Interface()
		switch m {
		case methodMarshalJSONTo:
			return V2Hooks.MarshalTo(ctx, b, held.Type(), recv)
		case methodMarshalJSON:
			raw, err := recv.(json.Marshaler).MarshalJSON()
			return appendV2Raw(ctx, held.Type(), b, raw, err)
		}
		return appendTextOf(ctx, b, held.Type(), m, recv)
	}
}

// v2HasMethod reports whether an addressable value of the type is written by a function or a method of marshaling:
// of the type of a default representation, after the ones which declined it ( see v2Code ).
func (c *Compiler) v2HasMethod(typ reflect.Type) bool {
	return c.v2HasMethodAt(typ, true)
}

// v2HasMethodAt is v2HasMethod for a value which is addressable or not: with CallMethodsWithLegacySemantics, the
// methods of the pointer don't write a value which is not ( see v2MethodOfAt ).
func (c *Compiler) v2HasMethodAt(typ reflect.Type, addressable bool) bool {
	if typ == c.v2DefaultType {
		return c.v2MethodOfAt(typ, c.v2DefaultAfter, addressable) != noMethod
	}
	return len(c.funcs.funcsOf(typ)) > 0 || c.v2MethodOfAt(typ, noMethod, addressable) != noMethod
}

// v2MethodCode returns the code of a value of typ, addressable or not, written by its method of marshaling after the
// method after, and true, if it has one.
func (c *Compiler) v2MethodCode(typ reflect.Type, after int, addressable bool) (Code, bool) {
	switch c.v2MethodOfAt(typ, after, addressable) {
	case methodMarshalJSONTo:
		return c.appendFuncCode(typ, V2Hooks.MarshalToOf(typ, func(ctx *RuntimeContext, b []byte, p unsafe.Pointer) ([]byte, error) {
			return appendDefault(ctx, b, typ, p, methodMarshalJSONTo, addressable)
		})), true
	case methodMarshalJSON:
		if typ == rawMessageType {
			// the output of its method is the value itself, or null: the method, which allocates null, is not
			// called.
			return c.appendFuncCode(typ, func(ctx *RuntimeContext, b []byte, p unsafe.Pointer) ([]byte, error) {
				raw := *(*[]byte)(p)
				if raw == nil {
					raw = nullJSON
				}
				out, err := AppendRaw(ctx, b, raw)
				if err != nil {
					return b, &errors.MethodError{GoType: typ, Err: err, Kind: errors.MethodMarshalJSON}
				}
				return out, nil
			}), true
		}
		appendOutput := addrJSONAppender(typ)
		// the method is called by its code, as the encoder of v1 calls it ( see MarshalerCall ).
		marshalJSON := addrMethod[func(unsafe.Pointer) ([]byte, error)](typ, marshalJSONInterface)
		code := c.appendFuncCode(typ, func(ctx *RuntimeContext, b []byte, p unsafe.Pointer) ([]byte, error) {
			if appendOutput != nil {
				// the output is known, and valid: the method is called only for its error.
				if out, ok := appendOutput(b, p); ok {
					return out, nil
				}
			}
			raw, err := marshalJSON(p)
			return appendV2Raw(ctx, typ, b, raw, err)
		})
		if m := newMarshalerCall(reflect.PointerTo(typ), marshalJSONInterface); m != nil {
			// called by appendValue without the function ( see MarshalerCall.v2Raw ).
			m.nilIsNull, m.trusted, m.appendOutput, m.v2Raw, m.recv = false, false, appendOutput, true, typ
			m.appendValue = code.appendValue
			code.call = m
		}
		return code, true
	case methodAppendText:
		appendText := addrMethod[func(unsafe.Pointer, []byte) ([]byte, error)](typ, appendTextInterface)
		return c.appendFuncCode(typ, func(ctx *RuntimeContext, b []byte, p unsafe.Pointer) ([]byte, error) {
			return appendMethodText(ctx, b, typ, errors.MethodAppendText, p, appendText)
		}), true
	case methodMarshalText:
		marshalText := addrMethod[func(unsafe.Pointer) ([]byte, error)](typ, marshalTextInterface)
		return c.appendFuncCode(typ, func(ctx *RuntimeContext, b []byte, p unsafe.Pointer) ([]byte, error) {
			return appendMethodText(ctx, b, typ, errors.MethodMarshalText, p, func(p unsafe.Pointer, dst []byte) ([]byte, error) {
				text, err := marshalText(p)
				return append(dst, text...), err
			})
		}), true
	}
	return nil, false
}

var (
	// rawMessageType is the type of json.RawMessage, which is jsontext.Value of the standard library where Go has
	// it.
	rawMessageType = reflect.TypeOf(json.RawMessage(nil))
	nullJSON       = []byte("null")
)

// appendV2Raw appends raw, which MarshalJSON of a value of typ returned with err, as a raw value of the v2
// semantics ( see AppendRaw ), or returns the error of the method.
func appendV2Raw(ctx *RuntimeContext, typ reflect.Type, b, raw []byte, err error) ([]byte, error) {
	if err == nil && ctx.Option.Flag&(RawRewriteOption|NormalizeUTF8Option|HTMLEscapeOption) == 0 && len(raw) > 0 &&
		!notPureRaw.Has(raw) && walkRaw[plainStrings](nil, raw, ctx.levelsOfRaw()) {
		// a value of ASCII without a control character and an escape, as most are, for the options which append
		// it as it is ( see AppendFormattedRaw ), without the calls of AppendRaw.
		return append(b, raw...), nil
	}
	if err != nil {
		return b, &errors.MethodError{GoType: typ, Err: unsupportedError(err, errors.MethodMarshalJSON), Kind: errors.MethodMarshalJSON}
	}
	out, err := AppendRaw(ctx, b, raw)
	if err != nil {
		return b, &errors.MethodError{GoType: typ, Err: err, Kind: errors.MethodMarshalJSON}
	}
	return out, nil
}

// AppendRaw appends the raw value which a method or a function returned, checked and formatted as the options of
// the context want it: as it is, or as AppendFormattedRaw writes it, without a call of the v2 json package, which
// formats the other values.
func AppendRaw(ctx *RuntimeContext, b, raw []byte) ([]byte, error) {
	if ctx.Option.Flag&RawRewriteOption == 0 {
		if out, ok := AppendFormattedRaw(ctx, b, raw); ok {
			return out, nil
		}
	}
	return V2Hooks.FormatRaw(ctx, b, raw)
}

// addrMethod returns the method of iface of a value of typ at an address, which the value or the pointer to it
// has, as a function of the address, which is the data word of the pointer: it is looked up when the type is
// compiled, as the encoder of v1 does ( see methodCode ), and not converted to an interface value by every call.
// The pointer to typ implements iface ( see v2MethodOf ), so the method is found by its name.
func addrMethod[F any](typ reflect.Type, iface *marshalerInterface) F {
	fn, _ := methodCode(reflect.PointerTo(typ), iface)
	code := &fn // a func value is a pointer to its code
	return *(*F)(unsafe.Pointer(&code))
}

// addrJSONAppender returns the function which writes the output of MarshalJSON of the value of typ at an address
// without calling it, as the encoder of v1 does ( see newMarshalerCall ): for a type of the standard library whose
// output is known, or a type which has the method from such a type embedded in its value. It returns nil for the
// other types.
func addrJSONAppender(typ reflect.Type) func([]byte, unsafe.Pointer) ([]byte, bool) {
	if runtime.IsStdMarshalerType(typ) {
		return stdJSONAppender(typ)
	}
	if origin, offset, inline, ok := runtime.PromotedStdMethod(typ, runtime.MarshalJSONMethod); ok && inline {
		return embeddedAppender(stdJSONAppender(origin), offset)
	}
	return nil
}

// RunHook runs the VM of the options of ctx for the opcodes of a value, which ctx is set up for, after the output
// b: the run package sets it, which the encoder can't import.
var RunHook func(ctx *RuntimeContext, b []byte, codeSet *OpcodeSet) ([]byte, error)

// defaultKey is the key of the opcodes of the default representation of a type, in a compile mode, after the
// functions and a method, of a value which is addressable or not ( see appendDefault ).
type defaultKey struct {
	typ         reflect.Type
	mode        compileMode
	addressable bool
	after       int
}

// defaultCodeSets are the opcodes of the default representations ( see appendDefault ) without functions of
// marshaling: the ones with functions are kept by the functions.
var defaultCodeSets sync.Map // map[defaultKey]*OpcodeSet

// appendDefault appends the value at p, of typ, by its representation after the functions of marshaling and the
// method after, which declined to write it: the next method of it, or the representation of its type. The value
// is written by the opcodes of the representation, in a context of their own, after the output b: the VM runs
// once more within the method which called this. The value is addressable or not, as the one which the method was
// called for: with CallMethodsWithLegacySemantics, which calls the methods of pointers for addressable values only,
// the opcodes of an addressable value are the ones of the value of a pointer. Without it, all the values are written
// alike, by the opcodes of a value given by itself.
func appendDefault(ctx *RuntimeContext, b []byte, typ reflect.Type, p unsafe.Pointer, after int, addressable bool) ([]byte, error) {
	mode := ctx.compileMode()
	key := defaultKey{typ: typ, mode: mode, after: after, addressable: addressable && mode&modeCallMethodsWithLegacySemantics != 0}
	var funcs *MarshalFuncs
	cache := &defaultCodeSets
	if ctx.Option.Flag&MarshalFuncsOption != 0 {
		funcs = ctx.Option.Funcs
		cache = &funcs.defaults
	}
	v, ok := cache.Load(key)
	if !ok {
		c := newCompiler(key.mode)
		c.funcs = funcs
		if after != allMethods {
			c.v2DefaultType, c.v2DefaultAfter = typ, after
		}
		valueCode := c.valueCode
		if key.addressable {
			valueCode = c.pointeeCode
		}
		code, err := valueCode(typ)
		if err != nil {
			return b, err
		}
		codeSet, err := c.codeToOpcodeSet(typ, code)
		if err != nil {
			return b, err
		}
		v, _ = cache.LoadOrStore(key, codeSet)
	}
	codeSet := v.(*OpcodeSet)
	return runInner(ctx, b, codeSet, p)
}

// appendAddressable returns the function which appends the addressable value of typ at p by its representation, as
// appendDefault compiles it, with its functions and its methods ( see recursiveValueCode ).
func appendAddressable(typ reflect.Type) AppendFunc {
	return func(ctx *RuntimeContext, b []byte, p unsafe.Pointer) ([]byte, error) {
		return appendDefault(ctx, b, typ, p, allMethods, true)
	}
}

// runInner runs the opcodes of the value at p in a context of its own, after the output b, and returns the output
// without the comma after the value, which the caller writes. An error is at its place in that output.
func runInner(ctx *RuntimeContext, b []byte, codeSet *OpcodeSet, p unsafe.Pointer) ([]byte, error) {
	if ctx.ValueDepth+codeSet.Levels > MaxDepth {
		// the depth of the value, which the frames of the VM check when they enter it ( see EnterInterface ), but
		// which a context of its own starts at.
		return b, &errors.OutputError{Out: b, Err: ErrMaxDepth(ctx)}
	}
	inner := TakeRuntimeContext()
	*inner.Option = *ctx.Option
	// the recent opcodes of inner, which are the ones of the options of encoding/json of ctx
	SetFlags(inner, ctx.Option.Flag)
	// the escaper of the options of ctx is the one of ctx, which records invalid UTF-8 for it: inner sets its own.
	inner.Option.EscaperFlags = noEscaperFlags
	inner.Init(p, codeSet.CodeLength)
	// the value is where the function or the method which called this was called: one which declined it, or the one
	// of an inline fallback map, whose values these are ( see appendValueOf ).
	inner.BaseIndent = ctx.ValueDepth
	inner.Prefix, inner.IndentStr = ctx.Prefix, ctx.IndentStr
	inner.RescanNames = false
	inner.RewriteFrom = ctx.RewriteFrom
	out, err := b, EnterNested(inner, ctx, codeSet, p)
	if err == nil {
		out, err = RunHook(inner, b, codeSet)
	}
	if invalidOut, invalid := inner.InvalidUTF8Output(); invalid {
		// the first error, before which the encoding went on.
		out, err = invalidOut, &errors.TextError{Err: errors.ErrInvalidUTF8}
	}
	ctx.RescanNames = ctx.RescanNames || inner.RescanNames
	ctx.RewriteFrom = inner.RewriteFrom
	inner.Option.V2, inner.Option.Funcs = nil, nil
	ReleaseRuntimeContext(inner)
	if err != nil {
		if _, ok := err.(*errors.OutputError); !ok {
			err = &errors.OutputError{Out: out, Err: err}
		}
		return b, err
	}
	// without the comma after the value, which the caller writes, and the newline after it in an indented output
	// ( see AppendCommaIndent )
	end := len(out) - 1
	if ctx.Option.Flag&IndentOption != 0 {
		end--
	}
	ctx.Rewrote(end)
	return out[:end], nil
}

// appendMethodText appends the text which appendText, the method kind, appends for the value at p, as a JSON string.
func appendMethodText(ctx *RuntimeContext, b []byte, typ reflect.Type, kind errors.MethodKind, p unsafe.Pointer, appendText func(unsafe.Pointer, []byte) ([]byte, error)) ([]byte, error) {
	start := len(b)
	text, err := appendText(p, ctx.MarshalBuf[:0])
	if err != nil {
		return b, &errors.MethodError{GoType: typ, Err: unsupportedError(err, kind), Kind: kind}
	}
	ctx.MarshalBuf = text[:0]
	out, err := appendName(ctx, b, *(*string)(unsafe.Pointer(&text)))
	if err != nil {
		// invalid UTF-8, of the string at the place of the value.
		return b[:start], &errors.MethodError{GoType: typ, Err: err, Kind: kind}
	}
	return out, nil
}

// unsupportedError returns the error of the method kind, which must not report errors.ErrUnsupported: only
// MarshalJSONTo and the functions of MarshalToFunc may.
func unsupportedError(err error, kind errors.MethodKind) error {
	if stderrors.Is(err, stderrors.ErrUnsupported) {
		return stderrors.New(kind.String() + " method may not return errors.ErrUnsupported")
	}
	return err
}
