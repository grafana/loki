package variant

import (
	"encoding/binary"
	"fmt"
	"math"
	"reflect"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Encode encodes a variant Value into its binary representation.
// Field names for objects are registered with the provided MetadataBuilder.
// Returns the encoded value bytes.
func Encode(b *MetadataBuilder, v Value) []byte {
	switch v.basic {
	case BasicObject:
		enc := getEncoder(b)

		start, end, err := enc.encodeValueObject(v.object)
		if err != nil {
			releaseEncoder(enc)
			return nil
		}
		res := make([]byte, end-start)
		copy(res, enc.scratch[start:end])

		releaseEncoder(enc)
		return res
	case BasicArray:
		enc := getEncoder(b)

		start, end, err := enc.encodeValueArray(v.array)
		if err != nil {
			releaseEncoder(enc)
			return nil
		}
		res := make([]byte, end-start)
		copy(res, enc.scratch[start:end])

		releaseEncoder(enc)
		return res
	default:
		return encodePrimitive(v)
	}
}

func encodePrimitive(v Value) []byte {
	switch v.primitive {
	case PrimitiveNull:
		return []byte{makeHeader(BasicPrimitive, byte(PrimitiveNull))}
	case PrimitiveTrue:
		return []byte{makeHeader(BasicPrimitive, byte(PrimitiveTrue))}
	case PrimitiveFalse:
		return []byte{makeHeader(BasicPrimitive, byte(PrimitiveFalse))}
	case PrimitiveInt8:
		return []byte{makeHeader(BasicPrimitive, byte(PrimitiveInt8)), byte(v.i64)}
	case PrimitiveInt16:
		buf := make([]byte, 3)
		buf[0] = makeHeader(BasicPrimitive, byte(PrimitiveInt16))
		binary.LittleEndian.PutUint16(buf[1:], uint16(v.i64))
		return buf
	case PrimitiveInt32:
		buf := make([]byte, 5)
		buf[0] = makeHeader(BasicPrimitive, byte(PrimitiveInt32))
		binary.LittleEndian.PutUint32(buf[1:], uint32(v.i64))
		return buf
	case PrimitiveInt64:
		buf := make([]byte, 9)
		buf[0] = makeHeader(BasicPrimitive, byte(PrimitiveInt64))
		binary.LittleEndian.PutUint64(buf[1:], uint64(v.i64))
		return buf
	case PrimitiveFloat:
		buf := make([]byte, 5)
		buf[0] = makeHeader(BasicPrimitive, byte(PrimitiveFloat))
		binary.LittleEndian.PutUint32(buf[1:], math.Float32bits(float32(v.f64)))
		return buf
	case PrimitiveDouble:
		buf := make([]byte, 9)
		buf[0] = makeHeader(BasicPrimitive, byte(PrimitiveDouble))
		binary.LittleEndian.PutUint64(buf[1:], math.Float64bits(v.f64))
		return buf
	case PrimitiveString:
		s := v.str
		if len(s) <= 63 {
			buf := make([]byte, 1+len(s))
			buf[0] = makeHeader(BasicShortString, byte(len(s)))
			copy(buf[1:], s)
			return buf
		}
		buf := make([]byte, 5+len(s))
		buf[0] = makeHeader(BasicPrimitive, byte(PrimitiveString))
		binary.LittleEndian.PutUint32(buf[1:], uint32(len(s)))
		copy(buf[5:], s)
		return buf
	case PrimitiveBinary:
		buf := make([]byte, 5+len(v.bytes))
		buf[0] = makeHeader(BasicPrimitive, byte(PrimitiveBinary))
		binary.LittleEndian.PutUint32(buf[1:], uint32(len(v.bytes)))
		copy(buf[5:], v.bytes)
		return buf
	case PrimitiveDate:
		buf := make([]byte, 5)
		buf[0] = makeHeader(BasicPrimitive, byte(PrimitiveDate))
		binary.LittleEndian.PutUint32(buf[1:], uint32(v.i64))
		return buf
	case PrimitiveTimestamp:
		buf := make([]byte, 9)
		buf[0] = makeHeader(BasicPrimitive, byte(PrimitiveTimestamp))
		binary.LittleEndian.PutUint64(buf[1:], uint64(v.i64))
		return buf
	case PrimitiveTimestampNTZ:
		buf := make([]byte, 9)
		buf[0] = makeHeader(BasicPrimitive, byte(PrimitiveTimestampNTZ))
		binary.LittleEndian.PutUint64(buf[1:], uint64(v.i64))
		return buf
	case PrimitiveTime:
		buf := make([]byte, 9)
		buf[0] = makeHeader(BasicPrimitive, byte(PrimitiveTime))
		binary.LittleEndian.PutUint64(buf[1:], uint64(v.i64))
		return buf
	case PrimitiveTimestampNanos:
		buf := make([]byte, 9)
		buf[0] = makeHeader(BasicPrimitive, byte(PrimitiveTimestampNanos))
		binary.LittleEndian.PutUint64(buf[1:], uint64(v.i64))
		return buf
	case PrimitiveTimestampNTZNanos:
		buf := make([]byte, 9)
		buf[0] = makeHeader(BasicPrimitive, byte(PrimitiveTimestampNTZNanos))
		binary.LittleEndian.PutUint64(buf[1:], uint64(v.i64))
		return buf
	case PrimitiveUUID:
		buf := make([]byte, 17)
		buf[0] = makeHeader(BasicPrimitive, byte(PrimitiveUUID))
		copy(buf[1:], v.uuid[:])
		return buf
	case PrimitiveDecimal4:
		buf := make([]byte, 6)
		buf[0] = makeHeader(BasicPrimitive, byte(PrimitiveDecimal4))
		buf[1] = v.scale
		binary.LittleEndian.PutUint32(buf[2:], uint32(v.i64))
		return buf
	case PrimitiveDecimal8:
		buf := make([]byte, 10)
		buf[0] = makeHeader(BasicPrimitive, byte(PrimitiveDecimal8))
		buf[1] = v.scale
		binary.LittleEndian.PutUint64(buf[2:], uint64(v.i64))
		return buf
	case PrimitiveDecimal16:
		buf := make([]byte, 18)
		buf[0] = makeHeader(BasicPrimitive, byte(PrimitiveDecimal16))
		buf[1] = v.scale
		copy(buf[2:], v.decimal16[:])
		return buf
	default:
		return []byte{makeHeader(BasicPrimitive, byte(PrimitiveNull))}
	}
}

// makeHeader constructs a value_metadata byte.
func makeHeader(basic BasicType, valueHeader byte) byte {
	return byte(basic) | (valueHeader << 2)
}

type encoder struct {
	b         *MetadataBuilder
	scratch   []byte
	temp      []byte
	positions []elementPos
	fields    []encodedField
	offsets   []int
}

var encoderPool = sync.Pool{
	New: func() any {
		return &encoder{
			scratch:   make([]byte, 0, 1024),
			temp:      make([]byte, 0, 1024),
			positions: make([]elementPos, 0, 32),
			fields:    make([]encodedField, 0, 32),
			offsets:   make([]int, 0, 32),
		}
	},
}

const (
	maxPooledEncoderBytes = 1 << 20
	maxPooledEncoderItems = 4096
)

func getEncoder(b *MetadataBuilder) *encoder {
	enc := encoderPool.Get().(*encoder)
	enc.b = b
	enc.scratch = enc.scratch[:0]
	enc.temp = enc.temp[:0]
	enc.positions = enc.positions[:0]
	enc.fields = enc.fields[:0]
	enc.offsets = enc.offsets[:0]
	return enc
}

func releaseEncoder(enc *encoder) {
	enc.b = nil
	if cap(enc.scratch) > maxPooledEncoderBytes {
		enc.scratch = nil
	} else {
		enc.scratch = enc.scratch[:0]
	}
	if cap(enc.temp) > maxPooledEncoderBytes {
		enc.temp = nil
	} else {
		enc.temp = enc.temp[:0]
	}
	if cap(enc.positions) > maxPooledEncoderItems {
		enc.positions = nil
	} else {
		enc.positions = enc.positions[:0]
	}
	if cap(enc.fields) > maxPooledEncoderItems {
		enc.fields = nil
	} else {
		enc.fields = enc.fields[:0]
	}
	if cap(enc.offsets) > maxPooledEncoderItems {
		enc.offsets = nil
	} else {
		enc.offsets = enc.offsets[:0]
	}
	encoderPool.Put(enc)
}

type encodedField struct {
	id    int
	name  string
	start int
	end   int
}

type elementPos struct {
	start int
	end   int
}

var testHookVerifyArrayLayout func(e *encoder, positions []elementPos)
var testHookVerifyObjectLayout func(e *encoder, entries []encodedField)

func (e *encoder) encodeValuePrimitive(v Value) (start, end int) {
	start = len(e.scratch)
	switch v.primitive {
	case PrimitiveNull:
		e.scratch = append(e.scratch, makeHeader(BasicPrimitive, byte(PrimitiveNull)))
	case PrimitiveTrue:
		e.scratch = append(e.scratch, makeHeader(BasicPrimitive, byte(PrimitiveTrue)))
	case PrimitiveFalse:
		e.scratch = append(e.scratch, makeHeader(BasicPrimitive, byte(PrimitiveFalse)))
	case PrimitiveInt8:
		e.scratch = append(e.scratch, makeHeader(BasicPrimitive, byte(PrimitiveInt8)), byte(v.i64))
	case PrimitiveInt16:
		e.scratch = append(e.scratch, makeHeader(BasicPrimitive, byte(PrimitiveInt16)), 0, 0)
		binary.LittleEndian.PutUint16(e.scratch[start+1:], uint16(v.i64))
	case PrimitiveInt32:
		e.scratch = append(e.scratch, makeHeader(BasicPrimitive, byte(PrimitiveInt32)), 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(e.scratch[start+1:], uint32(v.i64))
	case PrimitiveInt64:
		e.scratch = append(e.scratch, makeHeader(BasicPrimitive, byte(PrimitiveInt64)), 0, 0, 0, 0, 0, 0, 0, 0)
		binary.LittleEndian.PutUint64(e.scratch[start+1:], uint64(v.i64))
	case PrimitiveFloat:
		e.scratch = append(e.scratch, makeHeader(BasicPrimitive, byte(PrimitiveFloat)), 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(e.scratch[start+1:], math.Float32bits(float32(v.f64)))
	case PrimitiveDouble:
		e.scratch = append(e.scratch, makeHeader(BasicPrimitive, byte(PrimitiveDouble)), 0, 0, 0, 0, 0, 0, 0, 0)
		binary.LittleEndian.PutUint64(e.scratch[start+1:], math.Float64bits(v.f64))
	case PrimitiveString:
		s := v.str
		if len(s) <= 63 {
			e.scratch = append(e.scratch, makeHeader(BasicShortString, byte(len(s))))
			e.scratch = append(e.scratch, s...)
		} else {
			e.scratch = append(e.scratch, makeHeader(BasicPrimitive, byte(PrimitiveString)), 0, 0, 0, 0)
			binary.LittleEndian.PutUint32(e.scratch[start+1:], uint32(len(s)))
			e.scratch = append(e.scratch, s...)
		}
	case PrimitiveBinary:
		e.scratch = append(e.scratch, makeHeader(BasicPrimitive, byte(PrimitiveBinary)), 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(e.scratch[start+1:], uint32(len(v.bytes)))
		e.scratch = append(e.scratch, v.bytes...)
	case PrimitiveDate:
		e.scratch = append(e.scratch, makeHeader(BasicPrimitive, byte(PrimitiveDate)), 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(e.scratch[start+1:], uint32(v.i64))
	case PrimitiveTimestamp:
		e.scratch = append(e.scratch, makeHeader(BasicPrimitive, byte(PrimitiveTimestamp)), 0, 0, 0, 0, 0, 0, 0, 0)
		binary.LittleEndian.PutUint64(e.scratch[start+1:], uint64(v.i64))
	case PrimitiveTimestampNTZ:
		e.scratch = append(e.scratch, makeHeader(BasicPrimitive, byte(PrimitiveTimestampNTZ)), 0, 0, 0, 0, 0, 0, 0, 0)
		binary.LittleEndian.PutUint64(e.scratch[start+1:], uint64(v.i64))
	case PrimitiveTime:
		e.scratch = append(e.scratch, makeHeader(BasicPrimitive, byte(PrimitiveTime)), 0, 0, 0, 0, 0, 0, 0, 0)
		binary.LittleEndian.PutUint64(e.scratch[start+1:], uint64(v.i64))
	case PrimitiveTimestampNanos:
		e.scratch = append(e.scratch, makeHeader(BasicPrimitive, byte(PrimitiveTimestampNanos)), 0, 0, 0, 0, 0, 0, 0, 0)
		binary.LittleEndian.PutUint64(e.scratch[start+1:], uint64(v.i64))
	case PrimitiveTimestampNTZNanos:
		e.scratch = append(e.scratch, makeHeader(BasicPrimitive, byte(PrimitiveTimestampNTZNanos)), 0, 0, 0, 0, 0, 0, 0, 0)
		binary.LittleEndian.PutUint64(e.scratch[start+1:], uint64(v.i64))
	case PrimitiveUUID:
		e.scratch = append(e.scratch, makeHeader(BasicPrimitive, byte(PrimitiveUUID)))
		e.scratch = append(e.scratch, v.uuid[:]...)
	case PrimitiveDecimal4:
		e.scratch = append(e.scratch, makeHeader(BasicPrimitive, byte(PrimitiveDecimal4)), v.scale, 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(e.scratch[start+2:], uint32(v.i64))
	case PrimitiveDecimal8:
		e.scratch = append(e.scratch, makeHeader(BasicPrimitive, byte(PrimitiveDecimal8)), v.scale, 0, 0, 0, 0, 0, 0, 0, 0)
		binary.LittleEndian.PutUint64(e.scratch[start+2:], uint64(v.i64))
	case PrimitiveDecimal16:
		e.scratch = append(e.scratch, makeHeader(BasicPrimitive, byte(PrimitiveDecimal16)), v.scale)
		e.scratch = append(e.scratch, v.decimal16[:]...)
	default:
		e.scratch = append(e.scratch, makeHeader(BasicPrimitive, byte(PrimitiveNull)))
	}
	end = len(e.scratch)
	return start, end
}

func (e *encoder) encodeValue(v Value) (start, end int, err error) {
	switch v.basic {
	case BasicObject:
		return e.encodeValueObject(v.object)
	case BasicArray:
		return e.encodeValueArray(v.array)
	default:
		start, end = e.encodeValuePrimitive(v)
		return start, end, nil
	}
}

func (e *encoder) encodeValueArray(arr Array) (start, end int, err error) {
	n := len(arr.Elements)
	if n == 0 {
		return e.buildArrayBytes(nil)
	}

	savedStart := len(e.positions)
	if cap(e.positions) < savedStart+n {
		newCap := max(cap(e.positions)*2, savedStart+n)
		newP := make([]elementPos, len(e.positions), newCap)
		copy(newP, e.positions)
		e.positions = newP
	}
	e.positions = e.positions[:savedStart+n]
	positions := e.positions[savedStart : savedStart+n]

	for i := range n {
		pStart, pEnd, err := e.encodeValue(arr.Elements[i])
		if err != nil {
			e.positions = e.positions[:savedStart]
			return 0, 0, err
		}
		positions[i] = elementPos{start: pStart, end: pEnd}
	}

	resStart, resEnd, err := e.buildArrayBytes(positions)
	e.positions = e.positions[:savedStart]
	return resStart, resEnd, err
}

func (e *encoder) encodeValueObject(obj Object) (start, end int, err error) {
	n := len(obj.Fields)
	if n == 0 {
		return e.buildObjectBytes(nil)
	}

	savedStart := len(e.fields)
	if cap(e.fields) < savedStart+n {
		newCap := max(cap(e.fields)*2, savedStart+n)
		newF := make([]encodedField, len(e.fields), newCap)
		copy(newF, e.fields)
		e.fields = newF
	}
	e.fields = e.fields[:savedStart+n]
	entries := e.fields[savedStart : savedStart+n]

	for i, f := range obj.Fields {
		id := e.b.Add(f.Name)
		pStart, pEnd, err := e.encodeValue(f.Value)
		if err != nil {
			e.fields = e.fields[:savedStart]
			return 0, 0, err
		}
		entries[i] = encodedField{
			id:    id,
			name:  f.Name,
			start: pStart,
			end:   pEnd,
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].name < entries[j].name
	})

	resStart, resEnd, err := e.buildObjectBytes(entries)
	e.fields = e.fields[:savedStart]
	return resStart, resEnd, err
}

func (e *encoder) encodeReflect(rv reflect.Value) (start, end int, err error) {
	if !rv.IsValid() {
		start, end = e.encodeValuePrimitive(Null())
		return start, end, nil
	}

	// Dereference pointers/interfaces
	for rv.Kind() == reflect.Ptr || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			start, end = e.encodeValuePrimitive(Null())
			return start, end, nil
		}
		rv = rv.Elem()
	}

	// Check concrete types first
	if rv.Type() == reflect.TypeFor[uuid.UUID]() {
		start, end = e.encodeValuePrimitive(UUID(rv.Interface().(uuid.UUID)))
		return start, end, nil
	}
	if rv.Type() == reflect.TypeFor[time.Time]() {
		start, end = e.encodeValuePrimitive(timeToVariant(rv.Interface().(time.Time)))
		return start, end, nil
	}

	switch rv.Kind() {
	case reflect.Bool:
		start, end = e.encodeValuePrimitive(Bool(rv.Bool()))
		return start, end, nil
	case reflect.Int8:
		start, end = e.encodeValuePrimitive(Int8(int8(rv.Int())))
		return start, end, nil
	case reflect.Int16:
		start, end = e.encodeValuePrimitive(Int16(int16(rv.Int())))
		return start, end, nil
	case reflect.Int32:
		start, end = e.encodeValuePrimitive(Int32(int32(rv.Int())))
		return start, end, nil
	case reflect.Int64, reflect.Int:
		start, end = e.encodeValuePrimitive(Int64(rv.Int()))
		return start, end, nil
	case reflect.Uint8:
		start, end = e.encodeValuePrimitive(Int16(int16(rv.Uint())))
		return start, end, nil
	case reflect.Uint16:
		start, end = e.encodeValuePrimitive(Int32(int32(rv.Uint())))
		return start, end, nil
	case reflect.Uint32:
		start, end = e.encodeValuePrimitive(Int64(int64(rv.Uint())))
		return start, end, nil
	case reflect.Uint64, reflect.Uint:
		u := rv.Uint()
		if u > math.MaxInt64 {
			return 0, 0, fmt.Errorf("variant marshal: uint64 value %d overflows int64", u)
		}
		start, end = e.encodeValuePrimitive(Int64(int64(u)))
		return start, end, nil
	case reflect.Float32:
		start, end = e.encodeValuePrimitive(Float(float32(rv.Float())))
		return start, end, nil
	case reflect.Float64:
		start, end = e.encodeValuePrimitive(Double(rv.Float()))
		return start, end, nil
	case reflect.String:
		start, end = e.encodeValuePrimitive(String(rv.String()))
		return start, end, nil
	case reflect.Slice:
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			start, end = e.encodeValuePrimitive(Binary(rv.Bytes()))
			return start, end, nil
		}
		return e.encodeSliceArray(rv)
	case reflect.Array:
		// Byte arrays (including [16]byte) map to Binary. uuid.UUID is
		// handled above via its concrete type; without a logical UUID
		// annotation a fixed-length byte array is just bytes.
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			b := make([]byte, rv.Len())
			for i := range b {
				b[i] = byte(rv.Index(i).Uint())
			}
			start, end = e.encodeValuePrimitive(Binary(b))
			return start, end, nil
		}
		return e.encodeSliceArray(rv)
	case reflect.Map:
		return e.encodeMapObject(rv)
	case reflect.Struct:
		return e.encodeStructObject(rv)
	default:
		return 0, 0, fmt.Errorf("variant marshal: unsupported type %s", rv.Type())
	}
}

func (e *encoder) encodeSliceArray(rv reflect.Value) (start, end int, err error) {
	n := rv.Len()
	if n == 0 {
		return e.buildArrayBytes(nil)
	}

	savedStart := len(e.positions)
	if cap(e.positions) < savedStart+n {
		newCap := max(cap(e.positions)*2, savedStart+n)
		newP := make([]elementPos, len(e.positions), newCap)
		copy(newP, e.positions)
		e.positions = newP
	}
	e.positions = e.positions[:savedStart+n]
	positions := e.positions[savedStart : savedStart+n]

	for i := range n {
		pStart, pEnd, err := e.encodeReflect(rv.Index(i))
		if err != nil {
			e.positions = e.positions[:savedStart]
			return 0, 0, err
		}
		positions[i] = elementPos{start: pStart, end: pEnd}
	}

	resStart, resEnd, err := e.buildArrayBytes(positions)
	e.positions = e.positions[:savedStart]
	return resStart, resEnd, err
}

func (e *encoder) buildArrayBytes(positions []elementPos) (start, end int, err error) {
	n := len(positions)

	if testHookVerifyArrayLayout != nil {
		testHookVerifyArrayLayout(e, positions)
	}

	totalSize := 0
	for _, pos := range positions {
		totalSize += pos.end - pos.start
	}

	var offsetBuf [32]int
	var offsets []int
	if n+1 <= len(offsetBuf) {
		offsets = offsetBuf[:n+1]
	} else {
		if cap(e.offsets) < n+1 {
			e.offsets = make([]int, n+1)
		}
		offsets = e.offsets[:n+1]
	}
	off := 0
	for i, pos := range positions {
		offsets[i] = off
		off += pos.end - pos.start
	}
	offsets[n] = off

	offsetSzCode := offsetSizeCode(totalSize)
	offsetSz := offsetSize(offsetSzCode)

	isLarge := byte(0)
	numElemSize := 1
	if n > 255 {
		isLarge = 1
		numElemSize = 4
	}

	header := byte(BasicArray) | (offsetSzCode << 2) | (isLarge << 4)

	bufSize := 1 + numElemSize + (n+1)*offsetSz + totalSize
	if cap(e.temp) < bufSize {
		e.temp = make([]byte, bufSize)
	} else {
		e.temp = e.temp[:bufSize]
	}
	e.temp[0] = header

	posIdx := 1
	if isLarge == 1 {
		binary.LittleEndian.PutUint32(e.temp[posIdx:], uint32(n))
		posIdx += 4
	} else {
		e.temp[posIdx] = byte(n)
		posIdx += 1
	}

	for _, o := range offsets {
		writeUint(e.temp[posIdx:], o, offsetSz)
		posIdx += offsetSz
	}

	for _, p := range positions {
		copy(e.temp[posIdx:], e.scratch[p.start:p.end])
		posIdx += p.end - p.start
	}

	firstStart := len(e.scratch)
	if n > 0 {
		firstStart = positions[0].start
	}
	e.scratch = e.scratch[:firstStart]

	start = len(e.scratch)
	e.scratch = append(e.scratch, e.temp...)
	end = len(e.scratch)

	return start, end, nil
}

func (e *encoder) encodeMapObject(rv reflect.Value) (start, end int, err error) {
	if rv.IsNil() {
		start, end = e.encodeValuePrimitive(Null())
		return start, end, nil
	}

	n := rv.Len()
	if n == 0 {
		return e.buildObjectBytes(nil)
	}

	savedStart := len(e.fields)
	if cap(e.fields) < savedStart+n {
		newCap := max(cap(e.fields)*2, savedStart+n)
		newF := make([]encodedField, len(e.fields), newCap)
		copy(newF, e.fields)
		e.fields = newF
	}
	e.fields = e.fields[:savedStart+n]
	entries := e.fields[savedStart : savedStart+n]

	iter := rv.MapRange()
	idx := 0
	for iter.Next() {
		key := iter.Key()
		if key.Kind() != reflect.String {
			e.fields = e.fields[:savedStart]
			return 0, 0, fmt.Errorf("variant marshal: map key must be string, got %s", key.Type())
		}
		name := key.String()
		id := e.b.Add(name)

		pStart, pEnd, err := e.encodeReflect(iter.Value())
		if err != nil {
			e.fields = e.fields[:savedStart]
			return 0, 0, err
		}
		entries[idx] = encodedField{
			id:    id,
			name:  name,
			start: pStart,
			end:   pEnd,
		}
		idx++
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].name < entries[j].name
	})

	resStart, resEnd, err := e.buildObjectBytes(entries)
	e.fields = e.fields[:savedStart]
	return resStart, resEnd, err
}

