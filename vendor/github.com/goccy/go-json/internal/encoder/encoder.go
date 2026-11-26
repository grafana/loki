package encoder

import (
	"bytes"
	"encoding"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"math"
	"math/bits"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"

	"github.com/goccy/go-json/internal/errors"
	"github.com/goccy/go-json/internal/floatfmt"
	"github.com/goccy/go-json/internal/jsonnum"
	"github.com/goccy/go-json/internal/jsonstring"
	"github.com/goccy/go-json/internal/runtime"
)

func (t OpType) IsMultipleOpField() bool {
	switch t {
	case OpStructField:
		return true
	case OpStructFieldSlice:
		return true
	case OpStructFieldArray:
		return true
	case OpStructFieldMap:
		return true
	case OpStructFieldOmitEmpty:
		return true
	case OpStructFieldOmitEmptySlice:
		return true
	case OpStructFieldOmitEmptyArray:
		return true
	case OpStructFieldOmitEmptyMap:
		return true
	case OpStructFieldSlicePtr:
		return true
	case OpStructFieldOmitEmptySlicePtr:
		return true
	case OpStructFieldArrayPtr:
		return true
	case OpStructFieldOmitEmptyArrayPtr:
		return true
	case OpStructFieldMapPtr:
		return true
	}
	return false
}

type OpcodeSet struct {
	Type reflect.Type
	// IfaceIndir is whether a value of Type is stored indirectly in an interface value.
	// It is decided when the type is compiled, because deciding it is not cheap.
	IfaceIndir bool
	// DataWordIsAddr is whether the data word of an interface value of Type is the address of the value
	// which the opcodes take: the type is stored indirectly, or it is a pointer, which is the address of
	// the value it points to.
	DataWordIsAddr bool
	// Levels is how many levels deeper than the place of the value its opcodes go in their frame ( see
	// deepestLevel ).
	Levels uint32
	// KeyCodes and InterfaceKeyCodes are the opcodes of the value, and of the value held by an interface value, for
	// each escape of the names of the fields, indexed by the options of KeyEscapeOptions.
	KeyCodes          [4]*Opcode
	InterfaceKeyCodes [4]*Opcode
	CodeLength        int
	EndCode           *Opcode
	// Scalar is the opcode of the value if the type is encoded by a single opcode of a scalar ( a number,
	// a string, a bool, ... ), or nil. Such a value held by an interface value is encoded without a frame.
	Scalar *Opcode
	Code   Code
	// queryCache is the opcodes of the field queries of the type, by the hash of the query, which every goroutine
	// reads without a lock ( see getFilteredCodeSetIfNeeded ).
	queryCache sync.Map
	// values is the pool of the values of Type in the heap, which MarshalOf copies its argument to.
	values sync.Pool
	// pointee is the opcodes of the value which a pointer of Type points to, or noPointee ( see PointeeOf ).
	pointee atomic.Pointer[OpcodeSet]
}

// noPointee is the pointee of the opcodes of a type whose values are not encoded as the values they point to.
var noPointee = &OpcodeSet{}

// PointeeOf returns the opcodes of the value which a pointer of the type of codeSet points to, if it is stored
// indirectly in an interface value, or nil. With functions of marshaling, encoding/json/v2 encodes a pointer at the
// top level as the value it points to, addressable, so that the functions are given the value, or the pointer for
// the functions of pointers, not the address of the pointer: the pointer, the data word, is the address of the
// value for its opcodes. A value stored directly, as a pointer or a map, is encoded by the opcodes of the pointer,
// which give the functions of the pointer to it the address of it, as encoding/json/v2 does.
func PointeeOf(ctx *RuntimeContext, codeSet *OpcodeSet) *OpcodeSet {
	pointee := codeSet.pointee.Load()
	if pointee == nil {
		pointee = noPointee
		if t := codeSet.Type; t.Kind() == reflect.Pointer && runtime.IfaceIndir(t.Elem()) {
			if elem, err := pointeeCodeSet(ctx, t.Elem()); err == nil {
				pointee = elem
			}
		}
		codeSet.pointee.Store(pointee)
	}
	if pointee == noPointee {
		return nil
	}
	return pointee
}

// ValueShape classifies a type by what a nil data word of an interface value of the type means.
type ValueShape uint

const (
	// ValueShapePointer is the type whose nil data word is encoded as null: a pointer, a map, ...
	ValueShapePointer ValueShape = iota
	// ValueShapeAggregate is the type whose nil data word is a value to encode, if the type is stored directly
	// in an interface value:
	//   - a struct or an array which consists of a single pointer. The pointer is nil, not the value.
	//   - a map which has a marshaler. encoding/json writes null instead of calling the marshaler
	//     only for a nil pointer, so the marshaler of a nil map is called.
	//   - a channel, a function or an unsafe.Pointer, which has no JSON representation, nil or not: its
	//     compile fails, unless the type has a marshaler, which is called.
	ValueShapeAggregate
)

// ShapeOf returns the shape of the type given by the pointer to its type descriptor.
//
// ShapeOf and IfaceIndir are functions of their own, not a part of the VM, and the VM calls them in the
// same form as it did before, because any change of the code of the VM changes the register allocation
// of the whole VM.
//
//go:noinline
func ShapeOf(typ unsafe.Pointer) ValueShape {
	switch runtime.TypeOfPtr(typ).Kind() {
	case reflect.Struct, reflect.Array, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return ValueShapeAggregate
	case reflect.Map:
		// whether the type has a marshaler was decided when the type was compiled.
		codeSet, err := compileToGetUnfilteredCodeSet(uintptr(typ), 0)
		if err != nil {
			// the VM compiles the type right after this, and it reports the error.
			return ValueShapeAggregate
		}
		switch codeSet.Code.Kind() {
		case CodeKindMarshalJSON, CodeKindMarshalText:
			return ValueShapeAggregate
		}
	}
	return ValueShapePointer
}

// IfaceIndir reports whether a value of the type is stored indirectly in an interface value.
// It is decided only once per type, when the type is compiled.
//
//go:noinline
func IfaceIndir(typ unsafe.Pointer) bool {
	codeSet, err := compileToGetUnfilteredCodeSet(uintptr(typ), 0)
	if err != nil {
		// the VM compiles the type right after this, and it reports the error.
		return false
	}
	return codeSet.IfaceIndir
}

