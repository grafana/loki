package encoder

import (
	"bytes"
	"encoding"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
	"unsafe"

	"github.com/goccy/go-json/internal/errors"
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
	case OpStructFieldStruct:
		return true
	case OpStructFieldOmitEmpty:
		return true
	case OpStructFieldOmitEmptySlice:
		return true
	case OpStructFieldOmitEmptyArray:
		return true
	case OpStructFieldOmitEmptyMap:
		return true
	case OpStructFieldOmitEmptyStruct:
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
	case OpStructFieldOmitEmptyMapPtr:
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
	DataWordIsAddr           bool
	NoescapeKeyCode          *Opcode
	EscapeKeyCode            *Opcode
	InterfaceNoescapeKeyCode *Opcode
	InterfaceEscapeKeyCode   *Opcode
	CodeLength               int
	EndCode                  *Opcode
	// Scalar is the opcode of the value if the type is encoded by a single opcode of a scalar ( a number,
	// a string, a bool, ... ), or nil. Such a value held by an interface value is encoded without a frame.
	Scalar     *Opcode
	Code       Code
	QueryCache map[string]*OpcodeSet
	// values is the pool of the values of Type in the heap, which MarshalOf copies its argument to.
	values  sync.Pool
	cacheMu sync.RWMutex
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
	case reflect.Struct, reflect.Array:
		return ValueShapeAggregate
	case reflect.Map:
		// whether the type has a marshaler was decided when the type was compiled.
		codeSet, err := compileToGetUnfilteredCodeSet(uintptr(typ), false)
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
	codeSet, err := compileToGetUnfilteredCodeSet(uintptr(typ), false)
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
	s.cacheMu.RLock()
	codeSet := s.QueryCache[hash]
	s.cacheMu.RUnlock()
	return codeSet
}

func (s *OpcodeSet) setQueryCache(hash string, codeSet *OpcodeSet) {
	s.cacheMu.Lock()
	s.QueryCache[hash] = codeSet
	s.cacheMu.Unlock()
}

type CompiledCode struct {
	Code    *Opcode
	Linked  bool // whether recursive code already have linked
	CurLen  uintptr
	NextLen uintptr
	// Embedded is whether the recursive struct is embedded in the struct which jumps to it.
	// The code to jump to is only the fields of the struct then: it has neither the braces nor the check of nil.
	Embedded bool
}

const StartDetectingCyclesAfter = 1000

func ErrUnsupportedValue(code *Opcode, ptr unsafe.Pointer) *errors.UnsupportedValueError {
	v := *(*any)(unsafe.Pointer(&emptyInterface{
		typ: code.Type,
		ptr: ptr,
	}))
	return &errors.UnsupportedValueError{
		Value: reflect.ValueOf(v),
		Str:   fmt.Sprintf("encountered a cycle via %s", runtime.TypeOfPtr(code.Type)),
	}
}

func ErrUnsupportedFloat(v float64) *errors.UnsupportedValueError {
	return &errors.UnsupportedValueError{
		Value: reflect.ValueOf(v),
		Str:   strconv.FormatFloat(v, 'g', -1, 64),
	}
}

func ErrMarshalerWithCode(code *Opcode, err error) *errors.MarshalerError {
	return &errors.MarshalerError{
		Type: runtime.TypeOfPtr(code.Type),
		Err:  err,
	}
}

type emptyInterface struct {
	typ unsafe.Pointer
	ptr unsafe.Pointer
}

type MapItem struct {
	Key   []byte
	Value []byte
}

type Mapslice struct {
	Items []MapItem
	// escaped is whether a text was escaped while the entries of a map whose keys are texts were encoded ( see
	// appendText ): a key, or a text of a value, which tells that a key may have an escape.
	escaped bool
}

