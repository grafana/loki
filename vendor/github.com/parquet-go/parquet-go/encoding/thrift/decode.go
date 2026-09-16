package thrift

import (
	"fmt"
	"io"
	"maps"
	"reflect"
	"slices"
	"sync/atomic"
)

// Unmarshal deserializes the thrift data from b to v using to the protocol p.
//
// The function errors if the data in b does not match the type of v.
//
// The function panics if v cannot be converted to a thrift representation.
//
// As an optimization, the value passed in v may be reused across multiple calls
// to Unmarshal, allowing the function to reuse objects referenced by pointer
// fields of struct values: non-nil pointer fields present in the input have
// their pointed-to value zeroed and decoded in place, and pointer fields
// absent from the input are set to nil. When reusing objects, the application
// remains responsible for resetting non-pointer state of v (scalars, strings,
// and slice lengths) before calling Unmarshal again.
func Unmarshal(p Protocol, b []byte, v any) error {
	pr := p.NewReaderFromBytes(slices.Clone(b))

	if err := NewDecoder(pr).Decode(v); err != nil {
		return err
	}

	if n := len(b) - pr.BytesRead(); n != 0 {
		return fmt.Errorf("unexpected trailing bytes at the end of thrift input: %d", n)
	}

	return nil
}

type Decoder struct {
	r Reader
	f Flags
}

func NewDecoder(r Reader) *Decoder {
	return &Decoder{r: r, f: decoderFlags(r)}
}

func (d *Decoder) Decode(v any) error {
	t := reflect.TypeOf(v)
	p := reflect.ValueOf(v)

	if t.Kind() != reflect.Ptr {
		panic("thrift.(*Decoder).Decode: expected pointer type but got " + t.String())
	}

	t = t.Elem()
	p = p.Elem()

	cache, _ := decoderCache.Load().(map[typeID]DecodeFunc)
	decode, _ := cache[makeTypeID(t)]

	if decode == nil {
		decode = DecodeFuncOf(t, make(DecodeFuncCache))

		newCache := make(map[typeID]DecodeFunc, len(cache)+1)
		newCache[makeTypeID(t)] = decode
		maps.Copy(newCache, cache)

		decoderCache.Store(newCache)
	}

	return decode(d.r, p, d.f)
}

func (d *Decoder) Reset(r Reader) {
	d.r = r
	d.f = d.f.Without(protocolFlags).With(decoderFlags(r))
}

func (d *Decoder) SetStrict(enabled bool) {
	if enabled {
		d.f = d.f.With(Strict)
	} else {
		d.f = d.f.Without(Strict)
	}
}

func decoderFlags(r Reader) Flags {
	return Flags(r.Protocol().Features() << featuresBitOffset)
}

var decoderCache atomic.Value // map[typeID]DecodeFunc

// DecodeFunc is a function that decodes a value from a thrift reader.
type DecodeFunc func(Reader, reflect.Value, Flags) error

// DecodeFuncCache is a cache for decode functions.
type DecodeFuncCache map[reflect.Type]DecodeFunc

// DecodeFuncFor returns the decode function for type T.
// This is a convenience wrapper around DecodeFuncOf using reflect.TypeFor.
func DecodeFuncFor[T any](cache DecodeFuncCache) DecodeFunc {
	return DecodeFuncOf(reflect.TypeFor[T](), cache)
}