// TakeValue returns the address of a zero value of Type in the heap.
//
// The runtime context keeps the value of the type it encoded last, because taking one from a sync.Pool costs
// as much as a small allocation, and a runtime context is already taken from a pool.
func (s *OpcodeSet) TakeValue(ctx *RuntimeContext) unsafe.Pointer {
	if ctx.valueCodeSet == s {
		return ctx.value
	}
	if ctx.valueCodeSet != nil {
		ctx.valueCodeSet.values.Put(ctx.value)
	}
	ctx.valueCodeSet = s
	if p := s.values.Get(); p != nil {
		ctx.value = p.(unsafe.Pointer)
	} else {
		ctx.value = reflect.New(s.Type).UnsafePointer()
	}
	return ctx.value
}

func (s *OpcodeSet) getQueryCache(hash string) *OpcodeSet {
	if codeSet, ok := s.queryCache.Load(hash); ok {
		return codeSet.(*OpcodeSet)
	}
	return nil
}

func (s *OpcodeSet) setQueryCache(hash string, codeSet *OpcodeSet) {
	s.queryCache.Store(hash, codeSet)
}

type CompiledCode struct {
	Code    *Opcode
	CurLen  uintptr
	NextLen uintptr
	// Levels is the deepest level which Code goes to in its frame ( see deepestLevel ).
	Levels uint32
	// Embedded is whether the recursive struct is embedded in the struct which jumps to it.
	// The code to jump to is only the fields of the struct then: it has neither the braces nor the check of nil.
	Embedded bool
}

// KeyEscapeOptions are the options by which the names of the fields are escaped, whose bits are the index of the
// opcodes of each escape ( see OpcodeSet.KeyCodes ): for HTML, and for JavaScript, U+2028 and U+2029.
const KeyEscapeOptions = HTMLEscapeOption | NormalizeUTF8Option

var _ [0]struct{} = [KeyEscapeOptions - 3]struct{}{}

// MaxDepth is the deepest nesting of the objects and the arrays which encoding/json and encoding/json/v2 of Go 1.27
// take: 10000 levels. The VM checks it where a frame is entered, and where a value of a list is ( see
// TailRecursiveFlags ), by the indent of the frame and the deepest level which the opcodes of the frame go to ( see
// deepestLevel ). The encoder of a method or a function, and the default representation of a value it declines,
// count their levels after the depth of the value ( see RuntimeContext.ValueDepth ). A value which is null, omitted
// or empty, as a nil pointer or a nil or empty map or slice, is counted as the levels its type would open, and a raw
// value which a method or a function returns is checked by its own depth only ( see AppendRaw ): an output which is
// so deep there passes.
const MaxDepth = 10000

// ErrMaxDepth returns the error of a value nested deeper than MaxDepth, as encoding/json and encoding/json/v2 report
// it: an error of the syntax of the output.
func ErrMaxDepth(ctx *RuntimeContext) error {
	if ctx.Option.Flag&V2Option != 0 {
		return &errors.TextError{Err: errors.ErrMaxDepth}
	}
	return errors.ErrSyntax(errors.ErrMaxDepth.Error(), 0)
}

// StartDetectingCyclesAfter is the number of the frames ( see RuntimeContext.RecursiveLevel ) after which the
// values entered, and the maps, are recorded to detect a cycle.
//
// It is 998, not the 1000 of encoding/json, as it counts frames, not the depth of the JSON value: the top-level value
// is written in the frame of the caller, and the check is made before the frame of a value is entered, so the
// level is the depth minus 1 there. With 998, the first value recorded is at the depth 1000 and a cycle is found at
// the depth 1001, where encoding/json/v2 reports it: its byte offset and JSON pointer are the same.
const StartDetectingCyclesAfter = 998

// StartDetectingMapCyclesAfter is StartDetectingCyclesAfter for a map ( see RuntimeContext.RecordMap ), whose check
// is made in the frame of its value, one level past the check of a frame, which is made before it is entered.
const StartDetectingMapCyclesAfter = StartDetectingCyclesAfter + 1

// ErrUnsupportedValue returns the error of a cycle through the value at ptr, of the type of code: of the pointer
// to it for the opcode of a pointer.
func ErrUnsupportedValue(ctx *RuntimeContext, code *Opcode, ptr unsafe.Pointer) error {
	typ := runtime.TypeOfPtr(code.Type)
	if code.Op == OpRecursivePtr && ctx.Option.Flag&V2Option != 0 {
		return &errors.SemanticError{GoType: reflect.PointerTo(typ), Err: errors.ErrCycle}
	}
	return errCycle(ctx, typ, ptr)
}

// ErrNilMapKey returns the error of a key of a map which is a nil pointer, the operand of code, which has no name
// ( see MapKeyFlags ).
func ErrNilMapKey(code *Opcode) error {
	return &errors.UnsupportedValueError{Str: fmt.Sprintf("a nil key of %s", reflect.PointerTo(runtime.TypeOfPtr(code.Type)))}
}

// errCycle returns the error of a cycle through the value at p, of the type typ.
func errCycle(ctx *RuntimeContext, typ reflect.Type, p unsafe.Pointer) error {
	if ctx.Option.Flag&V2Option != 0 {
		return &errors.SemanticError{GoType: typ, Err: errors.ErrCycle}
	}
	return &errors.UnsupportedValueError{
		Value: reflect.NewAt(typ, p).Elem(),
		Str:   fmt.Sprintf("encountered a cycle via %s", typ),
	}
}

// ErrUnsupportedFloat returns the error of a float which is not finite, the value of the opcode: of encoding/json, with
// the float, for v1 and for ReportErrorsWithLegacySemantics.
func ErrUnsupportedFloat(ctx *RuntimeContext, code *Opcode, v float64) error {
	if ctx.Option.Flag&(V2Option|ReportErrorsWithLegacySemanticsOption) == V2Option {
		return &errors.SemanticError{GoType: runtime.TypeOfPtr(code.Type), Err: fmt.Errorf("unsupported value: %v", v)}
	}
	// the value is of the type of the float, which pointers may lead to, as encoding/json gives it
	value := reflect.ValueOf(v)
	typ := runtime.TypeOfPtr(code.Type)
	for typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	if typ.Kind() == reflect.Float32 || typ.Kind() == reflect.Float64 {
		value = reflect.New(typ).Elem()
		value.SetFloat(v)
	}
	return &errors.UnsupportedValueError{
		Value: value,
		Str:   strconv.FormatFloat(v, 'g', -1, 64),
	}
}

