// Copyright 2011-2019 Canonical Ltd
// Copyright 2025 The go-yaml Project Contributors
// SPDX-License-Identifier: Apache-2.0

// Representer stage: Converts Go values to YAML nodes.
// Handles marshaling from Go types to the intermediate node representation.

package libyaml

import (
	"encoding"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type keyList []reflect.Value

func (l keyList) Len() int      { return len(l) }
func (l keyList) Swap(i, j int) { l[i], l[j] = l[j], l[i] }
func (l keyList) Less(i, j int) bool {
	a := l[i]
	b := l[j]
	ak := a.Kind()
	bk := b.Kind()
	for (ak == reflect.Interface || ak == reflect.Pointer) && !a.IsNil() {
		a = a.Elem()
		ak = a.Kind()
	}
	for (bk == reflect.Interface || bk == reflect.Pointer) && !b.IsNil() {
		b = b.Elem()
		bk = b.Kind()
	}
	af, aok := keyFloat(a)
	bf, bok := keyFloat(b)
	if aok && bok {
		if af != bf {
			return af < bf
		}
		if ak != bk {
			return ak < bk
		}
		return numLess(a, b)
	}
	if ak != reflect.String || bk != reflect.String {
		return ak < bk
	}
	ar, br := []rune(a.String()), []rune(b.String())
	digits := false
	for i := 0; i < len(ar) && i < len(br); i++ {
		if ar[i] == br[i] {
			digits = unicode.IsDigit(ar[i])
			continue
		}
		al := unicode.IsLetter(ar[i])
		bl := unicode.IsLetter(br[i])
		if al && bl {
			return ar[i] < br[i]
		}
		if al || bl {
			if digits {
				return al
			} else {
				return bl
			}
		}
		var ai, bi int
		var an, bn int64
		if ar[i] == '0' || br[i] == '0' {
			for j := i - 1; j >= 0 && unicode.IsDigit(ar[j]); j-- {
				if ar[j] != '0' {
					an = 1
					bn = 1
					break
				}
			}
		}
		for ai = i; ai < len(ar) && unicode.IsDigit(ar[ai]); ai++ {
			an = an*10 + int64(ar[ai]-'0')
		}
		for bi = i; bi < len(br) && unicode.IsDigit(br[bi]); bi++ {
			bn = bn*10 + int64(br[bi]-'0')
		}
		if an != bn {
			return an < bn
		}
		if ai != bi {
			return ai < bi
		}
		return ar[i] < br[i]
	}
	return len(ar) < len(br)
}

// keyFloat returns a float value for v if it is a number/bool
// and whether it is a number/bool or not.
func keyFloat(v reflect.Value) (f float64, ok bool) {
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(v.Int()), true
	case reflect.Float32, reflect.Float64:
		return v.Float(), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return float64(v.Uint()), true
	case reflect.Bool:
		if v.Bool() {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// numLess returns whether a < b.
// a and b must necessarily have the same kind.
func numLess(a, b reflect.Value) bool {
	switch a.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return a.Int() < b.Int()
	case reflect.Float32, reflect.Float64:
		return a.Float() < b.Float()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return a.Uint() < b.Uint()
	case reflect.Bool:
		return !a.Bool() && b.Bool()
	}
	panic("not a number")
}

// Sentinel values for newRepresenter parameters.
// These provide clarity at call sites, similar to http.NoBody.
var (
	noWriter           io.Writer         = nil
	noVersionDirective *VersionDirective = nil
	noTagDirective     []TagDirective    = nil
)

type Representer struct {
	Emitter               Emitter
	Out                   []byte
	flow                  bool
	Indent                int
	lineWidth             int
	doneInit              bool
	explicitStart         bool
	explicitEnd           bool
	flowSimpleCollections bool
	quotePreference       QuoteStyle
}

// NewRepresenter creates a new YAML representr with the given options.
//
// The writer parameter specifies the output destination for the representr.
// If writer is nil, the representr will write to an internal buffer.
func NewRepresenter(writer io.Writer, opts *Options) *Representer {
	emitter := NewEmitter()
	emitter.CompactSequenceIndent = opts.CompactSeqIndent
	emitter.quotePreference = opts.QuotePreference
	emitter.SetWidth(opts.LineWidth)
	emitter.SetUnicode(opts.Unicode)
	emitter.SetCanonical(opts.Canonical)
	emitter.SetLineBreak(opts.LineBreak)

	r := &Representer{
		Emitter:               emitter,
		Indent:                opts.Indent,
		lineWidth:             opts.LineWidth,
		explicitStart:         opts.ExplicitStart,
		explicitEnd:           opts.ExplicitEnd,
		flowSimpleCollections: opts.FlowSimpleCollections,
		quotePreference:       opts.QuotePreference,
	}

	if writer != nil {
		r.Emitter.SetOutputWriter(writer)
	} else {
		r.Emitter.SetOutputString(&r.Out)
	}

	return r
}

func (r *Representer) init() {
	if r.doneInit {
		return
	}
	if r.Indent == 0 {
		r.Indent = 4
	}
	r.Emitter.BestIndent = r.Indent
	r.emit(NewStreamStartEvent(UTF8_ENCODING))
	r.doneInit = true
}

func (r *Representer) Finish() {
	r.Emitter.OpenEnded = false
	r.emit(NewStreamEndEvent())
}

func (r *Representer) Destroy() {
	r.Emitter.Delete()
}

func (r *Representer) emit(event Event) {
	// This will internally delete the event value.
	r.must(r.Emitter.Emit(&event))
}

func (r *Representer) must(err error) {
	if err != nil {
		msg := err.Error()
		if msg == "" {
			msg = "unknown problem generating YAML content"
		}
		failf("%s", msg)
	}
}

func (r *Representer) MarshalDoc(tag string, in reflect.Value) {
	r.init()
	var node *Node
	if in.IsValid() {
		node, _ = in.Interface().(*Node)
	}
	if node != nil && node.Kind == DocumentNode {
		r.nodev(in)
	} else {
		// Use !explicitStart for implicit flag (true = implicit/no marker)
		r.emit(NewDocumentStartEvent(noVersionDirective, noTagDirective, !r.explicitStart))
		r.marshal(tag, in)
		// Use !explicitEnd for implicit flag
		r.emit(NewDocumentEndEvent(!r.explicitEnd))
	}
}

func (r *Representer) marshal(tag string, in reflect.Value) {
	tag = shortTag(tag)
	if !in.IsValid() || in.Kind() == reflect.Pointer && in.IsNil() {
		r.nilv()
		return
	}
	iface := in.Interface()
	switch value := iface.(type) {
	case *Node:
		r.nodev(in)
		return
	case Node:
		if !in.CanAddr() {
			n := reflect.New(in.Type()).Elem()
			n.Set(in)
			in = n
		}
		r.nodev(in.Addr())
		return
	case time.Time:
		r.timev(tag, in)
		return
	case *time.Time:
		r.timev(tag, in.Elem())
		return
	case time.Duration:
		r.stringv(tag, reflect.ValueOf(value.String()))
		return
	case Marshaler:
		v, err := value.MarshalYAML()
		if err != nil {
			Fail(err)
		}
		if v == nil {
			r.nilv()
			return
		}
		r.marshal(tag, reflect.ValueOf(v))
		return
	case encoding.TextMarshaler:
		text, err := value.MarshalText()
		if err != nil {
			Fail(err)
		}
		in = reflect.ValueOf(string(text))
	case nil:
		r.nilv()
		return
	}
	switch in.Kind() {
	case reflect.Interface:
		r.marshal(tag, in.Elem())
	case reflect.Map:
		r.mapv(tag, in)
	case reflect.Pointer:
		r.marshal(tag, in.Elem())
	case reflect.Struct:
		r.structv(tag, in)
	case reflect.Slice, reflect.Array:
		r.slicev(tag, in)
	case reflect.String:
		r.stringv(tag, in)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		r.intv(tag, in)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		r.uintv(tag, in)
	case reflect.Float32, reflect.Float64:
		r.floatv(tag, in)
	case reflect.Bool:
		r.boolv(tag, in)
	default:
		panic("cannot marshal type: " + in.Type().String())
	}
}

func (r *Representer) mapv(tag string, in reflect.Value) {
	r.mappingv(tag, func() {
		keys := keyList(in.MapKeys())
		sort.Sort(keys)
		for _, k := range keys {
			r.marshal("", k)
			r.marshal("", in.MapIndex(k))
		}
	})
}

func (r *Representer) fieldByIndex(v reflect.Value, index []int) (field reflect.Value) {
	for _, num := range index {
		for {
			if v.Kind() == reflect.Pointer {
				if v.IsNil() {
					return reflect.Value{}
				}
				v = v.Elem()
				continue
			}
			break
		}
		v = v.Field(num)
	}
	return v
}

func (r *Representer) structv(tag string, in reflect.Value) {
	sinfo, err := getStructInfo(in.Type())
	if err != nil {
		panic(err)
	}
	r.mappingv(tag, func() {
		for _, info := range sinfo.FieldsList {
			var value reflect.Value
			if info.Inline == nil {
				value = in.Field(info.Num)
			} else {
				value = r.fieldByIndex(in, info.Inline)
				if !value.IsValid() {
					continue
				}
			}
			if info.OmitEmpty && isZero(value) {
				continue
			}
			r.marshal("", reflect.ValueOf(info.Key))
			r.flow = info.Flow
			r.marshal("", value)
		}
		if sinfo.InlineMap >= 0 {
			m := in.Field(sinfo.InlineMap)
			if m.Len() > 0 {
				r.flow = false
				keys := keyList(m.MapKeys())
				sort.Sort(keys)
				for _, k := range keys {
					if _, found := sinfo.FieldsMap[k.String()]; found {
						panic(fmt.Sprintf("cannot have key %q in inlined map: conflicts with struct field", k.String()))
					}
					r.marshal("", k)
					r.flow = false
					r.marshal("", m.MapIndex(k))
				}
			}
		}
	})
}

func (r *Representer) mappingv(tag string, f func()) {
	implicit := tag == ""
	style := BLOCK_MAPPING_STYLE
	if r.flow {
		r.flow = false
		style = FLOW_MAPPING_STYLE
	}
	r.emit(NewMappingStartEvent(nil, []byte(tag), implicit, style))
	f()
	r.emit(NewMappingEndEvent())
}

func (r *Representer) slicev(tag string, in reflect.Value) {
	implicit := tag == ""
	style := BLOCK_SEQUENCE_STYLE
	if r.flow {
		r.flow = false
		style = FLOW_SEQUENCE_STYLE
	}
	r.emit(NewSequenceStartEvent(nil, []byte(tag), implicit, style))
	n := in.Len()
	for i := 0; i < n; i++ {
		r.marshal("", in.Index(i))
	}
	r.emit(NewSequenceEndEvent())
}

// isBase60 returns whether s is in base 60 notation as defined in YAML 1.1.
//
// The base 60 float notation in YAML 1.1 is a terrible idea and is unsupported
// in YAML 1.2 and by this package, but these should be marshaled quoted for
// the time being for compatibility with other parsers.
func isBase60Float(s string) (result bool) {
	// Fast path.
	if s == "" {
		return false
	}
	c := s[0]
	if !(c == '+' || c == '-' || c >= '0' && c <= '9') || strings.IndexByte(s, ':') < 0 {
		return false
	}
	// Do the full match.
	return base60float.MatchString(s)
}

// From http://yaml.org/type/float.html, except the regular expression there
// is bogus. In practice parsers do not enforce the "\.[0-9_]*" suffix.
var base60float = regexp.MustCompile(`^[-+]?[0-9][0-9_]*(?::[0-5]?[0-9])+(?:\.[0-9_]*)?$`)

// isOldBool returns whether s is bool notation as defined in YAML 1.1.
//
// We continue to force strings that YAML 1.1 would interpret as booleans to be
// rendered as quotes strings so that the marshaled output valid for YAML 1.1
// parsing.
func isOldBool(s string) (result bool) {
	switch s {
	case "y", "Y", "yes", "Yes", "YES", "on", "On", "ON",
		"n", "N", "no", "No", "NO", "off", "Off", "OFF":
		return true
	default:
		return false
	}
}

// looksLikeMerge returns true if the given string is the merge indicator "<<".
//
// When encoding a scalar with this exact value, it must be quoted to prevent it
// from being interpreted as a merge indicator during decoding.
func looksLikeMerge(s string) (result bool) {
	return s == "<<"
}

func (r *Representer) stringv(tag string, in reflect.Value) {
	var style ScalarStyle
	s := in.String()
	canUsePlain := true
	switch {
	case !utf8.ValidString(s):
		if tag == binaryTag {
			failf("explicitly tagged !!binary data must be base64-encoded")
		}
		if tag != "" {
			failf("cannot marshal invalid UTF-8 data as %s", shortTag(tag))
		}
		// It can't be represented directly as YAML so use a binary tag
		// and represent it as base64.
		tag = binaryTag
		s = encodeBase64(s)
	case tag == "":
		// Check to see if it would resolve to a specific
		// tag when represented unquoted. If it doesn't,
		// there's no need to quote it.
		rtag, _ := resolve("", s)
		canUsePlain = rtag == strTag &&
			!(isBase60Float(s) ||
				isOldBool(s) ||
				looksLikeMerge(s))
	}
	// Note: it's possible for user code to emit invalid YAML
	// if they explicitly specify a tag and a string containing
	// text that's incompatible with that tag.
	switch {
	case strings.Contains(s, "\n"):
		if r.flow || !shouldUseLiteralStyle(s) {
			style = DOUBLE_QUOTED_SCALAR_STYLE
		} else {
			style = LITERAL_SCALAR_STYLE
		}
	case canUsePlain:
		style = PLAIN_SCALAR_STYLE
	default:
		style = r.quotePreference.ScalarStyle()
	}
	r.emitScalar(s, "", tag, style, nil, nil, nil, nil)
}

func (r *Representer) boolv(tag string, in reflect.Value) {
	var s string
	if in.Bool() {
		s = "true"
	} else {
		s = "false"
	}
	r.emitScalar(s, "", tag, PLAIN_SCALAR_STYLE, nil, nil, nil, nil)
}

func (r *Representer) intv(tag string, in reflect.Value) {
	s := strconv.FormatInt(in.Int(), 10)
	r.emitScalar(s, "", tag, PLAIN_SCALAR_STYLE, nil, nil, nil, nil)
}

func (r *Representer) uintv(tag string, in reflect.Value) {
	s := strconv.FormatUint(in.Uint(), 10)
	r.emitScalar(s, "", tag, PLAIN_SCALAR_STYLE, nil, nil, nil, nil)
}

func (r *Representer) timev(tag string, in reflect.Value) {
	t := in.Interface().(time.Time)
	s := t.Format(time.RFC3339Nano)
	r.emitScalar(s, "", tag, PLAIN_SCALAR_STYLE, nil, nil, nil, nil)
}

func (r *Representer) floatv(tag string, in reflect.Value) {
	// Issue #352: When formatting, use the precision of the underlying value
	precision := 64
	if in.Kind() == reflect.Float32 {
		precision = 32
	}

	s := strconv.FormatFloat(in.Float(), 'g', -1, precision)
	switch s {
	case "+Inf":
		s = ".inf"
	case "-Inf":
		s = "-.inf"
	case "NaN":
		s = ".nan"
	}
	r.emitScalar(s, "", tag, PLAIN_SCALAR_STYLE, nil, nil, nil, nil)
}

func (r *Representer) nilv() {
	r.emitScalar("null", "", "", PLAIN_SCALAR_STYLE, nil, nil, nil, nil)
}

func (r *Representer) emitScalar(
	value, anchor, tag string, style ScalarStyle, head, line, foot, tail []byte,
) {
	// TODO Kill this function. Replace all initialize calls by their underlining Go literals.
	implicit := tag == ""
	if !implicit {
		tag = longTag(tag)
	}
	event := NewScalarEvent([]byte(anchor), []byte(tag), []byte(value), implicit, implicit, style)
	event.HeadComment = head
	event.LineComment = line
	event.FootComment = foot
	event.TailComment = tail
	r.emit(event)
}

func (r *Representer) nodev(in reflect.Value) {
	r.node(in.Interface().(*Node), "")
}