func (e *encoder) encodeStructObject(rv reflect.Value) (start, end int, err error) {
	rt := rv.Type()
	numFields := rt.NumField()

	visibleCount := 0
	for i := range numFields {
		sf := rt.Field(i)
		if !sf.IsExported() {
			continue
		}
		name := fieldName(sf)
		if name == "-" {
			continue
		}
		visibleCount++
	}

	if visibleCount == 0 {
		return e.buildObjectBytes(nil)
	}

	savedStart := len(e.fields)
	if cap(e.fields) < savedStart+visibleCount {
		newCap := max(cap(e.fields)*2, savedStart+visibleCount)
		newF := make([]encodedField, len(e.fields), newCap)
		copy(newF, e.fields)
		e.fields = newF
	}
	e.fields = e.fields[:savedStart+visibleCount]
	entries := e.fields[savedStart : savedStart+visibleCount]

	idx := 0
	for i := range numFields {
		sf := rt.Field(i)
		if !sf.IsExported() {
			continue
		}
		name := fieldName(sf)
		if name == "-" {
			continue
		}

		id := e.b.Add(name)
		pStart, pEnd, err := e.encodeReflect(rv.Field(i))
		if err != nil {
			e.fields = e.fields[:savedStart]
			return 0, 0, err
		}
		entries[idx] = encodedField{
			id:    id,
			name:  name,
			start: pStart,
			end:   pEnd,
		}
		idx++
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].name < entries[j].name
	})

	resStart, resEnd, err := e.buildObjectBytes(entries)
	e.fields = e.fields[:savedStart]
	return resStart, resEnd, err
}

