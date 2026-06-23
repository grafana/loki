package variant

import (
	"encoding/binary"
	"math"
	"sort"
)

// Encode encodes a variant Value into its binary representation.
// Field names for objects are registered with the provided MetadataBuilder.
// Returns the encoded value bytes.
func Encode(b *MetadataBuilder, v Value) []byte {
	switch v.basic {
	case BasicObject:
		return encodeObject(b, v.object)
	case BasicArray:
		return encodeArray(b, v.array)
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
			// Use short string encoding
			buf := make([]byte, 1+len(s))
			buf[0] = makeHeader(BasicShortString, byte(len(s)))
			copy(buf[1:], s)
			return buf
		}
		// Long string: primitive string type + 4-byte length + data
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

func encodeObject(b *MetadataBuilder, obj Object) []byte {
	n := len(obj.Fields)

	// Register all field names and get their IDs
	type fieldEntry struct {
		id         int
		name       string
		valueBytes []byte
	}
	entries := make([]fieldEntry, n)
	for i, f := range obj.Fields {
		entries[i] = fieldEntry{
			id:         b.Add(f.Name),
			name:       f.Name,
			valueBytes: Encode(b, f.Value),
		}
	}

	// Sort by field name (as required by spec)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].name < entries[j].name
	})

	// Compute sizes
	maxID := 0
	for _, e := range entries {
		if e.id > maxID {
			maxID = e.id
		}
	}

	// Encode all values and compute offsets
	totalValueSize := 0
	valueOffsets := make([]int, n+1)
	for i, e := range entries {
		valueOffsets[i] = totalValueSize
		totalValueSize += len(e.valueBytes)
	}
	valueOffsets[n] = totalValueSize

	fieldIDSizeCode := offsetSizeCode(maxID)
	fieldIDSize := offsetSize(fieldIDSizeCode)
	offsetSzCode := offsetSizeCode(totalValueSize)
	offsetSz := offsetSize(offsetSzCode)

	// Header: basic_type=2 (object), value_header encodes num_fields, field_id_size, offset_size
	// Object header byte: bits 0-1 = BasicObject(2), bits 2-3 = field_id_size_minus_one,
	// bits 4-5 = offset_size_minus_one, bits 6-7 = unused
	// Then: num_elements (1 byte for small objects, but spec says offset_size bytes)
	//
	// Actually the spec says:
	// value_metadata byte: bits 0-1 = basic_type (2=object), bits 2-3 = field_id_size-1,
	// bits 4-5 = offset_size-1, bits 6-7 = is_large (0=1-byte num_elements, 1=4-byte)
	isLarge := byte(0)
	numElemSize := 1
	if n > 255 {
		isLarge = 1
		numElemSize = 4
	}

	header := byte(BasicObject) | (fieldIDSizeCode << 2) | (offsetSzCode << 4) | (isLarge << 6)

	// Total size: header(1) + num_elements + field_ids(n*fieldIDSize) + offsets((n+1)*offsetSz) + values
	totalSize := 1 + numElemSize + n*fieldIDSize + (n+1)*offsetSz + totalValueSize
	buf := make([]byte, totalSize)
	buf[0] = header

	pos := 1
	if isLarge == 1 {
		binary.LittleEndian.PutUint32(buf[pos:], uint32(n))
		pos += 4
	} else {
		buf[pos] = byte(n)
		pos += 1
	}

	// Write field IDs
	for _, e := range entries {
		writeUint(buf[pos:], e.id, fieldIDSize)
		pos += fieldIDSize
	}

	// Write offsets
	for _, off := range valueOffsets {
		writeUint(buf[pos:], off, offsetSz)
		pos += offsetSz
	}

	// Write values
	for _, e := range entries {
		copy(buf[pos:], e.valueBytes)
		pos += len(e.valueBytes)
	}

	return buf
}

func encodeArray(b *MetadataBuilder, arr Array) []byte {
	n := len(arr.Elements)

	// Encode all elements
	encodedElems := make([][]byte, n)
	totalSize := 0
	for i, e := range arr.Elements {
		encodedElems[i] = Encode(b, e)
		totalSize += len(encodedElems[i])
	}

	// Compute offsets
	offsets := make([]int, n+1)
	off := 0
	for i, enc := range encodedElems {
		offsets[i] = off
		off += len(enc)
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

	// Total: header(1) + num_elements + offsets((n+1)*offsetSz) + values
	bufSize := 1 + numElemSize + (n+1)*offsetSz + totalSize
	buf := make([]byte, bufSize)
	buf[0] = header

	pos := 1
	if isLarge == 1 {
		binary.LittleEndian.PutUint32(buf[pos:], uint32(n))
		pos += 4
	} else {
		buf[pos] = byte(n)
		pos += 1
	}

	// Write offsets
	for _, o := range offsets {
		writeUint(buf[pos:], o, offsetSz)
		pos += offsetSz
	}

	// Write values
	for _, enc := range encodedElems {
		copy(buf[pos:], enc)
		pos += len(enc)
	}

	return buf
}

// makeHeader constructs a value_metadata byte.
// For primitives: bits 0-1 = basic_type(0), bits 2-7 = primitive_type
// For short strings: bits 0-1 = basic_type(1), bits 2-7 = string length
func makeHeader(basic BasicType, valueHeader byte) byte {
	return byte(basic) | (valueHeader << 2)
}