type emptyInterface struct {
	typ unsafe.Pointer
	ptr unsafe.Pointer
}

// MapItem is a key of a sorted map whose keys are not of a string kind, as it is encoded with a comma after it, and
// the index of its entry in the MapContext. While the keys are encoded, Key is the keys from the first one up to the
// end of this one, in the output, which tells where it ends ( see MapContext.SortEncodedKeys ).
type MapItem struct {
	Key   []byte
	Entry int
}

type Mapslice struct {
	Items []MapItem
}

// insertionSortMapItems sorts the items by their keys without the last tail bytes of each: the quote which ends
// the name, and what follows it. It reports whether two keys have the same name, which are put in order after
// ( see sortTiedKeys ).
func insertionSortMapItems(items []MapItem, tail int) bool {
	tied := false
	for i := 1; i < len(items); i++ {
		if r := bytes.Compare(items[i-1].Key[:len(items[i-1].Key)-tail], items[i].Key[:len(items[i].Key)-tail]); r <= 0 {
			if r == 0 {
				tied = true
			}
			continue
		}
		item := items[i]
		name := item.Key[:len(item.Key)-tail]
		j := i
		for ; j > 0; j-- {
			if r := bytes.Compare(items[j-1].Key[:len(items[j-1].Key)-tail], name); r <= 0 {
				if r == 0 {
					tied = true
				}
				break
			}
			items[j] = items[j-1]
		}
		items[j] = item
	}
	return tied
}

// sortTiedKeys puts the items of the same name, which are next to each other, in the order of their Go keys
// ( see compareTiedKeys ). tail is the length of what follows the names of the keys.
func (c *MapContext) sortTiedKeys(items []MapItem, tail int) {
	for i := 1; i < len(items); i++ {
		item := items[i]
		name := item.Key[:len(item.Key)-tail]
		j := i
		for ; j > 0 && compareKeyNames(items[j-1].Key[:len(items[j-1].Key)-tail], name) == 0 && c.compareTiedKeys(items[j-1], item) > 0; j-- {
			items[j] = items[j-1]
		}
		items[j] = item
	}
}

// insertionSortMapItemsByNames is insertionSortMapItems for the keys of a map of which one has an escape
// ( see compareKeyNames ).
func insertionSortMapItemsByNames(items []MapItem, tail int) bool {
	tied := false
	for i := 1; i < len(items); i++ {
		item := items[i]
		name := item.Key[:len(item.Key)-tail]
		j := i
		for ; j > 0; j-- {
			if r := compareKeyNames(items[j-1].Key[:len(items[j-1].Key)-tail], name); r <= 0 {
				if r == 0 {
					tied = true
				}
				break
			}
			items[j] = items[j-1]
		}
		items[j] = item
	}
	return tied
}

// compareTiedKeys compares the entries of two keys of the same name, which the keys of the same text have, or of
// texts which have invalid UTF-8, replaced in their names: by their Go keys, which the context has, so that a map
// is encoded the same way every time. encoding/json leaves their order to the order of the map. A key of a string
// kind is compared by its string, and another one by its bytes, which are the same for the same map.
func (c *MapContext) compareTiedKeys(a, b MapItem) int {
	if c.layout.StringKey {
		return strings.Compare(c.Keys[a.Entry], c.Keys[b.Entry])
	}
	n := int(c.layout.keySize)
	return bytes.Compare(c.RawKeys[a.Entry*n:a.Entry*n+n], c.RawKeys[b.Entry*n:b.Entry*n+n])
}

// compareKeyNames compares the names of two encoded keys of a map, without the quotes which end them: by their
// bytes, which are in the order of the names up to the first one which differs, unless it is in an escape, whose
// character is not in the order of the escape. The names are decoded as they are compared then
// ( see compareEncodedNames ). The codes of a color before the names are the same for every key.
func compareKeyNames(a, b []byte) int {
	n := min(len(a), len(b))
	k := 0
	// the bytes which are the same, eight at a time: the first one which differs is the lowest of the word, as it
	// is loaded.
	for ; k+8 <= n; k += 8 {
		if x := binary.LittleEndian.Uint64(a[k:]) ^ binary.LittleEndian.Uint64(b[k:]); x != 0 {
			k += bits.TrailingZeros64(x) / 8
			break
		}
	}
	for k < n && a[k] == b[k] {
		k++
	}
	if k == n {
		// a name which the other one begins with: the other one has more characters.
		return len(a) - len(b)
	}
	// an escape at the byte, or before it, which the byte may be in: the bytes before it are the same in a and b.
	escaped := a[k] == '\\' || b[k] == '\\'
	for i := max(k-5, 0); i < k && !escaped; i++ {
		escaped = a[i] == '\\'
	}
	if escaped {
		return compareEncodedNames(a, b)
	}
	return int(a[k]) - int(b[k])
}

// compareEncodedNames compares the names of two encoded keys, the strings after their first quotes, whose escapes
// are the ones AppendString writes, decoded as they are compared. What precedes the quote, as the codes of a
// color, is not of the name.
func compareEncodedNames(a, b []byte) int {
	a, b = a[bytes.IndexByte(a, '"')+1:], b[bytes.IndexByte(b, '"')+1:]
	// ca and cb are the bytes of the characters being compared which are left: of the input, or of the
	// character an escape is of, in ra or rb.
	var ra, rb [utf8.UTFMax]byte
	var ca, cb []byte
	for {
		if len(ca) == 0 {
			if len(a) == 0 {
				if len(cb) == 0 && len(b) == 0 {
					return 0
				}
				return -1
			}
			ca, a = nextNameChar(a, &ra)
		}
		if len(cb) == 0 {
			if len(b) == 0 {
				return +1
			}
			cb, b = nextNameChar(b, &rb)
		}
		if ca[0] != cb[0] {
			return int(ca[0]) - int(cb[0])
		}
		ca, cb = ca[1:], cb[1:]
	}
}

