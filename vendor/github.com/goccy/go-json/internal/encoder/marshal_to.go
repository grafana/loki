package encoder

import (
	"bytes"
	"reflect"
	"sync"
	"unsafe"

	"github.com/goccy/go-json/internal/errors"
	"github.com/goccy/go-json/internal/jsonstring"
	"github.com/goccy/go-json/internal/options"
	"github.com/goccy/go-json/internal/runtime"
	"github.com/goccy/go-json/internal/textcoder"
	"github.com/goccy/go-json/jsontext"
)

// The MarshalJSONTo methods which the encoder of v1 calls, before MarshalJSON: the one which takes the Encoder of
// github.com/goccy/go-json/jsontext, which go-json replaces encoding/json/jsontext by, first, and the one which takes
// the Encoder of encoding/json/jsontext, which encoding/json of Go 1.27 calls ( see stdMarshalerToType ). A value
// with one is compiled as a value with MarshalJSON is, and the method writes to an encoder of its own, whose output
// is written as the output of MarshalJSON is. A method which declines, by errors.ErrUnsupported, leaves the value
// to the representation after it, as encoding/json/v2 documents it.

// marshalerToType is the interface of MarshalJSONTo with the Encoder of github.com/goccy/go-json/jsontext.
var marshalerToType = reflect.TypeFor[marshalerTo]()

type marshalerTo interface {
	MarshalJSONTo(*jsontext.Encoder) error
}

// toEncoder is an encoder of jsontext which a MarshalJSONTo method writes to, and the buffer of its output.
type toEncoder struct {
	buf bytes.Buffer
	enc *jsontext.Encoder
}

var toEncoders = sync.Pool{New: func() any {
	e := new(toEncoder)
	e.enc = jsontext.NewEncoder(&e.buf)
	return e
}}

// marshalTo returns what MarshalJSONTo of m writes, by the options of toConfig, and whether it declined.
func marshalTo(m marshalerTo) ([]byte, bool, error) {
	e := toEncoders.Get().(*toEncoder)
	defer toEncoders.Put(e)
	e.buf.Reset()
	e.enc.Reset(&e.buf, &toConfig)
	err := m.MarshalJSONTo(e.enc)
	_, n := e.enc.StackIndex(0)
	declined, err := errors.MethodCallResult(err, e.enc.StackDepth(), n)
	if err != nil || declined {
		return nil, declined, err
	}
	// the encoder ends a value at the top level with a newline
	return bytes.Clone(bytes.TrimSuffix(e.buf.Bytes(), []byte{'\n'})), false, nil
}

// toAttachment is the encoder of jsontext which MarshalJSONTo of github.com/goccy/go-json/jsontext writes to, for a
// compact output, attached after the output of a context, as a value at its top level ( see appendMarshalTo ), and
// the place where it is attached. A context has one, which it makes for its first such value.
type toAttachment struct {
	enc jsontext.Encoder
	pl  textcoder.Attachment
}

// toConfig are the options of the encoders which MarshalJSONTo of github.com/goccy/go-json/jsontext writes to: the
// ones of encoding/json, which DefaultOptionsV1 of the root package sets, as encoding/json gives its methods, by
// which MarshalEncode of v2 within the method encodes as the encoder of v1 does. The characters which the escapes of
// EscapeForHTML and EscapeForJS are for are escaped after the method, as in the output of MarshalJSON, by the options
// of the call ( see appendMarshalTo ).
var toConfig = options.Config{
	Set:   options.DefaultV1Flags,
	Value: options.DefaultV1Flags &^ (options.EscapeForHTML | options.EscapeForJS),
}

// htmlChars are the bytes which an output escaped for HTML has escaped: '<', '>', '&' and the first byte of U+2028
// and U+2029, which starts other characters as well ( see lookedAtByCompact ).
var htmlChars = jsonstring.NewByteClass('<', '>', '&', 0xe2)

