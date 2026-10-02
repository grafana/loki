package encoder

import (
	"encoding/binary"
	"reflect"
	"slices"
	"strings"
	"unsafe"

	"github.com/goccy/go-json/internal/runtime"
)

// The entries of a map are read into the map context before they are encoded: the keys and the values are
// copied, so that the VM encodes them from the context, in the order of the map or sorted by the keys.
//
// The runtime has no public function which gives the entries of a map of a type known only at run time
// without an allocation: reflect.MapIter copies a key and a value with a call of ten nanoseconds each, and
// go-json read them through linknames of the runtime for years. Instead, a map with keys of a string kind is
// ranged over as a map of the same layout: the layout of a map depends only on the size and the alignment of
// the key and of the value, and the hash depends only on the key. So map[K]V, with K of a string kind and
// V of up to 128 bytes, is read as map[string][n]uint64 by the code the compiler makes for a range: no
// allocation, no call for an entry, and no dependence on the runtime beyond the layout of a map being what
// its type says. A map of another key is read by reflect.MapIter, which is slower but is the public way.

// MapLayout is how the VM reads the entries of a map of a type, decided when the type is compiled.
type MapLayout struct {
	// collect reads the entries of the map at p into the context.
	collect func(p unsafe.Pointer, c *MapContext)
	// StringKey is whether the keys are of a string kind: then they are in MapContext.Keys.
	StringKey bool
	// A map whose entries are not sorted is written by the VM as it reads the map, when its keys are of a
	// string kind and the values are written by one opcode of a scalar ( ScalarValue: the map is ranged over
	// as a map of a value of ValueWords words, appendMapScalarValues ), or when the values are of interface{},
	// the map of a JSON object as a value of interface{} ( InterfaceValue: the values which hold a scalar are
	// written, and the others are read into the context, appendMapScalarEntries ).
	ScalarValue    bool
	InterfaceValue bool
	// ValueWords is the number of the words of a value when the map is ranged over as a map of the same layout,
	// or -1 when it is read by reflect.
	ValueWords int
	keySize    uintptr
	valueSize  uintptr
	// KeysMayEscape is whether an encoded key may have an escape, which puts it out of the order of its name
	// ( see Mapslice.Sort ): the keys of a kind other than a string or an integer are texts.
	KeysMayEscape bool
}

// MapScalarValueWords is the largest ValueWords of a value written by one opcode of a scalar: a slice of bytes.
const MapScalarValueWords = 3

// mapValueWords is the number of the words of a value up to which a map is ranged over as a map of the same
// layout: a larger value is stored out of the map by the runtime, which changes the layout.
const mapValueWords = 16

// NewMapLayout returns how the VM reads a map of the type.
func NewMapLayout(typ reflect.Type) *MapLayout {
	l := &MapLayout{
		StringKey: typ.Key().Kind() == reflect.String,
		keySize:   typ.Key().Size(),
		valueSize: typ.Elem().Size(),
	}
	switch typ.Key().Kind() {
	case reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		// a key of a string kind is sorted by its string, unless it has MarshalText, and an integer has no escape.
		l.KeysMayEscape = typ.Key().Implements(marshalTextType) || reflect.PointerTo(typ.Key()).Implements(marshalTextType)
	default:
		l.KeysMayEscape = true
	}
	words := (l.valueSize + 7) / 8
	if l.StringKey && words <= mapValueWords && (mapValueIsWords || l.valueSize%8 == 0) {
		l.collect = stringKeyCollectors[words]
		l.valueSize = words * 8
		l.ValueWords = int(words)
		l.InterfaceValue = typ.Elem().Kind() == reflect.Interface && typ.Elem().NumMethod() == 0
	} else {
		l.collect = newReflectCollector(typ)
		l.ValueWords = -1
	}
	return l
}

// collectStringKeys reads the map at p, as a map[string]V, into the context.
func collectStringKeys[V any](p unsafe.Pointer, c *MapContext) {
	for k, v := range *(*map[string]V)(unsafe.Pointer(&p)) {
		c.Keys = append(c.Keys, k)
		c.Values = append(c.Values, unsafe.Slice((*byte)(unsafe.Pointer(&v)), unsafe.Sizeof(v))...)
	}
}

