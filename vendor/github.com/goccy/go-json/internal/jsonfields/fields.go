// Package jsonfields finds the JSON object members of a Go struct by the rules of encoding/json/v2: the fields
// which a value of the struct is marshaled to and unmarshaled from, with the options of their `json` tags.
//
// A Go struct is a list of fields found by a breadth-first search over its fields and the fields of the structs
// embedded in it. A field hides the fields of the same name deeper than it, and of two fields at the same depth
// the one whose name is given by its tag. An error in the definition of the struct is reported when a value of it
// is marshaled or unmarshaled, not when the type is seen: an empty map of it is still {}.
package jsonfields

import (
	"cmp"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/goccy/go-json/internal/jsonstring"
)

// The types which the json packages give this package: the ones it can't import.
var (
	// RawValueType is jsontext.Value, which is an embedded fallback.
	RawValueType reflect.Type
	// MethodTypes are the interfaces of the methods of marshaling and unmarshaling: a type which has one of them
	// can't be embedded.
	MethodTypes []reflect.Type
)

var isZeroerType = reflect.TypeFor[interface{ IsZero() bool }]()

// Casing is how the name of a field is matched when unmarshaling, by its `case` option.
type Casing int8

const (
	CaseIgnore Casing = 1 << iota // case:ignore, which ignores the case, '-' and '_'
	CaseStrict                    // case:strict
)

// Options are the options of the `json` tag of a field.
type Options struct {
	Name       string // the JSON object name
	QuotedName string // Name as a JSON string
	HasName    bool   // whether the tag gives the name
	Casing     Casing
	Embed      bool
	OmitZero   bool
	OmitEmpty  bool
	String     bool
	Format     string
}

// Field is a JSON object member of a struct.
type Field struct {
	Options
	// Index is the index of the field by reflect.Type.FieldByIndex: the fields of the embedded structs on the
	// way to it, and the field.
	Index []int
	Type  reflect.Type
	// ID is the place of the field in the breadth-first order of the fields.
	ID int
	// HasIsZero is whether the type of the field, or the pointer to it, has the IsZero method, which decides
	// whether the field is omitted by omitzero.
	HasIsZero bool
}

// Fields are the JSON object members of a struct.
type Fields struct {
	// List is the fields in the depth-first order of their indexes, which is the order they are marshaled in.
	List []Field
	// Fallback is the embedded field of a map or of jsontext.Value which holds the members of no other field, or
	// nil.
	Fallback *Field
	// Err is the first error of the definition of the struct, or nil.
	Err *Error
	// FormatErr is the error of the first field with the `format` option, which encoding/json/v2 doesn't
	// support, or nil.
	FormatErr *Error
}

// Error is an error of the definition of a struct type.
type Error struct {
	GoType reflect.Type
	Err    error
}

var errNoExportedFields = errors.New("Go struct has no exported fields") //nolint:staticcheck // the text of encoding/json/v2

// Of returns the JSON object members of the struct type t.
func Of(t reflect.Type) *Fields {
	b := &builder{fs: &Fields{}, queue: []visit{{t, nil, true}}, seen: map[reflect.Type]bool{t: true}}
	for len(b.queue) > 0 {
		v := b.queue[0]
		b.queue = b.queue[1:]
		b.visitStruct(v)
	}
	b.fs.List = dominantFields(b.all)
	if n := len(b.fallbacks); n == 1 || (n > 1 && len(b.fallbacks[0].Index) != len(b.fallbacks[1].Index)) {
		b.fs.Fallback = &b.fallbacks[0]
	}
	return b.fs
}

// builder finds the fields of a struct by a breadth-first search over the structs embedded in it ( see Of ).
type builder struct {
	fs    *Fields
	queue []visit
	seen  map[reflect.Type]bool
	// all are the members of every struct visited, and fallbacks the embedded fallbacks, of which the ones which
	// hide the others are chosen.
	all, fallbacks []Field
}