// topLevel is the Outer of toAttachment: the value is at the top level of the encoder, which has no levels around
// it.
type topLevel struct{}

func (topLevel) Levels() []textcoder.Level { return []textcoder.Level{{}} }

// appendMarshalTo appends what MarshalJSONTo of m writes after b, by the encoder of the context attached after b,
// or returns that it declined. The pointers of the place are written only if they change, as a write of a pointer
// costs a write barrier while the GC marks: the output is mostly the same array, whose length is set by a slice of
// itself.
func appendMarshalTo(ctx *RuntimeContext, b []byte, m marshalerTo) ([]byte, bool, error) {
	a := ctx.marshalTo
	if a == nil {
		a = &toAttachment{pl: textcoder.Attachment{Top: true, Outer: topLevel{}, Opts: &toConfig}}
		ctx.marshalTo = a
	}
	pl := &a.pl
	if unsafe.SliceData(pl.Out) == unsafe.SliceData(b) && cap(pl.Out) == cap(b) {
		pl.Out = pl.Out[:len(b)]
	} else {
		pl.Out = b
	}
	pl.Count, pl.OuterDepth = 0, int(ctx.ValueDepth)
	textcoder.Attach(unsafe.Pointer(&a.enc), pl)
	pl.Same = true // the options of the next attach are the same
	err := m.MarshalJSONTo(&a.enc)
	textcoder.Detach(unsafe.Pointer(&a.enc), pl)
	if err == nil {
		// a call of MarshalEncode within the method which failed at the encoder
		err = pl.Failed
	}
	pl.Failed = nil
	declined, err := errors.MethodCallResult(err, pl.Depth, pl.Count)
	if err != nil || declined {
		return b, declined, err
	}
	out := pl.Out
	if ctx.Option.Flag&HTMLEscapeOption != 0 && htmlChars.Has(out[len(b):]) {
		out = jsonstring.AppendHTMLEscaped(out[:len(b)], bytes.Clone(out[len(b):]))
	}
	return out, false, nil
}

// implementsMarshalTo reports whether the encoder of v1 calls MarshalJSONTo of a value of the type, as the data word
// of its interface value: not where the method declined the value ( see declines ), not of an interface type, whose
// dynamic value has its own method, nor of a json.Number, which has the one of encoding/json/jsontext from Go 1.27,
// and which the encoder writes by itself, with the options and at the places which the method looks at.
func (c *Compiler) implementsMarshalTo(typ reflect.Type) bool {
	return !c.v2 && typ.Kind() != reflect.Interface && typ != jsonNumberType && typ != reflect.PointerTo(jsonNumberType) &&
		!c.declines(typ) && (typ.Implements(marshalerToType) || stdMarshalerToType != nil && typ.Implements(stdMarshalerToType))
}

// declines reports whether the type is the one of the value whose MarshalJSONTo declined, which is compiled by its
// representation after the method ( see appendDeclined ), or the pointer to it, or the value which it points to.
func (c *Compiler) declines(typ reflect.Type) bool {
	declined := c.v2DefaultType
	return declined != nil && (typ == declined || typ == reflect.PointerTo(declined) ||
		declined.Kind() == reflect.Pointer && typ == declined.Elem())
}