// stringKeyCollectors are the functions which read a map with keys of a string kind, by the words of a value.
var stringKeyCollectors = [mapValueWords + 1]func(unsafe.Pointer, *MapContext){
	collectStringKeys[[0]uint64],
	collectStringKeys[[1]uint64],
	collectStringKeys[[2]uint64],
	collectStringKeys[[3]uint64],
	collectStringKeys[[4]uint64],
	collectStringKeys[[5]uint64],
	collectStringKeys[[6]uint64],
	collectStringKeys[[7]uint64],
	collectStringKeys[[8]uint64],
	collectStringKeys[[9]uint64],
	collectStringKeys[[10]uint64],
	collectStringKeys[[11]uint64],
	collectStringKeys[[12]uint64],
	collectStringKeys[[13]uint64],
	collectStringKeys[[14]uint64],
	collectStringKeys[[15]uint64],
	collectStringKeys[[16]uint64],
}

// newReflectCollector returns the function which reads a map of the type by reflect.MapIter: the key and the
// value of an entry are set to interface values, which then refer to them in the map, or hold them if they
// are of a pointer shape, without an allocation.
func newReflectCollector(typ reflect.Type) func(unsafe.Pointer, *MapContext) {
	keyType, valueType := typ.Key(), typ.Elem()
	if keyType.Kind() == reflect.Interface {
		return newInterfaceKeyCollector(typ)
	}
	stringKey := keyType.Kind() == reflect.String
	keySize, valueSize := keyType.Size(), valueType.Size()
	keyDirect, valueDirect := !runtime.IfaceIndir(keyType), !runtime.IfaceIndir(valueType)
	// a value which is an interface value is copied to the interface value as it is: the value is the words.
	valueIsIface := valueType.Kind() == reflect.Interface
	typPtr := runtime.TypePtr(typ)
	return func(p unsafe.Pointer, c *MapContext) {
		// the map as a value which is not addressable: SetIterKey copies the key of an addressable map.
		var mapIface any
		*(*emptyInterface)(unsafe.Pointer(&mapIface)) = emptyInterface{typ: typPtr, ptr: p}
		m := reflect.ValueOf(mapIface)
		key := reflect.ValueOf(&c.keyIface).Elem()
		value := reflect.ValueOf(&c.valueIface).Elem()
		it := &c.iter
		it.Reset(m)
		for it.Next() {
			key.SetIterKey(it)
			value.SetIterValue(it)
			k := ifaceData(&c.keyIface, keyDirect)
			if stringKey {
				c.Keys = append(c.Keys, *(*string)(k))
			} else {
				c.RawKeys = append(c.RawKeys, unsafe.Slice((*byte)(k), keySize)...)
			}
			v := ifaceData(&c.valueIface, valueDirect)
			if valueIsIface {
				v = unsafe.Pointer(&c.valueIface)
			}
			c.Values = append(c.Values, unsafe.Slice((*byte)(v), valueSize)...)
		}
		it.Reset(reflect.Value{})
		c.keyIface, c.valueIface = nil, nil
	}
}

// newInterfaceKeyCollector is newReflectCollector for a map whose keys are of an interface type: a key is the
// words of the interface{} it is set to, whatever its interface type is ( see appendInterfaceMapKey ).
func newInterfaceKeyCollector(typ reflect.Type) func(unsafe.Pointer, *MapContext) {
	valueType := typ.Elem()
	valueSize := valueType.Size()
	valueDirect := !runtime.IfaceIndir(valueType)
	valueIsIface := valueType.Kind() == reflect.Interface
	typPtr := runtime.TypePtr(typ)
	return func(p unsafe.Pointer, c *MapContext) {
		var mapIface any
		*(*emptyInterface)(unsafe.Pointer(&mapIface)) = emptyInterface{typ: typPtr, ptr: p}
		m := reflect.ValueOf(mapIface)
		key := reflect.ValueOf(&c.keyIface).Elem()
		value := reflect.ValueOf(&c.valueIface).Elem()
		it := &c.iter
		it.Reset(m)
		for it.Next() {
			key.SetIterKey(it)
			value.SetIterValue(it)
			c.RawKeys = append(c.RawKeys, unsafe.Slice((*byte)(unsafe.Pointer(&c.keyIface)), unsafe.Sizeof(c.keyIface))...)
			v := ifaceData(&c.valueIface, valueDirect)
			if valueIsIface {
				v = unsafe.Pointer(&c.valueIface)
			}
			c.Values = append(c.Values, unsafe.Slice((*byte)(v), valueSize)...)
		}
		it.Reset(reflect.Value{})
		c.keyIface, c.valueIface = nil, nil
	}
}