func (e *encoder) buildObjectBytes(entries []encodedField) (start, end int, err error) {
	n := len(entries)

	if testHookVerifyObjectLayout != nil {
		testHookVerifyObjectLayout(e, entries)
	}

	maxID := 0
	totalValueSize := 0
	for _, entry := range entries {
		if entry.id > maxID {
			maxID = entry.id
		}
		totalValueSize += entry.end - entry.start
	}

	var offsetBuf [32]int
	var valueOffsets []int
	if n+1 <= len(offsetBuf) {
		valueOffsets = offsetBuf[:n+1]
	} else {
		if cap(e.offsets) < n+1 {
			e.offsets = make([]int, n+1)
		}
		valueOffsets = e.offsets[:n+1]
	}
	off := 0
	for i, entry := range entries {
		valueOffsets[i] = off
		off += entry.end - entry.start
	}
	valueOffsets[n] = off

	fieldIDSizeCode := offsetSizeCode(maxID)
	fieldIDSize := offsetSize(fieldIDSizeCode)
	offsetSzCode := offsetSizeCode(totalValueSize)
	offsetSz := offsetSize(offsetSzCode)

	// Header byte layout for objects, per the spec's value encoding grammar
	// (object_header: is_large << 4 | field_id_size_minus_one << 2 |
	// field_offset_size_minus_one, shifted left 2 past the basic type):
	//
	//	bits 0-1: basic_type (2 = object)
	//	bits 2-3: field_offset_size_minus_one
	//	bits 4-5: field_id_size_minus_one
	//	bit 6:    is_large (0 = 1-byte num_elements, 1 = 4-byte)
	isLarge := byte(0)
	numElemSize := 1
	if n > 255 {
		isLarge = 1
		numElemSize = 4
	}

	header := byte(BasicObject) | (offsetSzCode << 2) | (fieldIDSizeCode << 4) | (isLarge << 6)

	totalSize := 1 + numElemSize + n*fieldIDSize + (n+1)*offsetSz + totalValueSize
	if cap(e.temp) < totalSize {
		e.temp = make([]byte, totalSize)
	} else {
		e.temp = e.temp[:totalSize]
	}
	e.temp[0] = header

	pos := 1
	if isLarge == 1 {
		binary.LittleEndian.PutUint32(e.temp[pos:], uint32(n))
		pos += 4
	} else {
		e.temp[pos] = byte(n)
		pos += 1
	}

	for _, entry := range entries {
		writeUint(e.temp[pos:], entry.id, fieldIDSize)
		pos += fieldIDSize
	}

	for _, offsetVal := range valueOffsets {
		writeUint(e.temp[pos:], offsetVal, offsetSz)
		pos += offsetSz
	}

	for _, entry := range entries {
		copy(e.temp[pos:], e.scratch[entry.start:entry.end])
		pos += entry.end - entry.start
	}

	firstStart := len(e.scratch)
	if n > 0 {
		firstStart = entries[0].start
		for _, entry := range entries {
			if entry.start < firstStart {
				firstStart = entry.start
			}
		}
	}
	e.scratch = e.scratch[:firstStart]

	start = len(e.scratch)
	e.scratch = append(e.scratch, e.temp...)
	end = len(e.scratch)

	return start, end, nil
}