// visit is a struct of the search, at the index of the struct which embeds it.
type visit struct {
	typ      reflect.Type
	index    []int
	children bool // whether the structs embedded in it are visited
}

// structVisit is the state of a struct being visited.
type structVisit struct {
	visit
	names    map[string]int // the members of the struct by their names: the index of their field
	fallback int            // the index of the embedded fallback of the struct, or -1
}

func (b *builder) failf(typ reflect.Type, format string, args ...any) {
	if b.fs.Err == nil {
		b.fs.Err = &Error{GoType: typ, Err: fmt.Errorf(format, args...)}
	}
}

// visitStruct adds the members of the fields of the struct v, and queues the structs it embeds.
func (b *builder) visitStruct(v visit) {
	s := &structVisit{visit: v, names: map[string]int{}, fallback: -1}
	var hasTag, hasField bool
	for i := range v.typ.NumField() {
		sf := v.typ.Field(i)
		_, tagged := sf.Tag.Lookup("json")
		hasTag = hasTag || tagged
		opts, ignored, err := parseOptions(sf)
		if err != nil && b.fs.Err == nil {
			b.fs.Err = &Error{GoType: v.typ, Err: err}
		}
		if ignored {
			continue
		}
		hasField = true
		f := Field{Options: opts, Index: append(slices.Clip(v.index), i), Type: sf.Type}
		if sf.Anonymous && !f.HasName {
			if indirect(f.Type).Kind() == reflect.Struct {
				f.Embed = true // an embedded struct is embedded in the JSON object unless it is named
			} else {
				b.failf(v.typ, "embedded Go struct field %s of non-struct type must be explicitly given a JSON name", sf.Name)
			}
		}
		if f.Embed {
			b.addEmbedded(s, sf, f)
		} else {
			b.addMember(s, sf, f)
		}
	}
	// a struct which has fields but no member is most likely a mistake ( a struct of unexported fields, as an
	// error of errors.New is ), unless a field has a tag, which tells that it is meant to be so.
	if v.typ.NumField() > 0 && !hasTag && !hasField && b.fs.Err == nil {
		b.fs.Err = &Error{GoType: v.typ, Err: errNoExportedFields}
	}
}

// addMember adds the field f, a member of its own, of the struct s.
func (b *builder) addMember(s *structVisit, sf reflect.StructField, f Field) {
	typ := s.typ
	if !sf.IsExported() {
		// only an embedded struct of an unexported type may have exported fields.
		ft := indirect(f.Type)
		if !sf.Anonymous || ft.Kind() != reflect.Struct {
			b.failf(typ, "Go struct field %s is not exported", sf.Name)
			return
		}
		if implementsAny(ft, MethodTypes) || (f.OmitZero && implementsAny(ft, []reflect.Type{isZeroerType})) {
			b.failf(typ, "Go struct field %s is not exported for method calls", sf.Name)
			return
		}
	}
	f.HasIsZero = sf.Type.Implements(isZeroerType) || reflect.PointerTo(sf.Type).Implements(isZeroerType)
	if j, ok := s.names[f.Name]; ok {
		// the field is still listed: a field which hides both of them is chosen after.
		b.failf(typ, "Go struct fields %s and %s conflict over JSON object name %q", typ.Field(j).Name, sf.Name, f.Name)
	}
	s.names[f.Name] = f.Index[len(f.Index)-1]
	f.ID = len(b.all)
	b.all = append(b.all, f)
	if f.Format != "" && b.fs.FormatErr == nil {
		b.fs.FormatErr = &Error{GoType: typ, Err: fmt.Errorf("Go struct field %s has unsupported `format` tag option", sf.Name)} //nolint:staticcheck // the text of encoding/json/v2
	}
}