// ifaceData returns the address of the value held by the interface value: the data word if the value is stored
// in it, which is the case for a value of a pointer shape, or what the data word points to.
func ifaceData(iface *any, direct bool) unsafe.Pointer {
	data := &(*emptyInterface)(unsafe.Pointer(iface)).ptr
	if direct {
		return unsafe.Pointer(data)
	}
	return *data
}

// Reset empties the context for the entries of a map of the layout.
func (l *MapLayout) Reset(c *MapContext) {
	c.Keys, c.RawKeys, c.Values = c.Keys[:0], c.RawKeys[:0], c.Values[:0]
	c.layout = l
}

// Collect reads the entries of the map at p into the context, and returns their number.
func (l *MapLayout) Collect(p unsafe.Pointer, c *MapContext) int {
	l.Reset(c)
	l.collect(p, c)
	if l.StringKey {
		c.Len = len(c.Keys)
	} else {
		c.Len = len(c.RawKeys) / int(l.keySize)
	}
	return c.Len
}

// MapLen returns the number of the entries of the map at p. The length of a map is in its header, whatever
// its type, so the map is read as one of any type.
func MapLen(p unsafe.Pointer) int {
	return len(*(*map[struct{}]struct{})(unsafe.Pointer(&p)))
}

// KeyAt returns the address of the key of the entry: the string in Keys, or the bytes in RawKeys.
func (c *MapContext) KeyAt(i int) unsafe.Pointer {
	if c.layout.StringKey {
		return unsafe.Pointer(&c.Keys[i])
	}
	return unsafe.Pointer(&c.RawKeys[uintptr(i)*c.layout.keySize])
}

// ValueAt returns the address of the value of the entry. A value of no size has an address all the same: the
// opcode of a struct takes a nil one for a nil pointer.
func (c *MapContext) ValueAt(i int) unsafe.Pointer {
	if c.layout.valueSize == 0 {
		return unsafe.Pointer(&c.Len)
	}
	return unsafe.Pointer(&c.Values[uintptr(i)*c.layout.valueSize])
}

// SortKeys sorts the entries by their keys, which are strings, as encoding/json does: Order is the entries in
// that order.
//
// A comparison of two keys is by their first eight bytes as one number first, which decides it for most of
// the keys of a JSON object, and by the strings only when those are the same: a comparison of strings is a
// call which costs more than the rest of a sort of a small map. The insertion sort is for the small maps,
// which most of the maps are ( see Mapslice.Sort ).
func (c *MapContext) SortKeys() {
	n := len(c.Keys)
	if cap(c.Order) < n {
		c.Order = make([]int32, n)
		c.prefixes = make([]uint64, n)
	}
	order, prefixes, keys := c.Order[:n], c.prefixes[:n], c.Keys
	for i, key := range keys {
		order[i] = int32(i)
		prefixes[i] = keyPrefix(key)
	}
	less := func(a, b int32) bool {
		if prefixes[a] != prefixes[b] {
			return prefixes[a] < prefixes[b]
		}
		return keys[a] < keys[b]
	}
	if n > maxItemsOfInsertionSort {
		slices.SortFunc(order, func(a, b int32) int {
			if prefixes[a] != prefixes[b] {
				if prefixes[a] < prefixes[b] {
					return -1
				}
				return 1
			}
			return strings.Compare(keys[a], keys[b])
		})
	} else {
		for i := 1; i < n; i++ {
			e := order[i]
			if !less(e, order[i-1]) {
				continue
			}
			j := i
			for ; j > 0 && less(e, order[j-1]); j-- {
				order[j] = order[j-1]
			}
			order[j] = e
		}
	}
	c.Order = order
}

// keyPrefix returns the first eight bytes of the key as a number which compares as the bytes do, with zeros
// after a shorter key: a shorter key compares as less, as it should, unless the other has zeros there, and then
// the strings are compared.
func keyPrefix(key string) uint64 {
	if len(key) >= 8 {
		return binary.BigEndian.Uint64(unsafe.Slice(unsafe.StringData(key), 8))
	}
	var b [8]byte
	copy(b[:], key)
	return binary.BigEndian.Uint64(b[:])
}