// nextNameChar returns the bytes of the first character of the encoded name s, decoded into buf if it is an escape,
// and the rest of s. The escapes of the encoder are valid.
func nextNameChar(s []byte, buf *[utf8.UTFMax]byte) ([]byte, []byte) {
	if s[0] != '\\' || len(s) == 1 {
		return s[:1], s[1:]
	}
	switch e := s[1]; e {
	case 'u':
		if len(s) < 6 {
			return s[:1], s[1:]
		}
		r := hexRune(s[2:6])
		if utf16.IsSurrogate(r) && len(s) >= 12 && s[6] == '\\' && s[7] == 'u' {
			// a character out of the basic plane, as a pair of surrogates.
			if r := utf16.DecodeRune(r, hexRune(s[8:12])); r != utf8.RuneError {
				return buf[:utf8.EncodeRune(buf[:], r)], s[12:]
			}
		}
		return buf[:utf8.EncodeRune(buf[:], r)], s[6:]
	case 'n':
		buf[0] = '\n'
	case 'r':
		buf[0] = '\r'
	case 't':
		buf[0] = '\t'
	case 'b':
		buf[0] = '\b'
	case 'f':
		buf[0] = '\f'
	default:
		buf[0] = e
	}
	return buf[:1], s[2:]
}

// maxItemsOfInsertionSort is the number of the items up to which the insertion sort is used.
// With keys which are random in their content and in their length, it is faster than slices.SortFunc up to
// 24 items and slower from 32 items ( BenchmarkVariant_MapSort ).
const maxItemsOfInsertionSort = 16

// MapContext is what the VM encodes a map from: the entries read from the map ( MapLayout.Collect ), and
// the state of the entry being encoded.
type MapContext struct {
	layout *MapLayout
	// Keys are the keys of the entries, for keys of a string kind; RawKeys are the bytes of the keys of another
	// kind, keySize each; Values are the bytes of the values, valueSize each. The copies of the values may hold
	// pointers which the GC doesn't see, as the slots of the VM do: the map holds the values while they are
	// encoded.
	Keys    []string
	RawKeys []byte
	Values  []byte
	// Order is the entries sorted by their keys ( SortKeys ), for keys of a string kind; Sorted is whether
	// the entries are encoded in that order, or in the order of Slice, which is sorted when every key is encoded.
	Order    []int32
	prefixes []uint64 // the first bytes of the keys as numbers, while they are sorted
	Sorted   bool
	// DirectEntries is whether the entries of a sorted map are written directly by one call of the VM, up to one
	// whose value is left to its opcodes, as the entries of a map which is not sorted are written as it is read:
	// the values are written by one opcode of a scalar, or are of interface{} ( see MapLayout ).
	DirectEntries bool
	Len           int
	Idx           int
	// The keys of a sorted map whose keys are not of a string kind are encoded first, and the entries are written in
	// the order of their names after ( see SortEncodedKeys ): First is where the entries start in the buffer, Slice
	// has the keys and Buf is where they are copied.
	First int
	Slice *Mapslice
	items []MapItem // what Slice.Items is made of, kept between the maps
	Buf   []byte
	// what the collector by reflect.MapIter works with: here so that nothing is allocated for a map.
	iter       reflect.MapIter
	keyIface   any
	valueIface any
	// names are the names of the keys written, of a map whose keys may have the same name ( see
	// appendMapKeyName ), if namesKept is set: they are cleared for each map which needs them.
	names     map[string]struct{}
	namesKept bool
	// recorded is whether the map is recorded for the detection of cycles ( see RuntimeContext.RecordMap ).
	recorded bool
}

// NewMapContext returns the context to encode a map: the runtime context has one for each level of the maps
// nested in each other, kept from a call to the next, and refers to it while the VM has it in a slot, which
// the GC doesn't see.
func NewMapContext(rctx *RuntimeContext) *MapContext {
	if rctx.mapDepth == len(rctx.mapContexts) {
		rctx.mapContexts = append(rctx.mapContexts, &MapContext{Slice: &Mapslice{}})
	}
	ctx := rctx.mapContexts[rctx.mapDepth]
	rctx.mapDepth++
	ctx.Buf = ctx.Buf[:0]
	ctx.Idx = 0
	ctx.Sorted = false
	ctx.DirectEntries = false
	// Items is set by SortByEncodedKeys, and tells the VM the entries are put in that order.
	ctx.Slice.Items = nil
	ctx.namesKept, ctx.recorded = false, false
	return ctx
}

// RecordMap records the map at p of code, which the VM encodes past StartDetectingMapCyclesAfter, in m, for the
// detection of cycles, as encoding/json records a map: a cycle through the values of a map, which are copies, has
// the map alone as the value which it reaches again. The maps are recorded apart from the values of the frames,
// which a map is not compared with.
func (c *RuntimeContext) RecordMap(code *Opcode, p unsafe.Pointer, m *MapContext) error {
	if slices.Contains(c.seenMaps, p) {
		m := p // the map, whose address is taken here only, which keeps p from escaping
		return errCycle(c, runtime.TypeOfPtr(code.Type), unsafe.Pointer(&m))
	}
	c.seenMaps = append(c.seenMaps, p)
	m.recorded = true
	return nil
}

// ScalarValue is whether the values of the map are written by one opcode of a scalar ( MapLayout.ScalarValue ).
func (c *MapContext) ScalarValue() bool {
	return c.layout.ScalarValue
}

// SortByEncodedKeys makes the context write the entries in the order of their encoded keys, for a sorted map
// whose keys are not of a string kind: the VM encodes every key first and keeps it in Slice ( see OpMapValue ).
func (c *MapContext) SortByEncodedKeys() {
	if cap(c.items) < c.Len {
		c.items = make([]MapItem, c.Len)
	}
	c.Slice.Items = c.items[:c.Len]
}