// DecodeFuncOf returns the decode function for the given type.
func DecodeFuncOf(t reflect.Type, seen DecodeFuncCache) DecodeFunc {
	f := seen[t]
	if f != nil {
		return f
	}

	if isUnion(t) {
		return decodeFuncUnionOf(t, seen)
	}

	// Check if type implements Value interface first
	if t.Implements(valueType) {
		zv := reflect.Zero(t).Interface().(Value)
		f = zv.DecodeFunc(seen)
		seen[t] = f
		return f
	}

	switch t.Kind() {
	case reflect.Bool:
		f = decodeBool
	case reflect.Int8:
		f = decodeInt8
	case reflect.Int16:
		f = decodeInt16
	case reflect.Int32:
		f = decodeInt32
	case reflect.Int64, reflect.Int:
		f = decodeInt64
	case reflect.Float32, reflect.Float64:
		f = decodeFloat64
	case reflect.String:
		f = decodeString
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 { // []byte
			f = decodeBytes
		} else {
			f = decodeFuncSliceOf(t, seen)
		}
	case reflect.Map:
		f = decodeFuncMapOf(t, seen)
	case reflect.Struct:
		f = decodeFuncStructOf(t, seen)
	case reflect.Ptr:
		f = decodeFuncPtrOf(t, seen)
	default:
		panic("type cannot be decoded in thrift: " + t.String())
	}
	seen[t] = f
	return f
}

func decodeBool(r Reader, v reflect.Value, _ Flags) error {
	b, err := r.ReadBool()
	if err != nil {
		return err
	}
	v.SetBool(b)
	return nil
}

func decodeInt8(r Reader, v reflect.Value, _ Flags) error {
	i, err := r.ReadInt8()
	if err != nil {
		return err
	}
	v.SetInt(int64(i))
	return nil
}

func decodeInt16(r Reader, v reflect.Value, _ Flags) error {
	i, err := r.ReadInt16()
	if err != nil {
		return err
	}
	v.SetInt(int64(i))
	return nil
}

func decodeInt32(r Reader, v reflect.Value, _ Flags) error {
	i, err := r.ReadInt32()
	if err != nil {
		return err
	}
	v.SetInt(int64(i))
	return nil
}

func decodeInt64(r Reader, v reflect.Value, _ Flags) error {
	i, err := r.ReadInt64()
	if err != nil {
		return err
	}
	v.SetInt(int64(i))
	return nil
}

func decodeFloat64(r Reader, v reflect.Value, _ Flags) error {
	f, err := r.ReadFloat64()
	if err != nil {
		return err
	}
	v.SetFloat(f)
	return nil
}

func decodeString(r Reader, v reflect.Value, _ Flags) error {
	s, err := r.ReadString()
	if err != nil {
		return err
	}
	v.SetString(s)
	return nil
}

func decodeBytes(r Reader, v reflect.Value, _ Flags) error {
	b, err := r.ReadBytes()
	if err != nil {
		return err
	}
	v.SetBytes(b)
	return nil
}

func decodeFuncSliceOf(t reflect.Type, seen DecodeFuncCache) DecodeFunc {
	elem := t.Elem()
	typ := TypeOf(elem)
	dec := DecodeFuncOf(elem, seen)

	return func(r Reader, v reflect.Value, flags Flags) error {
		l, err := r.ReadList()
		if err != nil {
			return err
		}

		// Sometimes the list type is set to TRUE when the list contains only
		// TRUE values. Thrift does not seem to optimize the encoding by
		// omitting the boolean values that are known to all be TRUE, we still
		// need to decode them.
		switch l.Type {
		case TRUE:
			l.Type = BOOL
		}

		// TODO: implement type conversions?
		if typ != l.Type {
			if flags.Have(Strict) {
				return &TypeMismatch{item: "list item", Expect: typ, Found: l.Type}
			}
			return skipListItems(r, l)
		}

		size := int(l.Size)
		if v.IsNil() {
			v.Set(reflect.MakeSlice(t, size, size))
		} else if v.Cap() >= size {
			v.SetLen(size)
		} else {
			v.Grow(size - v.Len())
			v.SetLen(size)
		}
		flags = flags.Only(decodeFlags)

		for i := range size {
			if err := dec(r, v.Index(i), flags); err != nil {
				return with(dontExpectEOF(err), &decodeErrorList{cause: l, index: i})
			}
		}

		return nil
	}
}