// addEmbedded adds the field f, whose members are members of the object of the struct s: a struct, whose fields
// are visited later, or an embedded fallback.
func (b *builder) addEmbedded(s *structVisit, sf reflect.StructField, f Field) {
	typ := s.typ
	if f.Options != (Options{Name: f.Name, QuotedName: f.QuotedName, Embed: true}) {
		b.failf(typ, "Go struct field %s cannot have any options other than `embed` specified", sf.Name)
		if f.HasName {
			b.addMember(s, sf, f)
			return
		}
		f.Options = Options{Name: f.Name, QuotedName: f.QuotedName, Embed: true}
	}
	ft := indirect(f.Type)
	if ft != RawValueType && implementsAny(ft, MethodTypes) {
		b.failf(typ, "embedded Go struct field %s of type %s must not implement marshal or unmarshal methods", sf.Name, ft)
	}
	if ft.Kind() == reflect.Struct {
		if s.children {
			b.queue = append(b.queue, visit{ft, f.Index, !b.seen[ft]})
		}
		b.seen[ft] = true
		return
	}
	if !sf.IsExported() {
		b.failf(typ, "embedded Go struct field %s is not exported", sf.Name)
		return
	}
	switch {
	case ft == RawValueType:
	case ft.Kind() == reflect.Map && ft.Key().Kind() == reflect.String:
		if implementsAny(ft.Key(), MethodTypes) {
			b.failf(typ, "embedded map field %s of type %s must have a string key that does not implement marshal or unmarshal methods", sf.Name, ft)
			b.addMember(s, sf, f)
			return
		}
	default:
		b.failf(typ, "embedded Go struct field %s of type %s must be a Go struct, Go map of string key, or jsontext.Value", sf.Name, ft)
		b.addMember(s, sf, f)
		return
	}
	if s.fallback >= 0 {
		// it is still listed: one deeper than the other is chosen after.
		b.failf(typ, "embedded Go struct fields %s and %s cannot both be a Go map or jsontext.Value", typ.Field(s.fallback).Name, sf.Name)
	}
	s.fallback = f.Index[len(f.Index)-1]
	b.fallbacks = append(b.fallbacks, f)
}

// dominantFields returns the members of the object of the fields all: of the fields of a name, the one at the
// shallowest depth, or the one of them with the name in its tag if the others have none, and otherwise none. They
// are numbered in their breadth-first order, and listed in their depth-first order.
func dominantFields(all []Field) []Field {
	slices.SortStableFunc(all, func(x, y Field) int {
		return cmp.Or(strings.Compare(x.Name, y.Name), cmp.Compare(len(x.Index), len(y.Index)), compareBool(!x.HasName, !y.HasName))
	})
	var list []Field
	for len(all) > 0 {
		n := 1
		for n < len(all) && all[n].Name == all[0].Name {
			n++
		}
		if n == 1 || len(all[0].Index) != len(all[1].Index) || all[0].HasName != all[1].HasName {
			list = append(list, all[0])
		}
		all = all[n:]
	}
	slices.SortFunc(list, func(x, y Field) int { return cmp.Compare(x.ID, y.ID) })
	for i := range list {
		list[i].ID = i
	}
	slices.SortFunc(list, func(x, y Field) int { return slices.Compare(x.Index, y.Index) })
	return list
}

// indirect returns the type a field of type t embeds: the element of an unnamed pointer, or t.
func indirect(t reflect.Type) reflect.Type {
	if t.Kind() == reflect.Pointer && t.Name() == "" {
		return t.Elem()
	}
	return t
}

func implementsAny(t reflect.Type, ifaces []reflect.Type) bool {
	for _, iface := range ifaces {
		if t.Implements(iface) || reflect.PointerTo(t).Implements(iface) {
			return true
		}
	}
	return false
}

// compareBool orders false before true.
func compareBool(x, y bool) int {
	switch {
	case !x && y:
		return -1
	case x && !y:
		return +1
	}
	return 0
}