// SortEncodedKeys sorts the keys of a sorted map whose keys are not of a string kind, which the VM has encoded one
// after another in b from First, each item with the keys up to the end of its own: they are moved to Buf, and the
// output is put back to First, where the entries are written in the order of the names.
//
// The keys are sorted by their names, as encoding/json sorts them. A key is its name between quotes, with the codes
// of a color around it, which are the same for every key: the keys are compared without the quote which ends the
// name and what follows it, as a space or a '!' comes before the quote, and without the codes of the color which
// end the name of an integer, which is colored within its quotes: footer is their length, which the VM finds from
// the opcode of the keys. When a key has an escape,
// which is not in the order of its character, the escapes are decoded where the names differ ( see
// compareKeyNames ). It is not sort.Sort, which
// calls Less and Swap through an interface for every comparison: the maps to encode are small in most cases, and
// the sort was a tenth of the time to encode one.
func (c *MapContext) SortEncodedKeys(rctx *RuntimeContext, b []byte, footer int) []byte {
	c.Buf = append(c.Buf[:0], b[c.First:]...)
	items := c.Slice.Items
	start := 0
	for i := range items {
		end := len(items[i].Key)
		items[i].Key = c.Buf[start:end:end]
		start = end
	}
	if len(items) == 1 {
		c.Sorted = true
		rctx.Rewrote(c.First)
		return b[:c.First]
	}
	first := items[0].Key
	tail := 1
	for first[len(first)-tail] != '"' {
		tail++
	}
	tail += footer
	switch escaped, many := bytes.IndexByte(c.Buf, '\\') >= 0, len(items) > maxItemsOfInsertionSort; {
	case escaped && many:
		// a key with an escape, which is not in the order of its character.
		slices.SortFunc(items, func(a, b MapItem) int {
			if r := compareKeyNames(a.Key[:len(a.Key)-tail], b.Key[:len(b.Key)-tail]); r != 0 {
				return r
			}
			return c.compareTiedKeys(a, b)
		})
	case escaped:
		if insertionSortMapItemsByNames(items, tail) {
			c.sortTiedKeys(items, tail)
		}
	case many:
		slices.SortFunc(items, func(a, b MapItem) int {
			if r := bytes.Compare(a.Key[:len(a.Key)-tail], b.Key[:len(b.Key)-tail]); r != 0 {
				return r
			}
			return c.compareTiedKeys(a, b)
		})
	default:
		if insertionSortMapItems(items, tail) {
			c.sortTiedKeys(items, tail)
		}
	}
	c.Sorted = true
	rctx.Rewrote(c.First)
	return b[:c.First]
}

// SortedKeysStart returns where the keys of the sorted map whose keys are being encoded start in the output, which
// are not entries yet ( see MapContext.SortEncodedKeys ): a method which writes a key sees the output before them.
func (c *RuntimeContext) SortedKeysStart() int {
	return c.mapContexts[c.mapDepth-1].First
}

// appendText appends the text of a marshaler or of the key of a map as a string.
func appendText(ctx *RuntimeContext, b []byte, text string) []byte {
	b, _ = jsonstring.AppendQuoted(StringEscaper(ctx), b, text)
	return b
}

func ReleaseMapContext(rctx *RuntimeContext, c *MapContext) {
	// a map is always released before the maps it is in.
	rctx.mapDepth--
	if c.recorded {
		// the last record: the ones of the maps of its values were dropped before
		rctx.seenMaps = rctx.seenMaps[:len(rctx.seenMaps)-1]
	}
	// the keys refer to the map, which the context must not keep alive.
	clear(c.Keys)
	c.Keys = c.Keys[:0]
}

func AppendByteSlice(ctx *RuntimeContext, b []byte, src []byte) []byte {
	if src == nil {
		if NilSliceIsEmpty(ctx) {
			return append(b, `""`...)
		}
		return append(b, `null`...)
	}
	encodedLen := base64.StdEncoding.EncodedLen(len(src))
	b = append(b, '"')
	pos := len(b)
	remainLen := cap(b[pos:])
	var buf []byte
	if remainLen > encodedLen {
		buf = b[pos : pos+encodedLen]
	} else {
		buf = make([]byte, encodedLen)
	}
	base64.StdEncoding.Encode(buf, src)
	return append(append(b, buf...), '"')
}

// AppendFloat32 appends v as encoding/json writes a float32: by the shortest decimal which rounds to it, in the
// format 'f', or 'e' below 1e-6 and from 1e21, whose exponent has no leading zero.
func AppendFloat32(_ *RuntimeContext, b []byte, v float32) []byte {
	return appendFloatOfBits(b, float64(v), 32)
}

// AppendFloat64 appends v as encoding/json writes a float64, as AppendFloat32 does.
func AppendFloat64(_ *RuntimeContext, b []byte, v float64) []byte {
	return appendFloatOfBits(b, v, 64)
}

// appendFloatOfBits appends the float of the precision bits, 64 or 32. NaN and the infinities, which the callers
// report as errors before, but a map key, are appended as strconv appends them.
func appendFloatOfBits(b []byte, f float64, bits int) []byte {
	const exponent = 0x7ff << 52
	if math.Float64bits(f)&exponent == exponent {
		return strconv.AppendFloat(b, f, 'g', -1, bits)
	}
	return floatfmt.AppendFloat(b, f, bits)
}