func decodeFuncMapOf(t reflect.Type, seen DecodeFuncCache) DecodeFunc {
	key, elem := t.Key(), t.Elem()
	if elem.Size() == 0 { // map[?]struct{}
		return decodeFuncMapAsSetOf(t, seen)
	}

	mapType := reflect.MapOf(key, elem)
	keyZero := reflect.Zero(key)
	elemZero := reflect.Zero(elem)
	keyType := TypeOf(key)
	elemType := TypeOf(elem)
	decodeKey := DecodeFuncOf(key, seen)
	decodeElem := DecodeFuncOf(elem, seen)

	return func(r Reader, v reflect.Value, flags Flags) error {
		m, err := r.ReadMap()
		if err != nil {
			return err
		}

		v.Set(reflect.MakeMapWithSize(mapType, int(m.Size)))

		if m.Size == 0 { // empty map
			return nil
		}

		// TODO: implement type conversions?
		if keyType != m.Key {
			if flags.Have(Strict) {
				return &TypeMismatch{item: "map key", Expect: keyType, Found: m.Key}
			}
			return skipMapItems(r, m)
		}

		if elemType != m.Value {
			if flags.Have(Strict) {
				return &TypeMismatch{item: "map value", Expect: elemType, Found: m.Value}
			}
			return skipMapItems(r, m)
		}

		tmpKey := reflect.New(key).Elem()
		tmpElem := reflect.New(elem).Elem()
		flags = flags.Only(decodeFlags)

		for i := range int(m.Size) {
			if err := decodeKey(r, tmpKey, flags); err != nil {
				return with(dontExpectEOF(err), &decodeErrorMap{cause: m, index: i})
			}
			if err := decodeElem(r, tmpElem, flags); err != nil {
				return with(dontExpectEOF(err), &decodeErrorMap{cause: m, index: i})
			}
			v.SetMapIndex(tmpKey, tmpElem)
			tmpKey.Set(keyZero)
			tmpElem.Set(elemZero)
		}

		return nil
	}
}

func decodeFuncMapAsSetOf(t reflect.Type, seen DecodeFuncCache) DecodeFunc {
	key, elem := t.Key(), t.Elem()
	keyZero := reflect.Zero(key)
	elemZero := reflect.Zero(elem)
	typ := TypeOf(key)
	dec := DecodeFuncOf(key, seen)

	return func(r Reader, v reflect.Value, flags Flags) error {
		s, err := r.ReadSet()
		if err != nil {
			return err
		}

		// See decodeFuncSliceOf for details about why this type conversion
		// needs to be done.
		switch s.Type {
		case TRUE:
			s.Type = BOOL
		}

		v.Set(reflect.MakeMapWithSize(t, int(s.Size)))

		if s.Size == 0 {
			return nil
		}

		// TODO: implement type conversions?
		if typ != s.Type {
			if flags.Have(Strict) {
				return &TypeMismatch{item: "list item", Expect: typ, Found: s.Type}
			}
			return skipSetItems(r, s)
		}

		tmp := reflect.New(key).Elem()
		flags = flags.Only(decodeFlags)

		for i := range int(s.Size) {
			if err := dec(r, tmp, flags); err != nil {
				return with(dontExpectEOF(err), &decodeErrorSet{cause: s, index: i})
			}
			v.SetMapIndex(tmp, elemZero)
			tmp.Set(keyZero)
		}

		return nil
	}
}

type structDecoder struct {
	fields   []structDecoderField
	minID    int16
	required []uint64
	// clears contains the indices (into fields) of pointer-typed and
	// union-typed fields. Such fields absent from the input are zeroed
	// after decoding, so that reusing a previously decoded value as the
	// decode target produces the same result as decoding into a fresh
	// value. Without this, stale pointers (or stale union members) from a
	// previous decode would survive, which is invisible for regular
	// optional fields but corrupts values where "which member is non-nil"
	// carries meaning.
	clears []int
}

