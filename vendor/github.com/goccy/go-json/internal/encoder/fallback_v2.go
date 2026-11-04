package encoder

import (
	"reflect"
	"slices"
	"strings"
	"unsafe"

	"github.com/goccy/go-json/internal/errors"
	"github.com/goccy/go-json/internal/jsonfields"
	"github.com/goccy/go-json/internal/jsonstring"
	"github.com/goccy/go-json/internal/runtime"
)

// The embedded fallback of a struct of the v2 semantics: a field of a map of string keys or of jsontext.Value,
// whose members are members of the JSON object of the struct, after the members of its fields. It is encoded by
// a field of its own at the end of the struct, which writes no key: its value writes the members, followed by the
// comma after them which the field writes, or takes back the comma before them if there is none.

// fallback is the embedded fallback of a struct.
type fallback struct {
	root  reflect.Type // the struct whose address the field is given
	index []int        // the index of the fallback in it, by reflect.Type.FieldByIndex
	raw   bool         // whether it is jsontext.Value, or a map
	// names are the names of the members of the fields, which a name of the fallback may not be: by their exact
	// names, and by their folded names ( see jsonfields.FoldName ) in their breadth-first order. caseIgnore is
	// whether a field matches by its folded name ( case:ignore ) without MatchCaseInsensitiveNames.
	names      map[string]*jsonfields.Field
	folded     map[string][]*jsonfields.Field
	caseIgnore bool
}

// v2FallbackField returns the field of the embedded fallback of the struct typ, whose members are fields.
func (c *Compiler) v2FallbackField(typ reflect.Type, fields *jsonfields.Fields) *StructFieldCode {
	fb := &fallback{
		root:   typ,
		index:  fields.Fallback.Index,
		raw:    indirectOf(fields.Fallback.Type) == jsonfields.RawValueType,
		names:  map[string]*jsonfields.Field{},
		folded: map[string][]*jsonfields.Field{},
	}
	for i := range fields.List {
		f := &fields.List[i]
		fb.names[f.Name] = f
		folded := string(jsonfields.FoldName([]byte(f.Name)))
		fb.folded[folded] = append(fb.folded[folded], f)
		fb.caseIgnore = fb.caseIgnore || f.Casing&jsonfields.CaseIgnore != 0
	}
	for _, fs := range fb.folded {
		slices.SortFunc(fs, func(x, y *jsonfields.Field) int { return x.ID - y.ID })
	}
	var elem reflect.Type
	if !fb.raw {
		elem = indirectOf(fields.Fallback.Type).Elem()
	}
	return &StructFieldCode{
		typ:        typ,
		tag:        &runtime.StructTag{},
		isFallback: true,
		value: c.appendFuncCode(typ, func(ctx *RuntimeContext, b []byte, p unsafe.Pointer) ([]byte, error) {
			return fb.appendMembers(ctx, b, p, elem)
		}),
	}
}

func indirectOf(t reflect.Type) reflect.Type {
	if t.Kind() == reflect.Pointer && t.Name() == "" {
		return t.Elem()
	}
	return t
}

// appendMembers appends the members of the fallback of the struct at p, after b, which ends with the comma after
// the last member of the struct, or with the brace which begins the object.
func (fb *fallback) appendMembers(ctx *RuntimeContext, b []byte, p unsafe.Pointer, elem reflect.Type) ([]byte, error) {
	// a nil pointer on the way to the fallback, or to it, is no member.
	v, err := reflect.NewAt(fb.root, p).Elem().FieldByIndexErr(fb.index)
	present := err == nil
	if present && v.Kind() == reflect.Pointer {
		present = !v.IsNil()
		if present {
			v = v.Elem()
		}
	}
	out := b
	if present && v.Len() > 0 {
		// the members of the map or the raw value, which an empty one has none of.
		var insertName func(name []byte) bool
		if ctx.Option.Flag&AllowDuplicateNamesOption == 0 {
			names := &fallbackNames{
				fb: fb, before: b, others: fb.raw || ctx.RescanNames,
				flags: ctx.Option.Flag,
			}
			insertName = names.insertName
		}
		if fb.raw {
			out, err = V2Hooks.AppendRawMembers(ctx, b, v.Bytes(), insertName)
		} else {
			out, err = fb.appendMapMembers(ctx, b, v, elem, insertName)
		}
		if err != nil {
			return b, err
		}
	}
	// the comma after the members is written by the field, which takes back the one before them if none is.
	if out[len(out)-1] == ',' {
		out = out[:len(out)-1]
		ctx.Rewrote(len(out))
	}
	return out, nil
}

// fallbackNames decides which names of the members of a fallback may be written: neither the name of a member
// which is written already, of the object which before is in, nor the name of another member of the fallback.
//
// Most names are none of the names of the fields of the struct, which the names of the fallback are found in
// first: the members of the fields which the object has are read from its output only for a name of a field,
// which is written or was omitted. The names of the fallback itself are kept only where two of them can be the
// same: the members of a jsontext.Value, and the keys of a map, which are unique, but whose names are the same if
// the bytes of their invalid UTF-8 are written as U+FFFD ( see RuntimeContext.RescanNames ).
type fallbackNames struct {
	fb     *fallback
	before []byte
	// flags are the options of the encoding, of which MatchCaseInsensitiveNamesOption and
	// MatchCaseSensitiveDelimiterOption match the names, and others is whether the names of the fallback are kept.
	flags  OptionFlag
	others bool
	// written are the fields whose members are written, read from before when a name is the one of a field, and
	// otherNames the names of the fallback which are no field's.
	written    map[*jsonfields.Field]bool
	otherNames map[string]bool
}