// parseOptions parses the `json` tag of the field. A field is ignored by the tag "-", and an unexported field
// which is not embedded is ignored, whose tag is an error unless it is "-".
func parseOptions(sf reflect.StructField) (Options, bool, error) {
	var opts Options
	var err error
	tag, tagged := sf.Tag.Lookup("json")
	if tag == "-" {
		return Options{}, true, nil
	}
	fail := func(format string, args ...any) {
		if err == nil {
			err = fmt.Errorf(format, args...)
		}
	}
	if !sf.IsExported() && !sf.Anonymous {
		if tagged {
			fail("unexported Go struct field %s cannot have non-ignored `json:%q` tag", sf.Name, tag)
		}
		return Options{}, true, err
	}

	opts.Name = sf.Name
	if tag != "" && tag[0] != ',' {
		// almost any text is a name, up to the comma: a backslash and the quotes are reserved.
		n := strings.IndexAny(tag, ",\\'\"`")
		if n < 0 {
			n = len(tag)
		}
		name := tag[:n]
		var nameErr error
		if n < len(tag) && tag[n] != ',' {
			name, n, nameErr = consumeOption(tag, false)
			if nameErr != nil {
				fail("Go struct field %s has malformed `json` tag: %v", sf.Name, nameErr)
			}
		}
		if !utf8.ValidString(name) {
			fail("Go struct field %s has JSON object name %q with invalid UTF-8", sf.Name, name)
			name = string([]rune(name))
		}
		if nameErr == nil {
			opts.HasName = true
			opts.Name = name
		}
		tag = tag[n:]
	}
	opts.QuotedName = quoteName(opts.Name)

	var formatSeen bool
	seen := map[string]bool{}
	for tag != "" {
		if tag[0] != ',' {
			fail("Go struct field %s has malformed `json` tag: invalid character %q before next option (expecting ',')", sf.Name, tag[0])
		} else {
			tag = tag[1:]
			if tag == "" {
				fail("Go struct field %s has malformed `json` tag: invalid trailing ',' character", sf.Name)
				break
			}
		}
		opt, n, optErr := consumeOption(tag, false)
		if optErr != nil {
			fail("Go struct field %s has malformed `json` tag: %v", sf.Name, optErr)
		}
		raw := tag[:n]
		tag = tag[n:]
		switch {
		case formatSeen:
			fail("Go struct field %s has `format` tag option that was not specified last", sf.Name)
		case strings.HasPrefix(raw, "'") && strings.TrimFunc(opt, isLetterOrDigit) == "":
			fail("Go struct field %s has unnecessarily quoted appearance of `%s` tag option; specify `%s` instead", sf.Name, raw, opt)
		}
		switch opt {
		case "case":
			if !strings.HasPrefix(tag, ":") {
				fail("Go struct field %s is missing value for `case` tag option; specify `case:ignore` or `case:strict` instead", sf.Name)
				break
			}
			tag = tag[1:]
			value, n, valueErr := consumeOption(tag, false)
			if valueErr != nil {
				fail("Go struct field %s has malformed value for `case` tag option: %v", sf.Name, valueErr)
				break
			}
			rawValue := tag[:n]
			tag = tag[n:]
			if strings.HasPrefix(rawValue, "'") {
				fail("Go struct field %s has unnecessarily quoted appearance of `case:%s` tag option; specify `case:%s` instead", sf.Name, rawValue, value)
			}
			switch value {
			case "ignore":
				opts.Casing |= CaseIgnore
			case "strict":
				opts.Casing |= CaseStrict
			default:
				fail("Go struct field %s has unknown `case:%s` tag value", sf.Name, rawValue)
			}
		case "embed":
			opts.Embed = true
		case "omitzero":
			opts.OmitZero = true
		case "omitempty":
			opts.OmitEmpty = true
		case "string":
			opts.String = true
		case "format":
			if !strings.HasPrefix(tag, ":") {
				fail("Go struct field %s is missing value for `format` tag option", sf.Name)
				break
			}
			tag = tag[1:]
			value, n, valueErr := consumeOption(tag, true)
			if valueErr != nil {
				fail("Go struct field %s has malformed value for `format` tag option: %v", sf.Name, valueErr)
				break
			}
			if value == "" {
				fail("Go struct field %s cannot have empty value for `format` tag option", sf.Name)
				break
			}
			tag = tag[n:]
			opts.Format = value
			formatSeen = true
		default:
			// an option which looks like one of them by its letters is a mistake for it.
			switch norm := strings.ReplaceAll(strings.ToLower(opt), "_", ""); norm {
			case "case", "embed", "omitzero", "omitempty", "string", "format":
				fail("Go struct field %s has invalid appearance of `%s` tag option; specify `%s` instead", sf.Name, opt, norm)
			}
			// the other options are ignored, as a future version may know them.
		}
		switch {
		case opts.Casing == CaseIgnore|CaseStrict:
			fail("Go struct field %s cannot have both `case:ignore` and `case:strict` tag options", sf.Name)
		case seen[opt]:
			fail("Go struct field %s has duplicate appearance of `%s` tag option", sf.Name, raw)
		}
		seen[opt] = true
	}
	return opts, false, err
}