// marshalToCall returns the call of MarshalJSONTo of the receiver type, which a MarshalJSONCode makes, or nil if the
// encoder of v1 doesn't call one. addressable is whether the value is, which the representation after the method
// depends on, as the methods of its pointer are its own then.
func (c *Compiler) marshalToCall(recv reflect.Type, addressable bool) *MarshalerCall {
	if !c.implementsMarshalTo(recv) {
		return nil
	}
	typ := runtime.TypePtr(recv)
	// the method of github.com/goccy/go-json/jsontext is called by the interface value made of its table of methods,
	// found once, and the data word, without the check of a type assertion
	var tab *nonEmptyInterface
	if recv.Implements(marshalerToType) {
		proto, _ := reflect.New(recv).Elem().Interface().(marshalerTo)
		tab = (*nonEmptyInterface)(unsafe.Pointer(&proto))
	}
	// the error is of the pointer to the value, which encoding/json calls the method of
	errType := recv
	if errType.Kind() != reflect.Pointer {
		errType = reflect.PointerTo(recv)
	}
	m := &MarshalerCall{nilIsNull: recv.Kind() == reflect.Ptr, recv: recv}
	m.marshalTo = func(ctx *RuntimeContext, b []byte, p unsafe.Pointer) ([]byte, bool, error) {
		var out []byte
		var declined, appended bool
		var err error
		switch {
		case tab == nil:
			// the method of encoding/json/jsontext, which implementsMarshalTo found
			out, declined, err = stdMarshalTo(*(*any)(unsafe.Pointer(&emptyInterface{typ: typ, ptr: p})))
		case ctx.Option.Flag&IndentOption == 0:
			// written after b, compact and escaped as the options say, which nothing checks or copies again
			var m marshalerTo
			w := (*nonEmptyInterface)(unsafe.Pointer(&m))
			w.itab, w.ptr = tab.itab, p
			out, declined, err = appendMarshalTo(ctx, b, m)
			appended = true
		default:
			var m marshalerTo
			w := (*nonEmptyInterface)(unsafe.Pointer(&m))
			w.itab, w.ptr = tab.itab, p
			out, declined, err = marshalTo(m)
		}
		switch {
		case err != nil:
			return nil, false, errors.ErrMarshaler(errType, err, "MarshalJSONTo")
		case declined:
			out, err := appendDeclined(ctx, b, recv, p, addressable)
			return out, true, err
		}
		return out, appended, nil
	}
	return m
}

// declinedKey is what the opcodes of a value which its MarshalJSONTo declined are looked up by ( see appendDeclined ).
type declinedKey struct {
	recv        reflect.Type
	mode        compileMode
	addressable bool
}

var declinedCodeSets sync.Map // map[declinedKey]*OpcodeSet

// appendDeclined appends the value of recv whose data word is p, which its MarshalJSONTo declined, by its
// representation after the method, as the context writes it: indented and colored as the value would be. The value
// is written by the opcodes of its own type, at its address, which identifies it for the detection of cycles: a
// pointer is the address of the value it points to, which is addressable, and an addressable value has the methods
// of its pointer, as an element of a slice has.
func appendDeclined(ctx *RuntimeContext, b []byte, recv reflect.Type, p unsafe.Pointer, addressable bool) ([]byte, error) {
	typ, addr := recv, p
	switch {
	case recv.Kind() == reflect.Pointer:
		typ, addressable = recv.Elem(), true
	case !runtime.IfaceIndir(recv):
		// the data word is the value itself, whose address is the one of p
		addr = unsafe.Pointer(&p)
	}
	key := declinedKey{recv: recv, mode: ctx.compileMode(), addressable: addressable}
	v, ok := declinedCodeSets.Load(key)
	if !ok {
		c := newCompiler(key.mode)
		// MarshalJSONTo of recv, and of its pointer, is not called for the value ( see declines )
		c.v2DefaultType = recv
		var code Code
		var err error
		if addressable {
			code, err = c.listElemCode(typ, true)
		} else {
			code, err = c.valueCode(typ)
		}
		if err != nil {
			return b, err
		}
		if structCode, ok := code.(*StructCode); ok {
			structCode.enableIndirect()
		}
		codeSet, err := c.codeToOpcodeSet(typ, code)
		if err != nil {
			return b, err
		}
		v, _ = declinedCodeSets.LoadOrStore(key, codeSet)
	}
	out, err := runInner(ctx, b, v.(*OpcodeSet), addr)
	if outErr, ok := err.(*errors.OutputError); ok {
		// the error of the value itself, as the encoder of v1 reports it
		err = outErr.Err
	}
	return out, err
}