// Sort sorts the items by their keys, which are the names of the keys as encoding/json sorts them: an encoded
// key is in the order of its name unless it has an escape, and then the names are decoded to be sorted by.
// A text which was escaped while the entries were encoded tells that a key may have one ( see appendText ).
//
// It is not sort.Sort, which calls Less and Swap through an interface for every comparison:
// the maps to encode are small in most cases, and the sort was a tenth of the time to encode one.
func (m *Mapslice) Sort() {
	items := m.Items
	if m.escaped {
		sortMapItemsByNames(items)
		return
	}
	if len(items) > maxItemsOfInsertionSort {
		slices.SortFunc(items, func(a, b MapItem) int {
			return bytes.Compare(a.Key, b.Key)
		})
		return
	}
	insertionSortMapItems(items)
}

func insertionSortMapItems(items []MapItem) {
	for i := 1; i < len(items); i++ {
		if bytes.Compare(items[i-1].Key, items[i].Key) <= 0 {
			continue
		}
		item := items[i]
		j := i
		for ; j > 0 && bytes.Compare(items[j-1].Key, item.Key) > 0; j-- {
			items[j] = items[j-1]
		}
		items[j] = item
	}
}

// sortMapItemsByNames sorts the items by the names their encoded keys are of, which one of them has an escape
// for: the escape of a character is not in the order of the character.
func sortMapItemsByNames(items []MapItem) {
	type named struct {
		name string
		item MapItem
	}
	byName := make([]named, len(items))
	for i, item := range items {
		byName[i] = named{name: encodedKeyName(item.Key), item: item}
	}
	slices.SortStableFunc(byName, func(a, b named) int {
		return strings.Compare(a.name, b.name)
	})
	for i := range byName {
		items[i] = byName[i].item
	}
}

// encodedKeyName returns the name of an encoded key: the string from its first quote, whose escapes are the
// ones AppendString writes, decoded. What follows the string, and what precedes its quote, as the codes of a
// color, is not of the name.
func encodedKeyName(key []byte) string {
	start := bytes.IndexByte(key, '"')
	if start < 0 {
		return string(key)
	}
	var name []byte
	for i := start + 1; i < len(key); i++ {
		c := key[i]
		switch {
		case c == '"':
			return string(name)
		case c != '\\' || i+1 == len(key):
			name = append(name, c)
		case key[i+1] == 'u' && i+5 < len(key):
			r, err := strconv.ParseUint(string(key[i+2:i+6]), 16, 16)
			if err != nil {
				name = append(name, c)
				continue
			}
			name = utf8.AppendRune(name, rune(r))
			i += 5
		default:
			i++
			switch e := key[i]; e {
			case 'n':
				name = append(name, '\n')
			case 'r':
				name = append(name, '\r')
			case 't':
				name = append(name, '\t')
			case 'b':
				name = append(name, '\b')
			case 'f':
				name = append(name, '\f')
			default:
				name = append(name, e)
			}
		}
	}
	return string(name)
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
	// the entries are encoded in that order.
	Order    []int32
	prefixes []uint64 // the first bytes of the keys as numbers, while they are sorted
	Sorted   bool
	Len      int
	Idx      int
	// The entries of a sorted map whose keys are not of a string kind are encoded as they come and put in
	// the order of their encoded keys after: Start is where the key or the value being written starts,
	// First is where the entries start in the buffer, Slice has the entries and Buf is where they are copied.
	Start int
	First int
	Slice *Mapslice
	items []MapItem // what Slice.Items is made of, kept between the maps
	Buf   []byte
	// what the collector by reflect.MapIter works with: here so that nothing is allocated for a map.
	iter       reflect.MapIter
	keyIface   any
	valueIface any
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
	// Items is set by SortByEncodedKeys, and tells the VM the entries are put in that order.
	ctx.Slice.Items = nil
	return ctx
}

// SortByEncodedKeys makes the context put the entries in the order of their encoded keys, for a sorted map
// whose keys are not of a string kind: the VM records the entries in Slice as it encodes them.
func (c *MapContext) SortByEncodedKeys() {
	if cap(c.items) < c.Len {
		c.items = make([]MapItem, c.Len)
	}
	c.Slice.Items = c.items[:c.Len]
	c.Slice.escaped = false
}