// AppendNumber appends n, which must be a JSON number by its grammar, as encoding/json writes a json.Number: an
// empty one as 0.
//
// A number of up to 24 bytes is copied by the words which overlap, without a call of memmove, and the bytes which
// are not digits are found in the words as they are loaded, which the grammar is then checked by, without a call:
// an integer at once, and another number by the places of its bytes which are not digits.
//
// A json.Number is a raw value for the options of encoding/json/v2 which rewrite raw values: they are checked here,
// in the function itself, as a call more costs every number.
func AppendNumber(ctx *RuntimeContext, b []byte, n json.Number) ([]byte, error) {
	if ctx.Option.Flag&RawRewriteOption != 0 {
		return appendRewrittenNumber(ctx, b, n)
	}
	l := len(n)
	if l == 0 {
		return append(b, '0'), nil
	}
	if l > 24 {
		if !jsonnum.IsValid(unsafe.Slice(unsafe.StringData(string(n)), l)) {
			return nil, invalidNumberError(n, false)
		}
		return append(b, n...), nil
	}
	if cap(b)-len(b) < l {
		b = growForNumber(b, l)
	}
	src := unsafe.Slice(unsafe.StringData(string(n)), l)
	dst := b[len(b) : len(b)+l]
	// nonDigits has the bit i of each byte i of n which is not a digit
	var nonDigits uint
	switch {
	case l > 16:
		first, mid, last := binary.LittleEndian.Uint64(src), binary.LittleEndian.Uint64(src[8:]), binary.LittleEndian.Uint64(src[l-8:])
		binary.LittleEndian.PutUint64(dst, first)
		binary.LittleEndian.PutUint64(dst[8:], mid)
		binary.LittleEndian.PutUint64(dst[l-8:], last)
		tf, tm, tl := jsonnum.NonDigitTops(first), jsonnum.NonDigitTops(mid), jsonnum.NonDigitTops(last)
		if tf|tm|tl == 0 && src[0] != '0' {
			return b[:len(b)+l], nil
		}
		nonDigits = uint(jsonnum.GatherTops(tf)) | uint(jsonnum.GatherTops(tm))<<8 | uint(jsonnum.GatherTops(tl))<<(l-8)
	case l >= 8:
		first, last := binary.LittleEndian.Uint64(src), binary.LittleEndian.Uint64(src[l-8:])
		binary.LittleEndian.PutUint64(dst, first)
		binary.LittleEndian.PutUint64(dst[l-8:], last)
		tf, tl := jsonnum.NonDigitTops(first), jsonnum.NonDigitTops(last)
		if tf|tl == 0 && src[0] != '0' {
			return b[:len(b)+l], nil
		}
		nonDigits = uint(jsonnum.GatherTops(tf)) | uint(jsonnum.GatherTops(tl))<<(l-8)
	case l >= 4:
		first, last := binary.LittleEndian.Uint32(src), binary.LittleEndian.Uint32(src[l-4:])
		binary.LittleEndian.PutUint32(dst, first)
		binary.LittleEndian.PutUint32(dst[l-4:], last)
		t := jsonnum.NonDigitTops(uint64(first) | uint64(last)<<32)
		if t == 0 && src[0] != '0' {
			return b[:len(b)+l], nil
		}
		m := uint(jsonnum.GatherTops(t))
		nonDigits = m&0xf | m>>4<<(l-4)
	default:
		for i := 0; i < l; i++ {
			c := src[i]
			dst[i] = c
			if c-'0' > 9 {
				nonDigits |= 1 << i
			}
		}
	}
	// a number without a sign or an exponent, the most common one, first
	if nonDigits&(nonDigits-1) == 0 {
		if nonDigits == 0 {
			if src[0] != '0' || l == 1 {
				return b[:len(b)+l], nil
			}
		} else if dot := bits.TrailingZeros(nonDigits); src[dot] == '.' && dot > 0 && dot < l-1 && (src[0] != '0' || dot == 1) {
			return b[:len(b)+l], nil
		}
	}
	// the others by the grammar in place: the bytes which are not digits are a sign, a decimal point, the exponent
	// and its sign, each at most once and in its place, whose bits are cleared one by one.
	start := 0
	if src[0] == '-' {
		nonDigits &^= 1
		start = 1
	}
	valid := false
	if start < l && nonDigits&(1<<start) == 0 && (src[start] != '0' || start+1 == l || nonDigits&(1<<(start+1)) != 0) {
		valid = nonDigits == 0
		p, fractionDigits := bits.TrailingZeros(nonDigits), true // digits after the decimal point, if there is one
		if !valid && src[p] == '.' {
			fractionDigits = p+1 < l && nonDigits&(1<<(p+1)) == 0
			nonDigits &^= 1 << p
			valid = fractionDigits && nonDigits == 0
			p = bits.TrailingZeros(nonDigits)
		}
		if !valid && fractionDigits && nonDigits != 0 && src[p]|0x20 == 'e' {
			nonDigits &^= 1 << p
			if p+1 < l && (src[p+1] == '+' || src[p+1] == '-') {
				nonDigits &^= 1 << (p + 1)
				p++
			}
			valid = p+1 < l && nonDigits == 0
		}
	}
	if !valid {
		return nil, invalidNumberError(n, false)
	}
	return b[:len(b)+l], nil
}

// AppendNumberString is AppendNumber of a json.Number of a field with the option string, which the caller
// quotes: its error shows the number quoted, as encoding/json shows it.
func AppendNumberString(ctx *RuntimeContext, b []byte, n json.Number) ([]byte, error) {
	if ctx.Option.Flag&RawRewriteOption != 0 {
		// a number in a string, which the options which rewrite raw values don't rewrite.
		return appendNumberAsIs(b, n)
	}
	out, err := AppendNumber(ctx, b, n)
	if err != nil {
		return nil, invalidNumberError(n, true)
	}
	return out, nil
}

// appendRewrittenNumber is AppendNumber of a json.Number which the options of encoding/json/v2 which rewrite raw
// values rewrite, as it is a raw value for them.
//
//go:noinline
func appendRewrittenNumber(ctx *RuntimeContext, b []byte, n json.Number) ([]byte, error) {
	if n == "" {
		n = "0"
	}
	raw := unsafe.Slice(unsafe.StringData(string(n)), len(n))
	if !jsonnum.IsValid(raw) {
		return nil, invalidNumberError(n, false)
	}
	return V2Hooks.FormatRaw(ctx, b, raw)
}

// appendNumberAsIs is AppendNumberString of a number which the options which rewrite raw values don't rewrite.
//
//go:noinline
func appendNumberAsIs(b []byte, n json.Number) ([]byte, error) {
	if n == "" {
		return append(b, '0'), nil
	}
	if !jsonnum.IsValid(unsafe.Slice(unsafe.StringData(string(n)), len(n))) {
		return nil, invalidNumberError(n, true)
	}
	return append(b, n...), nil
}

// growForNumber returns b with room for n bytes more, growing as append does.
//
//go:noinline
func growForNumber(b []byte, n int) []byte {
	grown := make([]byte, len(b), 2*cap(b)+n)
	copy(grown, b)
	return grown
}

// addrForMarshaler returns the pointer to the value held by v, to call a marshaler with a pointer receiver.
//
// The VM makes v from the address of the value ( interfaceOf ), so the data word of v is that address for every
// type, also for a type which is stored directly in an interface value, whose data word is then not the value:
// the marshaler is called with the original value, as encoding/json calls it for an addressable value, and
// nothing is allocated.
func addrForMarshaler(v any, rv reflect.Value) reflect.Value {
	return reflect.NewAt(rv.Type(), (*emptyInterface)(unsafe.Pointer(&v)).ptr)
}

// errStoppedByInvalidUTF8 stops an encoding which wrote a string of invalid UTF-8, whose error is the one of the
// string ( see RuntimeContext.InvalidUTF8Output ).
var errStoppedByInvalidUTF8 = stderrors.New("stopped by invalid UTF-8")

// appendValue appends the value at p by the function of m. An encoding which wrote a string of invalid UTF-8 goes
// on to its end, or to the next function, which is not called: a method or a function of marshaling is not called
// after the error, as it isn't by encoding/json/v2.
func appendValue(ctx *RuntimeContext, m *MarshalerCall, b []byte, p unsafe.Pointer) ([]byte, error) {
	if ctx.Option.Flag&RejectInvalidUTF8Option != 0 && ctx.HasInvalidUTF8() {
		return b, errStoppedByInvalidUTF8
	}
	return m.appendValue(ctx, b, p)
}

