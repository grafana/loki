package encoder

import (
	"encoding/binary"
	"reflect"
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
// the key and of the value, and the hash depends only on the key. So map[K]V, with K of a string kind, is
// read as a map[string] of a value of the size of V ( see stringKeyCollector ) by the code the compiler makes
// for a range: no allocation, no call for an entry, and no dependence on the runtime beyond the layout of a
// map being what its type says. A map of another key, or of a value of another size, is read by
// reflect.MapIter, which is slower but is the public way.

// MapLayout is how the VM reads the entries of a map of a type, decided when the type is compiled.
type MapLayout struct {
	// collect reads the entries of the map at p into the context.
	collect func(p unsafe.Pointer, c *MapContext)
	// StringKey is whether the keys are of a string kind: then they are in MapContext.Keys.
	StringKey bool
	// A map whose entries are not sorted is written by the VM as it reads the map, when its keys are of a
	// string kind and the values are written by one opcode of a scalar ( ScalarValue: the map is ranged over
	// as a map of the values of the basic type of ValueKind, appendMapScalarValues ), or when the values are of
	// interface{}, the map of a JSON object as a value of interface{} ( InterfaceValue: the values which hold a
	// scalar are written, and the others are read into the context, appendMapAsRead ).
	ScalarValue    bool
	InterfaceValue bool
	// ValueKind is the kind of the values.
	ValueKind reflect.Kind
	keySize   uintptr
	valueSize uintptr
	// Cyclic is whether an entry may hold the map again, which is recorded for the detection of cycles then ( see
	// RuntimeContext.RecordMap ): the type of a key or a value reaches the type of the map, or an interface.
	Cyclic bool
}

// wordSize is the size of a word, of a pointer, by which a value is read when its size is a multiple of it.
const wordSize = unsafe.Sizeof(uintptr(0))

// mapValueWords is the number of the words of a value up to which a map is ranged over as a map of the same
// layout: 256 bytes on 64-bit platforms and 128 bytes on 32-bit ones. A value of more than 128 bytes is stored
// out of the map by the runtime, but so is the value of the map[string][n]uintptr of its size, so the layouts
// are still the same. Each size is a function of about a kilobyte of code, so the sizes stop there; a larger
// value is read by reflect.
const mapValueWords = 32

// NewMapLayout returns how the VM reads a map of the type.
func NewMapLayout(typ reflect.Type) *MapLayout {
	l := &MapLayout{
		StringKey: typ.Key().Kind() == reflect.String,
		keySize:   rawKeySize(typ.Key()),
		valueSize: typ.Elem().Size(),
		ValueKind: typ.Elem().Kind(),
		Cyclic:    reaches(typ.Key(), typ, map[reflect.Type]bool{}) || reaches(typ.Elem(), typ, map[reflect.Type]bool{}),
	}
	if collect := stringKeyCollector(l.valueSize); l.StringKey && collect != nil {
		l.collect = collect
		l.InterfaceValue = typ.Elem().Kind() == reflect.Interface && typ.Elem().NumMethod() == 0
	} else {
		l.collect = withMethodValues(typ.Elem(), newReflectCollector(typ))
	}
	return l
}

// stringKeyCollector returns the function which reads a map with keys of a string kind and values of size bytes
// as a map of a value of the same size, or nil when there is none. A value of a size and of an alignment up to
// the ones of the key is at the same place in a map of every layout ( the Swiss tables of Go 1.24, the split
// groups of Go 1.28, the buckets before ), whatever its type.
func stringKeyCollector(size uintptr) func(unsafe.Pointer, *MapContext) {
	switch {
	case size%wordSize == 0 && size/wordSize <= mapValueWords:
		return stringKeyCollectors[size/wordSize]
	case size < uintptr(len(byteValueCollectors)):
		return byteValueCollectors[size]
	}
	return nil
}

// byteValueCollectors are the functions which read a map with keys of a string kind, by the bytes of a value
// smaller than 32 bytes whose size is not a multiple of a word ( see stringKeyCollectors ): the sizes which are
// multiples of 8 bytes are left out, as they are of words on every platform.
var byteValueCollectors = [32]func(unsafe.Pointer, *MapContext){
	1:  collectStringKeys[[1]byte],
	2:  collectStringKeys[[2]byte],
	3:  collectStringKeys[[3]byte],
	4:  collectStringKeys[[4]byte],
	5:  collectStringKeys[[5]byte],
	6:  collectStringKeys[[6]byte],
	7:  collectStringKeys[[7]byte],
	9:  collectStringKeys[[9]byte],
	10: collectStringKeys[[10]byte],
	11: collectStringKeys[[11]byte],
	12: collectStringKeys[[12]byte],
	13: collectStringKeys[[13]byte],
	14: collectStringKeys[[14]byte],
	15: collectStringKeys[[15]byte],
	17: collectStringKeys[[17]byte],
	18: collectStringKeys[[18]byte],
	19: collectStringKeys[[19]byte],
	20: collectStringKeys[[20]byte],
	21: collectStringKeys[[21]byte],
	22: collectStringKeys[[22]byte],
	23: collectStringKeys[[23]byte],
	25: collectStringKeys[[25]byte],
	26: collectStringKeys[[26]byte],
	27: collectStringKeys[[27]byte],
	28: collectStringKeys[[28]byte],
	29: collectStringKeys[[29]byte],
	30: collectStringKeys[[30]byte],
	31: collectStringKeys[[31]byte],
}

// rawKeySize is the number of the bytes of RawKeys which a key of typ takes: its size, or one byte for a key of
// no size, so that a map of it has as many keys in RawKeys as it has. The byte is never used: such a key is read at
// its address only.
func rawKeySize(typ reflect.Type) uintptr {
	return max(typ.Size(), 1)
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
	collectStringKeys[[0]uintptr],
	collectStringKeys[[1]uintptr],
	collectStringKeys[[2]uintptr],
	collectStringKeys[[3]uintptr],
	collectStringKeys[[4]uintptr],
	collectStringKeys[[5]uintptr],
	collectStringKeys[[6]uintptr],
	collectStringKeys[[7]uintptr],
	collectStringKeys[[8]uintptr],
	collectStringKeys[[9]uintptr],
	collectStringKeys[[10]uintptr],
	collectStringKeys[[11]uintptr],
	collectStringKeys[[12]uintptr],
	collectStringKeys[[13]uintptr],
	collectStringKeys[[14]uintptr],
	collectStringKeys[[15]uintptr],
	collectStringKeys[[16]uintptr],
	collectStringKeys[[17]uintptr],
	collectStringKeys[[18]uintptr],
	collectStringKeys[[19]uintptr],
	collectStringKeys[[20]uintptr],
	collectStringKeys[[21]uintptr],
	collectStringKeys[[22]uintptr],
	collectStringKeys[[23]uintptr],
	collectStringKeys[[24]uintptr],
	collectStringKeys[[25]uintptr],
	collectStringKeys[[26]uintptr],
	collectStringKeys[[27]uintptr],
	collectStringKeys[[28]uintptr],
	collectStringKeys[[29]uintptr],
	collectStringKeys[[30]uintptr],
	collectStringKeys[[31]uintptr],
	collectStringKeys[[32]uintptr],
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
	keySize, valueSize := rawKeySize(keyType), valueType.Size()
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

// withMethodValues returns collect, which reads the values as the words of an interface{}, for values of valueType,
// with the values made the words of valueType when it is an interface type with methods: an interface{} has the
// type of the value where such a type has its itab.
func withMethodValues(valueType reflect.Type, collect func(unsafe.Pointer, *MapContext)) func(unsafe.Pointer, *MapContext) {
	if valueType.Kind() != reflect.Interface || valueType.NumMethod() == 0 {
		return collect
	}
	return func(p unsafe.Pointer, c *MapContext) {
		collect(p, c)
		for i := 0; i < len(c.Values); i += int(unsafe.Sizeof(any(nil))) {
			value := unsafe.Pointer(&c.Values[i])
			if v := *(*any)(value); v != nil {
				reflect.NewAt(valueType, value).Elem().Set(reflect.ValueOf(v))
			}
		}
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
// call which costs more than the rest of a sort of a small map. The sort is written here, so that the
// comparisons are inlined into it: a sort which takes the comparison as a function, as slices.SortFunc does,
// calls it for every one.
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
	if n > maxItemsOfInsertionSort {
		sortKeyOrder(order, prefixes, keys)
	} else {
		insertionSortKeyOrder(order, prefixes, keys)
	}
	c.Order = order
}

// keyLess is whether the key of the entry a comes before the one of b.
func keyLess(a, b int32, prefixes []uint64, keys []string) bool {
	if pa, pb := prefixes[a], prefixes[b]; pa != pb {
		return pa < pb
	}
	return keys[a] < keys[b]
}

// sortKeyOrder sorts the entries of order by their keys: by quicksort, with the median of three entries as the
// pivot, down to the parts of up to maxItemsOfInsertionSort entries, which are sorted by insertion. The smaller
// part is sorted first, and the larger one by the loop, so that the depth is logarithmic.
func sortKeyOrder(order []int32, prefixes []uint64, keys []string) {
	for len(order) > maxItemsOfInsertionSort {
		last := len(order) - 1
		mid := last / 2
		// the median of the first, the middle and the last entries, at the middle
		if keyLess(order[mid], order[0], prefixes, keys) {
			order[0], order[mid] = order[mid], order[0]
		}
		if keyLess(order[last], order[mid], prefixes, keys) {
			order[mid], order[last] = order[last], order[mid]
			if keyLess(order[mid], order[0], prefixes, keys) {
				order[0], order[mid] = order[mid], order[0]
			}
		}
		pivot := order[mid]
		// Hoare's partition: the keys are distinct, so an entry equals the pivot only if it is the pivot.
		i, j := 0, last
		for {
			for keyLess(order[i], pivot, prefixes, keys) {
				i++
			}
			for keyLess(pivot, order[j], prefixes, keys) {
				j--
			}
			if i >= j {
				break
			}
			order[i], order[j] = order[j], order[i]
			i++
			j--
		}
		if j+1 < len(order)-j-1 {
			sortKeyOrder(order[:j+1], prefixes, keys)
			order = order[j+1:]
		} else {
			sortKeyOrder(order[j+1:], prefixes, keys)
			order = order[:j+1]
		}
	}
	insertionSortKeyOrder(order, prefixes, keys)
}

// insertionSortKeyOrder sorts the entries of order by their keys by insertion, which is the fastest sort of a few
// entries.
func insertionSortKeyOrder(order []int32, prefixes []uint64, keys []string) {
	for i := 1; i < len(order); i++ {
		e := order[i]
		if !keyLess(e, order[i-1], prefixes, keys) {
			continue
		}
		j := i
		for ; j > 0 && keyLess(e, order[j-1], prefixes, keys); j-- {
			order[j] = order[j-1]
		}
		order[j] = e
	}
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

// reaches reports whether a value of typ may hold a value of target: typ is target, or an interface, or has one of
// them through its pointers, elements, keys and fields. seen are the types walked, which a recursive type reaches
// again.
func reaches(typ, target reflect.Type, seen map[reflect.Type]bool) bool {
	if typ == target || typ.Kind() == reflect.Interface {
		return true
	}
	if seen[typ] {
		return false
	}
	seen[typ] = true
	switch typ.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array:
		return reaches(typ.Elem(), target, seen)
	case reflect.Map:
		return reaches(typ.Key(), target, seen) || reaches(typ.Elem(), target, seen)
	case reflect.Struct:
		for i := range typ.NumField() {
			if reaches(typ.Field(i).Type, target, seen) {
				return true
			}
		}
	}
	return false
}