// insertName records that the member of the fallback of the name is written, and reports whether it may be: the
// name is neither one which is written already nor, if the names of the fallback are kept, one of the fallback
// before it.
func (n *fallbackNames) insertName(name []byte) bool {
	fb := n.fb
	if f := fb.names[string(name)]; f != nil {
		return n.insertField(f)
	}
	if insensitive := n.flags&MatchCaseInsensitiveNamesOption != 0; fb.caseIgnore || insensitive {
		for _, f := range fb.folded[string(jsonfields.FoldName(name))] {
			if (f.Casing&jsonfields.CaseIgnore != 0 || (insensitive && f.Casing&jsonfields.CaseStrict == 0)) &&
				(n.flags&MatchCaseSensitiveDelimiterOption == 0 || strings.EqualFold(string(name), f.Name)) {
				return n.insertField(f)
			}
		}
	}
	if !n.others {
		return true
	}
	if n.otherNames == nil {
		n.otherNames = map[string]bool{}
	} else if n.otherNames[string(name)] {
		return false
	}
	n.otherNames[string(name)] = true
	return true
}

// insertField records that the member of f is written, and reports whether it was not: the members of the fields
// which the object has are read from its output the first time.
func (n *fallbackNames) insertField(f *jsonfields.Field) bool {
	if n.written == nil {
		n.written = n.writtenFields()
	}
	if n.written[f] {
		return false
	}
	n.written[f] = true
	return true
}

// writtenFields returns the fields whose members the object which the output before is in has.
func (n *fallbackNames) writtenFields() map[*jsonfields.Field]bool {
	written := map[*jsonfields.Field]bool{}
	obj := n.before[enclosingObjectStart(n.before):]
	for _, pos := range memberNames(append(obj[:len(obj):len(obj)], '}')) {
		end := stringEnd(obj, pos)
		name := jsonstring.AppendUnescaped(nil, obj[pos+1:end])
		if f := n.fb.names[string(name)]; f != nil {
			written[f] = true
		}
	}
	return written
}

// enclosingObjectStart returns where the object starts which the output b, compact and valid, is in: the
// brace before it which no other value contains.
func enclosingObjectStart(b []byte) int {
	depth := 0
	for i := len(b) - 1; i >= 0; i-- {
		switch b[i] {
		case '"':
			i = stringStart(b, i)
		case '}', ']':
			depth++
		case '{', '[':
			if depth == 0 {
				return i
			}
			depth--
		}
	}
	return 0
}

// appendMapMembers appends the entries of the map m, of string keys, as members, by the order of the keys if the
// output is deterministic.
func (fb *fallback) appendMapMembers(ctx *RuntimeContext, b []byte, m reflect.Value, elem reflect.Type, insertName func([]byte) bool) ([]byte, error) {
	if m.Len() == 0 {
		return b, nil
	}
	keys := m.MapKeys()
	if ctx.Option.Flag&UnorderedMapOption == 0 {
		slices.SortFunc(keys, func(x, y reflect.Value) int {
			return strings.Compare(x.String(), y.String())
		})
	}
	value := reflect.New(elem).Elem()
	for _, k := range keys {
		before := b
		out, err := appendName(ctx, b, k.String())
		if err != nil {
			// a name of invalid UTF-8, which is reported for the type of the key.
			return before, &errors.SemanticError{GoType: k.Type(), Err: errors.ErrInvalidUTF8}
		}
		if insertName != nil {
			name := jsonstring.AppendUnescaped(nil, out[len(b)+1:len(out)-1])
			if !insertName(name) {
				return before, &errors.TextError{Err: errors.ErrDuplicateName, Name: string(name), Out: before, AtDelim: true}
			}
		}
		b = append(out, ':')
		value.Set(m.MapIndex(k))
		b, err = appendValueOf(ctx, b, elem, value.Addr().UnsafePointer())
		if err != nil {
			return b, err
		}
		b = append(b, ',')
	}
	return b, nil
}

// appendValueOf appends the value at p, of typ, by its opcodes, which run in a context of their own, as the ones of
// a default representation do ( see appendDefault ).
func appendValueOf(ctx *RuntimeContext, b []byte, typ reflect.Type, p unsafe.Pointer) ([]byte, error) {
	codeSet, err := compileValueOf(ctx, typ)
	if err != nil {
		return b, err
	}
	return runInner(ctx, b, codeSet, p)
}

// compileValueOf returns the opcodes of a value of typ for the options of ctx.
func compileValueOf(ctx *RuntimeContext, typ reflect.Type) (*OpcodeSet, error) {
	if ctx.Option.Flag&MarshalFuncsOption != 0 {
		return ctx.Option.Funcs.codeSet(uintptr(runtime.TypePtr(typ)), ctx.compileMode())
	}
	return compileToGetUnfilteredCodeSet(uintptr(runtime.TypePtr(typ)), ctx.compileMode())
}