// appendV2MarshalJSON appends the output of MarshalJSON of the v2 semantics of the value at p, which it calls by
// its code, without the call of the function value of m ( see MarshalerCall.v2Raw ), as appendValue does after
// m.appendOutput, which AppendMarshalJSON looked at.
func appendV2MarshalJSON(ctx *RuntimeContext, m *MarshalerCall, b []byte, p unsafe.Pointer) ([]byte, error) {
	if ctx.Option.Flag&RejectInvalidUTF8Option != 0 && ctx.HasInvalidUTF8() {
		return b, errStoppedByInvalidUTF8
	}
	raw, err := m.call(p)
	return appendV2Raw(ctx, m.recv, b, raw, err)
}

// AppendMarshalJSON appends what MarshalJSON of the value returns, compacted. p is the data word of the
// interface value of the type of the opcode: the address of the value, or the pointer for a pointer type.
func AppendMarshalJSON(ctx *RuntimeContext, code *Opcode, b []byte, p unsafe.Pointer) ([]byte, error) {
	m := code.Marshaler
	if m == nil {
		return appendMarshalJSONByInterface(ctx, code, b, interfaceOf(code, p))
	}
	// a function of the v2 semantics has neither nilIsNull nor, but for MarshalJSON, appendOutput.
	if m.nilIsNull && p == nil {
		return AppendNull(ctx, b), nil
	}
	if m.appendOutput != nil {
		// the output of a type of the standard library is valid and compact: it is written as it is.
		if out, ok := m.appendOutput(b, p); ok {
			return out, nil
		}
	}
	var bb []byte
	var err error
	if code.Flags&(MarshalerContextFlags|MarshalerFuncFlags) != 0 {
		// the calls of a context, of MarshalJSONTo and of the v2 semantics, which the others don't look at.
		switch {
		case code.Flags&MarshalerFuncFlags == 0:
			stdctx := ctx.marshalerContext()
			if ctx.Option.Flag&FieldQueryOption != 0 {
				stdctx = SetFieldQueryToContext(stdctx, code.FieldQuery)
			}
			bb, err = m.callContext(p, stdctx)
		case m.marshalTo != nil:
			// its error is the one of MarshalJSONTo, or of the value it declined
			ctx.ValueDepth = ctx.BaseIndent + code.Indent
			var appended bool
			if bb, appended, err = m.marshalTo(ctx, b, p); err != nil || appended {
				return bb, err
			}
		case m.v2Raw:
			return appendV2MarshalJSON(ctx, m, b, p)
		default:
			ctx.ValueDepth = ctx.BaseIndent + code.Indent
			return appendValue(ctx, m, b, p)
		}
	} else {
		bb, err = m.call(p)
	}
	if err != nil {
		return nil, &errors.MarshalerError{Type: m.recv, Err: err}
	}
	escape := (ctx.Option.Flag & HTMLEscapeOption) != 0
	if out, ok := appendCompactOutput(b, bb, escape, m.trusted); ok {
		// the output is compact and valid: it is copied as it is.
		return out, nil
	}
	out, err := appendCompacted(ctx, b, bb, escape)
	if err != nil {
		return nil, &errors.MarshalerError{Type: m.recv, Err: err}
	}
	return out, nil
}

// interfaceOf returns the interface value of the type of the opcode whose data word is p.
func interfaceOf(code *Opcode, p unsafe.Pointer) any {
	return *(*any)(unsafe.Pointer(&emptyInterface{typ: code.Type, ptr: p}))
}

func appendMarshalJSONByInterface(ctx *RuntimeContext, code *Opcode, b []byte, v any) ([]byte, error) {
	rv := reflect.ValueOf(v) // convert by dynamic interface type
	if (code.Flags & AddrForMarshalerFlags) != 0 {
		rv = addrForMarshaler(v, rv)
	}

	if rv.Kind() == reflect.Ptr && rv.IsNil() {
		return AppendNull(ctx, b), nil
	}

	v = rv.Interface()
	var bb []byte
	if (code.Flags & MarshalerContextFlags) != 0 {
		marshaler, ok := v.(marshalerContext)
		if !ok {
			return AppendNull(ctx, b), nil
		}
		stdctx := ctx.marshalerContext()
		if ctx.Option.Flag&FieldQueryOption != 0 {
			stdctx = SetFieldQueryToContext(stdctx, code.FieldQuery)
		}
		b, err := marshaler.MarshalJSON(stdctx)
		if err != nil {
			return nil, &errors.MarshalerError{Type: reflect.TypeOf(v), Err: err}
		}
		bb = b
	} else {
		marshaler, ok := v.(json.Marshaler)
		if !ok {
			return AppendNull(ctx, b), nil
		}
		b, err := marshaler.MarshalJSON()
		if err != nil {
			return nil, &errors.MarshalerError{Type: reflect.TypeOf(v), Err: err}
		}
		bb = b
	}
	compactedBuf, err := appendCompact(ctx, b, bb, (ctx.Option.Flag&HTMLEscapeOption) != 0)
	if err != nil {
		return nil, &errors.MarshalerError{Type: reflect.TypeOf(v), Err: err}
	}
	return compactedBuf, nil
}

func AppendMarshalJSONIndent(ctx *RuntimeContext, code *Opcode, b []byte, p unsafe.Pointer) ([]byte, error) {
	m := code.Marshaler
	if m == nil {
		return appendMarshalJSONIndentByInterface(ctx, code, b, interfaceOf(code, p))
	}
	if m.nilIsNull && p == nil {
		return AppendNull(ctx, b), nil
	}
	if m.appendOutput != nil {
		// the output of a type of the standard library is one token, which has nothing to indent.
		if out, ok := m.appendOutput(b, p); ok {
			return out, nil
		}
	}
	var bb []byte
	var err error
	switch {
	case m.marshalTo != nil:
		// its error is the one of MarshalJSONTo, or of the value it declined
		ctx.ValueDepth = ctx.BaseIndent + code.Indent
		var appended bool
		if bb, appended, err = m.marshalTo(ctx, b, p); err != nil || appended {
			return bb, err
		}
	case (code.Flags & MarshalerContextFlags) != 0:
		bb, err = m.callContext(p, ctx.marshalerContext())
	default:
		bb, err = m.call(p)
	}
	if err != nil {
		return nil, &errors.MarshalerError{Type: m.recv, Err: err}
	}
	return appendIndentedMarshalJSON(ctx, code, b, bb)
}