// appendText appends the text of a marshaler or of the key of a map as a string. A text which is escaped is
// written longer than itself and its quotes: that tells the map being encoded, if any, that its keys are to be
// sorted by their names ( see Mapslice.Sort ).
func appendText(ctx *RuntimeContext, b []byte, text string) []byte {
	n := len(b)
	b = AppendString(ctx, b, text)
	if len(b)-n != len(text)+2 {
		ctx.textEscaped()
	}
	return b
}

// textEscaped tells the map being encoded, if any, that a text was escaped ( see appendText ).
func (c *RuntimeContext) textEscaped() {
	if c.mapDepth == 0 {
		return
	}
	if m := c.mapContexts[c.mapDepth-1]; m.layout != nil && m.layout.KeysMayEscape {
		m.Slice.escaped = true
	}
}

func ReleaseMapContext(rctx *RuntimeContext, c *MapContext) {
	// a map is always released before the maps it is in.
	rctx.mapDepth--
	// the keys refer to the map, which the context must not keep alive.
	clear(c.Keys)
	c.Keys = c.Keys[:0]
}

func AppendByteSlice(_ *RuntimeContext, b []byte, src []byte) []byte {
	if src == nil {
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

func AppendFloat32(_ *RuntimeContext, b []byte, v float32) []byte {
	f64 := float64(v)
	abs := math.Abs(f64)
	fmt := byte('f')
	// Note: Must use float32 comparisons for underlying float32 value to get precise cutoffs right.
	if abs != 0 {
		f32 := float32(abs)
		if f32 < 1e-6 || f32 >= 1e21 {
			fmt = 'e'
		}
	}
	return appendFloatOfFormat(b, f64, fmt, 32)
}

func AppendFloat64(_ *RuntimeContext, b []byte, v float64) []byte {
	abs := math.Abs(v)
	fmt := byte('f')
	// Note: Must use float32 comparisons for underlying float32 value to get precise cutoffs right.
	if abs != 0 {
		if abs < 1e-6 || abs >= 1e21 {
			fmt = 'e'
		}
	}
	return appendFloatOfFormat(b, v, fmt, 64)
}

// appendFloatOfFormat appends the float in the format, as encoding/json does: the exponent of the 'e' format
// has no leading zero, so 1e-07 is written as 1e-7.
func appendFloatOfFormat(b []byte, v float64, fmt byte, bits int) []byte {
	b = strconv.AppendFloat(b, v, fmt, -1, bits)
	if fmt == 'e' {
		n := len(b)
		if n >= 4 && b[n-4] == 'e' && b[n-3] == '-' && b[n-2] == '0' {
			b[n-2] = b[n-1]
			b = b[:n-1]
		}
	}
	return b
}

func AppendBool(_ *RuntimeContext, b []byte, v bool) []byte {
	if v {
		return append(b, "true"...)
	}
	return append(b, "false"...)
}

var (
	floatTable = [256]bool{
		'0': true,
		'1': true,
		'2': true,
		'3': true,
		'4': true,
		'5': true,
		'6': true,
		'7': true,
		'8': true,
		'9': true,
		'.': true,
		'e': true,
		'E': true,
		'+': true,
		'-': true,
	}
)

func AppendNumber(_ *RuntimeContext, b []byte, n json.Number) ([]byte, error) {
	if len(n) == 0 {
		return append(b, '0'), nil
	}
	for i := 0; i < len(n); i++ {
		if !floatTable[n[i]] {
			return nil, fmt.Errorf("json: invalid number literal %q", n)
		}
	}
	b = append(b, n...)
	return b, nil
}

// addrForMarshaler returns the pointer to the value held by v, to call a marshaler with a pointer receiver.
//
// The VM makes v from the address of the value, so the data word of v is that address unless the type is
// stored directly in an interface value. Only a pointer-sized type can be stored directly, so for the other
// sizes the pointer is made from the data word: the marshaler is called with the original value, as
// encoding/json does, and nothing is allocated. A pointer-sized value is copied, because its data word
// may be the value itself.
func addrForMarshaler(v any, rv reflect.Value) reflect.Value {
	if rv.CanAddr() {
		return rv.Addr()
	}
	typ := rv.Type()
	if typ.Size() != unsafe.Sizeof(unsafe.Pointer(nil)) {
		return reflect.NewAt(typ, (*emptyInterface)(unsafe.Pointer(&v)).ptr)
	}
	newV := reflect.New(typ)
	newV.Elem().Set(rv)
	return newV
}

// AppendMarshalJSON appends what MarshalJSON of the value returns, compacted. p is the data word of the
// interface value of the type of the opcode: the address of the value, or the pointer for a pointer type.
func AppendMarshalJSON(ctx *RuntimeContext, code *Opcode, b []byte, p unsafe.Pointer) ([]byte, error) {
	m := code.Marshaler
	if m == nil {
		return appendMarshalJSONByInterface(ctx, code, b, interfaceOf(code, p))
	}
	if m.nilIsNull && p == nil {
		return AppendNull(ctx, b), nil
	}
	var bb []byte
	var err error
	if (code.Flags & MarshalerContextFlags) != 0 {
		stdctx := ctx.marshalerContext()
		if ctx.Option.Flag&FieldQueryOption != 0 {
			stdctx = SetFieldQueryToContext(stdctx, code.FieldQuery)
		}
		bb, err = m.callContext(p, stdctx)
	} else {
		bb, err = m.call(p)
	}
	if err != nil {
		return nil, &errors.MarshalerError{Type: m.recv, Err: err}
	}
	escape := (ctx.Option.Flag & HTMLEscapeOption) != 0
	if out, ok := appendCompactOutput(b, bb, escape); ok {
		// the output is compact and valid: it is copied as it is.
		return out, nil
	}
	marshalBuf := ctx.MarshalBuf[:0]
	marshalBuf = append(append(marshalBuf, bb...), nul)
	compactedBuf, err := compact(b, marshalBuf, escape)
	if err != nil {
		return nil, err
	}
	ctx.MarshalBuf = marshalBuf
	return compactedBuf, nil
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
	marshalBuf := ctx.MarshalBuf[:0]
	marshalBuf = append(append(marshalBuf, bb...), nul)
	compactedBuf, err := compact(b, marshalBuf, (ctx.Option.Flag&HTMLEscapeOption) != 0)
	if err != nil {
		return nil, &errors.MarshalerError{Type: reflect.TypeOf(v), Err: err}
	}
	ctx.MarshalBuf = marshalBuf
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
	var bb []byte
	var err error
	if (code.Flags & MarshalerContextFlags) != 0 {
		bb, err = m.callContext(p, ctx.marshalerContext())
	} else {
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
	marshalBuf := ctx.MarshalBuf[:0]
	marshalBuf = append(append(marshalBuf, bb...), nul)
	indentedBuf, err := doIndent(
		b,
		marshalBuf,
		string(ctx.Prefix)+strings.Repeat(string(ctx.IndentStr), int(ctx.BaseIndent+code.Indent)),
		string(ctx.IndentStr),
		(ctx.Option.Flag&HTMLEscapeOption) != 0,
	)
	if err != nil {
		return nil, &errors.MarshalerError{Type: runtime.TypeOfPtr(code.Type), Err: err}
	}
	ctx.MarshalBuf = marshalBuf
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
	bytes, err := m.call(p)
	if err != nil {
		return nil, &errors.MarshalerError{Type: m.recv, Err: err}
	}
	// appendText, written here: it is not inlined, and this is the text of the key of most maps of texts.
	n := len(b)
	b = AppendString(ctx, b, *(*string)(unsafe.Pointer(&bytes)))
	if len(b)-n != len(bytes)+2 {
		ctx.textEscaped()
	}
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
		return nil, &errors.MarshalerError{Type: reflect.TypeOf(v), Err: err}
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

func AppendStructEnd(_ *RuntimeContext, b []byte) []byte {
	return append(b, '}', ',')
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

func IsNilForMarshaler(v any) bool {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Bool:
		return !rv.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return rv.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return math.Float64bits(rv.Float()) == 0
	case reflect.Interface, reflect.Ptr, reflect.Func:
		return rv.IsNil()
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		// the same as encoding/json: they are empty if the length is zero, even if they are not nil.
		return rv.Len() == 0
	}
	return false
}