func (dec *structDecoder) decode(r Reader, v reflect.Value, flags Flags) error {
	flags = flags.Only(decodeFlags)
	coalesceBool := flags.Have(coalesceBoolFields)

	// seen tracks which of the (sparse, id-indexed) fields were observed
	// in the input; required is sized to the same word count.
	var seenBuf [4]uint64
	seen := seenBuf[:]
	if n := len(dec.required); n > len(seenBuf) {
		seen = make([]uint64, n)
	}

	// Inlined readStruct loop to avoid closure overhead
	lastFieldID := int16(0)
	numFields := 0

decodeFields:
	for {
		f, err := r.ReadField()
		if err != nil {
			if numFields > 0 {
				err = dontExpectEOF(err)
			}
			return err
		}

		if f.Type == STOP {
			break
		}

		if f.Delta {
			f.ID += lastFieldID
			f.Delta = false
		}

		i := int(f.ID) - int(dec.minID)
		var field *structDecoderField
		if i >= 0 && i < len(dec.fields) && dec.fields[i].decode != nil {
			field = &dec.fields[i]
		}
		// Unknown fields and (in non-strict mode) type-mismatched fields
		// are consumed so the stream stays aligned, and left unseen: they
		// were not decoded, so they must not satisfy the required check nor
		// protect a stale pointer from the clearUnseen pass below.
		// TODO: implement type conversions?
		if field == nil || (f.Type != field.typ && !(f.Type == TRUE && field.typ == BOOL)) {
			if field != nil && flags.Have(Strict) {
				return &TypeMismatch{item: "field value", Expect: field.typ, Found: f.Type}
			}
			if err := skip(r, f.Type); err != nil {
				return with(dontExpectEOF(err), &decodeErrorField{cause: f})
			}
			lastFieldID = f.ID
			numFields++
			continue decodeFields
		}
		seen[i/64] |= 1 << (i % 64)

		x := v
		for _, i := range field.index {
			if x.Kind() == reflect.Ptr {
				x = x.Elem()
			}
			if x = x.Field(i); x.Kind() == reflect.Ptr {
				if x.IsNil() {
					x.Set(reflect.New(x.Type().Elem()))
				}
			}
		}

		if coalesceBool && (f.Type == TRUE || f.Type == FALSE) {
			for x.Kind() == reflect.Ptr {
				if x.IsNil() {
					x.Set(reflect.New(x.Type().Elem()))
				}
				x = x.Elem()
			}
			// Handle BoolValue types (like Null[bool])
			if x.Type().Implements(boolValueType) {
				x.FieldByName("V").SetBool(f.Type == TRUE)
				x.FieldByName("Valid").SetBool(true)
			} else {
				x.SetBool(f.Type == TRUE)
			}
			lastFieldID = f.ID
			numFields++
			continue decodeFields
		}

		if err := field.decode(r, x, flags.With(field.flags)); err != nil {
			return with(dontExpectEOF(err), &decodeErrorField{cause: f})
		}

		lastFieldID = f.ID
		numFields++
	}

	for i, required := range dec.required {
		if mask := required & seen[i]; mask != required {
			missing := required &^ seen[i]
			i *= 64
			for (missing & 1) == 0 {
				missing >>= 1
				i++
			}
			field := &dec.fields[i]
			return &MissingField{Field: Field{ID: field.id, Type: field.typ}}
		}
	}

clearUnseen:
	for _, i := range dec.clears {
		if seen[i/64]&(1<<(i%64)) != 0 {
			continue
		}
		x := v
		for _, j := range dec.fields[i].index {
			if x.Kind() == reflect.Ptr {
				if x.IsNil() {
					continue clearUnseen
				}
				x = x.Elem()
			}
			x = x.Field(j)
		}
		// x is either a pointer field or a union struct; zeroing a union
		// nils its member interface, marking the union unset.
		if x.Kind() != reflect.Ptr || !x.IsNil() {
			x.SetZero()
		}
	}

	return nil
}