func appendMarshalJSONIndentByInterface(ctx *RuntimeContext, code *Opcode, b []byte, v any) ([]byte, error) {
	rv := reflect.ValueOf(v) // convert by dynamic interface type
	if (code.Flags & AddrForMarshalerFlags) != 0 {
		rv = addrForMarshaler(v, rv)
	}
	// a nil pointer is null, as encoding/json does. The VM gives the address of the pointer,
	// so it is not known to be nil until here.
	if rv.Kind() == reflect.Ptr && rv.IsNil() {
		return AppendNull(ctx, b), nil
	}
	v = rv.Interface()
	var bb []byte
	if (code.Flags & MarshalerContextFlags) != 0 {
		marshaler, ok := v.(marshalerContext)
		if !ok {
			return AppendNull(ctx, b), nil
		}
		b, err := marshaler.MarshalJSON(ctx.marshalerContext())
		if err != nil {
			return nil, &errors.MarshalerError{Type: reflect.TypeOf(v), Err: err}
		}
		bb = b
	} else {
		marshaler, ok := v.(json.Marshaler)
		if !ok {
			return AppendNull(ctx, b), nil
		}
		b, err := marshaler.MarshalJSON()
		if err != nil {
			return nil, &errors.MarshalerError{Type: reflect.TypeOf(v), Err: err}
		}
		bb = b
	}
	return appendIndentedMarshalJSON(ctx, code, b, bb)
}

func appendIndentedMarshalJSON(ctx *RuntimeContext, code *Opcode, b []byte, bb []byte) ([]byte, error) {
	indentedBuf, err := appendIndent(
		ctx,
		b,
		bb,
		string(ctx.Prefix)+strings.Repeat(string(ctx.IndentStr), int(ctx.BaseIndent+code.Indent)),
		string(ctx.IndentStr),
		(ctx.Option.Flag&HTMLEscapeOption) != 0,
		false,
	)
	if err != nil {
		return nil, &errors.MarshalerError{Type: runtime.TypeOfPtr(code.Type), Err: err}
	}
	return indentedBuf, nil
}

// AppendMarshalText appends what MarshalText of the value returns, as a string. p is the data word of the
// interface value of the type of the opcode: the address of the value, or the pointer for a pointer type.
func AppendMarshalText(ctx *RuntimeContext, code *Opcode, b []byte, p unsafe.Pointer) ([]byte, error) {
	m := code.Marshaler
	if m == nil {
		if code.Flags&InterfaceMapKeyFlags != 0 {
			return appendInterfaceMapKey(ctx, code, b, p)
		}
		return appendMarshalTextByInterface(ctx, code, b, interfaceOf(code, p))
	}
	if m.nilIsNull && p == nil {
		return appendNilText(ctx, code, b), nil
	}
	var bytes []byte
	appended := false
	if m.appendOutput != nil {
		// the text of a type of the standard library is appended to a buffer of the context, not allocated.
		bytes, appended = m.appendOutput(ctx.MarshalBuf[:0], p)
		if appended {
			ctx.MarshalBuf = bytes
		}
	}
	if !appended {
		var err error
		if bytes, err = m.call(p); err != nil {
			return nil, errors.ErrMarshaler(m.recv, err, "MarshalText")
		}
	}
	// appendText, written here: it is not inlined, and this is the text of the key of most maps of texts.
	b, _ = jsonstring.AppendQuoted(StringEscaper(ctx), b, *(*string)(unsafe.Pointer(&bytes)))
	return b, nil
}

func appendMarshalTextByInterface(ctx *RuntimeContext, code *Opcode, b []byte, v any) ([]byte, error) {
	rv := reflect.ValueOf(v) // convert by dynamic interface type
	if (code.Flags & AddrForMarshalerFlags) != 0 {
		rv = addrForMarshaler(v, rv)
	}
	// a nil pointer is null, or "" as the name of a key, as encoding/json does. The VM gives the address of
	// the pointer, so it is not known to be nil until here.
	if rv.Kind() == reflect.Ptr && rv.IsNil() {
		return appendNilText(ctx, code, b), nil
	}
	v = rv.Interface()
	marshaler, ok := v.(encoding.TextMarshaler)
	if !ok {
		return AppendNull(ctx, b), nil
	}
	bytes, err := marshaler.MarshalText()
	if err != nil {
		return nil, errors.ErrMarshaler(reflect.TypeOf(v), err, "MarshalText")
	}
	return appendText(ctx, b, *(*string)(unsafe.Pointer(&bytes))), nil
}

// appendNilText appends the text of a nil pointer: null, or "" as the name of a key of a map, as
// encoding/json writes them.
func appendNilText(ctx *RuntimeContext, code *Opcode, b []byte) []byte {
	if code.Flags&MapKeyFlags != 0 {
		return append(b, `""`...)
	}
	return AppendNull(ctx, b)
}

// AppendMarshalTextIndent is AppendMarshalText: the text has no indent.
func AppendMarshalTextIndent(ctx *RuntimeContext, code *Opcode, b []byte, p unsafe.Pointer) ([]byte, error) {
	return AppendMarshalText(ctx, code, b, p)
}

func AppendNull(_ *RuntimeContext, b []byte) []byte {
	return append(b, "null"...)
}

func AppendComma(_ *RuntimeContext, b []byte) []byte {
	return append(b, ',')
}

func AppendCommaIndent(_ *RuntimeContext, b []byte) []byte {
	return append(b, ',', '\n')
}

func AppendStructEndIndent(ctx *RuntimeContext, code *Opcode, b []byte) []byte {
	b = append(b, '\n')
	b = append(b, ctx.Prefix...)
	indentNum := ctx.BaseIndent + code.Indent - 1
	for i := uint32(0); i < indentNum; i++ {
		b = append(b, ctx.IndentStr...)
	}
	return append(b, '}', ',', '\n')
}

func AppendIndent(ctx *RuntimeContext, b []byte, indent uint32) []byte {
	b = append(b, ctx.Prefix...)
	indentNum := ctx.BaseIndent + indent
	for i := uint32(0); i < indentNum; i++ {
		b = append(b, ctx.IndentStr...)
	}
	return b
}