// consumeOption returns the option at the start of in, a Go identifier or, if quoted is true, a single-quoted
// string, and its length in in. An invalid option is the text up to the next comma, with an error.
func consumeOption(in string, quoted bool) (string, int, error) {
	end := strings.IndexByte(in, ',')
	if end < 0 {
		end = len(in)
	}
	r, _ := utf8.DecodeRuneInString(in)
	switch {
	case r == '_' || unicode.IsLetter(r):
		n := len(in) - len(strings.TrimLeftFunc(in, isLetterOrDigit))
		return in[:n], n, nil
	case in == "":
		return in, 0, fmt.Errorf("unexpected EOF")
	case r == '\'' && quoted:
		// a Go string literal in single quotes, which a struct tag can have where it can't have double quotes.
		lit := []byte{'"'}
		escaped := false
		for n := 1; n < len(in); {
			r, size := utf8.DecodeRuneInString(in[n:])
			switch {
			case escaped:
				if r == '\'' {
					lit = lit[:len(lit)-1] // \' is '
				}
				escaped = false
			case r == '\\':
				escaped = true
			case r == '"':
				lit = append(lit, '\\')
			case r == '\'':
				n++
				s, err := strconv.Unquote(string(append(lit, '"')))
				if err != nil {
					return in[:end], end, fmt.Errorf("invalid single-quoted string: %s", in[:n])
				}
				return s, n, nil
			}
			lit = append(lit, in[n:n+size]...)
			n += size
		}
		return in[:end], end, fmt.Errorf("single-quoted string not terminated: %s...", in[:min(len(in), 10)]) //nolint:staticcheck // the text of encoding/json/v2
	case quoted:
		return in[:end], end, fmt.Errorf("invalid character %q at start of option (expecting Unicode letter or single quote)", r)
	default:
		return in[:end], end, fmt.Errorf("invalid character %q at start of option (expecting Unicode letter)", r)
	}
}

// quoteName returns the name as a JSON string, escaped as jsontext escapes a string by default.
func quoteName(name string) string {
	b, _ := jsonstring.AppendQuoted(jsonstring.TextEscaper(false, false), nil, name)
	return string(b)
}

func isLetterOrDigit(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r)
}

// FoldName returns the name of which the names that case:ignore matches are the same: the case is folded, and '_'
// and '-' are removed.
func FoldName(name []byte) []byte {
	out := make([]byte, 0, len(name))
	for i := 0; i < len(name); {
		if c := name[i]; c < utf8.RuneSelf {
			if c != '_' && c != '-' {
				if 'a' <= c && c <= 'z' {
					c -= 'a' - 'A'
				}
				out = append(out, c)
			}
			i++
			continue
		}
		r, n := utf8.DecodeRune(name[i:])
		out = utf8.AppendRune(out, foldRune(r))
		i += n
	}
	return out
}

// foldRune returns the smallest rune of the runes which r folds to: the same one for all of them.
func foldRune(r rune) rune {
	smallest := r
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		smallest = min(smallest, f)
	}
	return smallest
}