type structDecoderField struct {
	index  []int
	id     int16
	flags  Flags
	typ    Type
	isPtr  bool
	decode DecodeFunc
}

func decodeFuncStructOf(t reflect.Type, seen DecodeFuncCache) DecodeFunc {
	dec := &structDecoder{}
	decode := dec.decode
	seen[t] = decode

	fields := make([]structDecoderField, 0, t.NumField())
	forEachStructField(t, nil, func(f structField) {
		fields = append(fields, structDecoderField{
			index:  f.index,
			id:     f.id,
			flags:  f.flags,
			typ:    TypeOf(f.typ),
			isPtr:  f.typ.Kind() == reflect.Ptr,
			decode: decodeFuncStructFieldOf(f, seen),
		})
	})

	minID := int16(0)
	maxID := int16(0)

	for _, f := range fields {
		if f.id < minID || minID == 0 {
			minID = f.id
		}
		if f.id > maxID {
			maxID = f.id
		}
	}

	dec.fields = make([]structDecoderField, (maxID-minID)+1)
	dec.minID = minID
	dec.required = make([]uint64, (len(dec.fields)+63)/64)

	for _, f := range fields {
		i := f.id - minID
		p := dec.fields[i]
		if p.decode != nil {
			panic(fmt.Errorf("thrift struct field id %d is present multiple times in %s with types %s and %s", f.id, t, p.typ, f.typ))
		}
		dec.fields[i] = f
		if f.flags.Have(Required) {
			dec.required[i/64] |= 1 << (i % 64)
		}
		if f.isPtr || f.flags.Have(UnionType) {
			dec.clears = append(dec.clears, int(i))
		}
	}

	return decode
}

func decodeFuncStructFieldOf(f structField, seen DecodeFuncCache) DecodeFunc {
	if f.flags.Have(Enum) {
		switch f.typ.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return decodeInt32
		}
	}
	return DecodeFuncOf(f.typ, seen)
}

func decodeFuncPtrOf(t reflect.Type, seen DecodeFuncCache) DecodeFunc {
	elem := t.Elem()
	decode := DecodeFuncOf(t.Elem(), seen)
	return func(r Reader, v reflect.Value, f Flags) error {
		if v.IsNil() {
			v.Set(reflect.New(elem))
		} else {
			// Reuse the allocation but not the contents: optional fields
			// of the pointed-to value that are absent from the input must
			// not survive from a previous decode.
			v.Elem().SetZero()
		}
		return decode(r, v.Elem(), f)
	}
}

func readBinary(r Reader, f func(io.Reader) error) error {
	n, err := r.ReadLength()
	if err != nil {
		return err
	}
	return dontExpectEOF(f(io.LimitReader(r.Reader(), int64(n))))
}

func skip(r Reader, t Type) error {
	var err error
	switch t {
	case TRUE, FALSE:
		// When CoalesceBoolFields is advertised, boolean field values are encoded in the field
		// type (TRUE/FALSE) with no additional data bytes. ReadBool() would
		// consume a byte that belongs to the next field, misaligning the reader.
		// So, only read a byte for protocols that don't coalesce bool fields.
		// Related test file: binary_min_val_exact.parquet
		if r.Protocol().Features()&CoalesceBoolFields == 0 {
			_, err = r.ReadBool()
		}
	case I8:
		_, err = r.ReadInt8()
	case I16:
		_, err = r.ReadInt16()
	case I32:
		_, err = r.ReadInt32()
	case I64:
		_, err = r.ReadInt64()
	case DOUBLE:
		_, err = r.ReadFloat64()
	case BINARY:
		err = skipBinary(r)
	case LIST:
		err = skipList(r)
	case SET:
		err = skipSet(r)
	case MAP:
		err = skipMap(r)
	case STRUCT:
		err = skipStruct(r)
	case UUID:
		// UUID is 16 bytes (fixed size) in compact protocol. ReadFloat64 is
		// used because it reads exactly 8 raw bytes in compact encoding,
		// unlike ReadInt64 which reads a variable-length zigzag varint.
		_, err = r.ReadFloat64()
		if err == nil {
			_, err = r.ReadFloat64()
		}
	default:
		return fmt.Errorf("skipping unsupported thrift type %d", t)
	}
	return err
}

func skipBinary(r Reader) error {
	n, err := r.ReadLength()
	if err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	_, err = discard(r, int(n))
	return dontExpectEOF(err)
}

type discarder interface{ Discard(int) (int, error) }

// discard advances r past n bytes. The bytes-backed readers implement
// Discard themselves; their Reader() method returns a throwaway view that
// does not advance the read offset, so discarding through it would silently
// desynchronize the stream. Streaming readers discard through the
// underlying io.Reader when it supports it (*bufio.Reader does).
func discard(r Reader, n int) (int, error) {
	if d, ok := r.(discarder); ok {
		return d.Discard(n)
	}
	if d, ok := r.Reader().(discarder); ok {
		return d.Discard(n)
	}
	c, err := io.CopyN(io.Discard, r.Reader(), int64(n))
	return int(c), err
}

func skipList(r Reader) error {
	l, err := r.ReadList()
	if err != nil {
		return err
	}
	return skipListItems(r, l)
}

func skipSet(r Reader) error {
	s, err := r.ReadSet()
	if err != nil {
		return err
	}
	return skipSetItems(r, s)
}

func skipMap(r Reader) error {
	m, err := r.ReadMap()
	if err != nil {
		return err
	}
	return skipMapItems(r, m)
}

// skipItem consumes one container element of the given type. Unlike bool
// fields, whose value compact-protocol encoders coalesce into the field
// type, bool container elements always occupy one byte.
func skipItem(r Reader, t Type) error {
	switch t {
	case TRUE, FALSE:
		_, err := r.ReadBool()
		return err
	default:
		return skip(r, t)
	}
}

// skipListItems, skipSetItems, and skipMapItems consume the elements of a
// container whose header has already been read. They are used both by the
// skip functions above and by the decoders when the elements cannot be
// decoded into the target type: the payload must still be consumed, or the
// reader would desynchronize from the stream.

func skipListItems(r Reader, l List) error {
	for i := range int(l.Size) {
		if err := skipItem(r, l.Type); err != nil {
			return with(dontExpectEOF(err), &decodeErrorList{cause: l, index: i})
		}
	}
	return nil
}

func skipSetItems(r Reader, s Set) error {
	for i := range int(s.Size) {
		if err := skipItem(r, s.Type); err != nil {
			return with(dontExpectEOF(err), &decodeErrorSet{cause: s, index: i})
		}
	}
	return nil
}

func skipMapItems(r Reader, m Map) error {
	for i := range int(m.Size) {
		if err := skipItem(r, m.Key); err != nil {
			return with(dontExpectEOF(err), &decodeErrorMap{cause: m, index: i})
		}
		if err := skipItem(r, m.Value); err != nil {
			return with(dontExpectEOF(err), &decodeErrorMap{cause: m, index: i})
		}
	}
	return nil
}

func skipStruct(r Reader) error {
	lastFieldID := int16(0)
	numFields := 0

	for {
		f, err := r.ReadField()
		if err != nil {
			if numFields > 0 {
				err = dontExpectEOF(err)
			}
			return err
		}

		if f.Type == STOP {
			return nil
		}

		if f.Delta {
			f.ID += lastFieldID
			f.Delta = false
		}

		if err := skip(r, f.Type); err != nil {
			return with(dontExpectEOF(err), &decodeErrorField{cause: f})
		}

		lastFieldID = f.ID
		numFields++
	}
}
