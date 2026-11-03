package variant

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"unicode/utf8"
)

// Decode decodes a variant value from its binary representation using the
// given metadata dictionary.
//
// Decode rejects input the spec declares invalid: strings that are not valid
// UTF-8, objects with duplicate field names, and structures whose declared
// sizes exceed the data. It accepts input that is non-canonical but
// unambiguous, as the spec directs for readers: reserved header bits are
// ignored, object fields need not be listed in lexicographic order, and
// bytes past the end of the value are ignored (values are self-delimiting).
func Decode(m Metadata, data []byte) (Value, error) {
	if len(data) == 0 {
		return Null(), errors.New("variant value: empty data")
	}
	v, _, err := decodeValue(m, data)
	return v, err
}

// decodeValue decodes a value and returns the number of bytes consumed.
func decodeValue(m Metadata, data []byte) (Value, int, error) {
	if len(data) == 0 {
		return Null(), 0, errors.New("variant value: unexpected end of data")
	}

	header := data[0]
	basic := BasicType(header & 0x03)
	valueHeader := header >> 2

	switch basic {
	case BasicPrimitive:
		return decodePrimitive(PrimitiveType(valueHeader), data[1:])
	case BasicShortString:
		length := int(valueHeader)
		if len(data) < 1+length {
			return Null(), 0, fmt.Errorf("variant value: short string length %d exceeds data", length)
		}
		if !utf8.Valid(data[1 : 1+length]) {
			return Null(), 0, errors.New("variant value: short string is not valid UTF-8")
		}
		// Normalize to the string primitive so the value re-encodes
		// correctly. VariantEncoding.md: "The 'short string' basic type
		// may be used as an optimization to fold string length into the
		// type byte for strings less than 64 bytes. It is semantically
		// identical to the 'string' primitive type."
		return String(string(data[1 : 1+length])), 1 + length, nil
	case BasicObject:
		return decodeObject(m, header, data[1:])
	case BasicArray:
		return decodeArray(m, header, data[1:])
	default:
		return Null(), 0, fmt.Errorf("variant value: unknown basic type %d", basic)
	}
}

func decodePrimitive(pt PrimitiveType, data []byte) (Value, int, error) {
	switch pt {
	case PrimitiveNull:
		return Null(), 1, nil
	case PrimitiveTrue:
		return Bool(true), 1, nil
	case PrimitiveFalse:
		return Bool(false), 1, nil
	case PrimitiveInt8:
		if len(data) < 1 {
			return Null(), 0, errors.New("variant value: not enough data for int8")
		}
		return Int8(int8(data[0])), 2, nil
	case PrimitiveInt16:
		if len(data) < 2 {
			return Null(), 0, errors.New("variant value: not enough data for int16")
		}
		return Int16(int16(binary.LittleEndian.Uint16(data[:2]))), 3, nil
	case PrimitiveInt32:
		if len(data) < 4 {
			return Null(), 0, errors.New("variant value: not enough data for int32")
		}
		return Int32(int32(binary.LittleEndian.Uint32(data[:4]))), 5, nil
	case PrimitiveInt64:
		if len(data) < 8 {
			return Null(), 0, errors.New("variant value: not enough data for int64")
		}
		return Int64(int64(binary.LittleEndian.Uint64(data[:8]))), 9, nil
	case PrimitiveFloat:
		if len(data) < 4 {
			return Null(), 0, errors.New("variant value: not enough data for float")
		}
		return Float(math.Float32frombits(binary.LittleEndian.Uint32(data[:4]))), 5, nil
	case PrimitiveDouble:
		if len(data) < 8 {
			return Null(), 0, errors.New("variant value: not enough data for double")
		}
		return Double(math.Float64frombits(binary.LittleEndian.Uint64(data[:8]))), 9, nil
	case PrimitiveString:
		if len(data) < 4 {
			return Null(), 0, errors.New("variant value: not enough data for string length")
		}
		// On 32-bit platforms a 4-byte length above math.MaxInt32
		// overflows int and is negative, and 4+length can overflow the
		// same way, so the bounds check avoids the addition. Neither
		// overflow can happen on 64-bit platforms.
		length := int(binary.LittleEndian.Uint32(data[:4]))
		if length < 0 || length > len(data)-4 {
			return Null(), 0, fmt.Errorf("variant value: string length %d exceeds data", length)
		}
		// VariantEncoding.md: "All strings within the Variant binary
		// format must be UTF-8 encoded. This includes the dictionary key
		// string values, the 'short string' values, and the 'long string'
		// values." (Short strings and dictionary entries are validated at
		// their own decode sites.)
		if !utf8.Valid(data[4 : 4+length]) {
			return Null(), 0, errors.New("variant value: string is not valid UTF-8")
		}
		return String(string(data[4 : 4+length])), 5 + length, nil
	case PrimitiveBinary:
		if len(data) < 4 {
			return Null(), 0, errors.New("variant value: not enough data for binary length")
		}
		length := int(binary.LittleEndian.Uint32(data[:4]))
		// The check is phrased for 32-bit overflow safety; see the string
		// case above.
		if length < 0 || length > len(data)-4 {
			return Null(), 0, fmt.Errorf("variant value: binary length %d exceeds data", length)
		}
		b := make([]byte, length)
		copy(b, data[4:4+length])
		return Binary(b), 5 + length, nil
	case PrimitiveDate:
		if len(data) < 4 {
			return Null(), 0, errors.New("variant value: not enough data for date")
		}
		return Date(int32(binary.LittleEndian.Uint32(data[:4]))), 5, nil
	case PrimitiveTimestamp:
		if len(data) < 8 {
			return Null(), 0, errors.New("variant value: not enough data for timestamp")
		}
		return Timestamp(int64(binary.LittleEndian.Uint64(data[:8]))), 9, nil
	case PrimitiveTimestampNTZ:
		if len(data) < 8 {
			return Null(), 0, errors.New("variant value: not enough data for timestamp_ntz")
		}
		return TimestampNTZ(int64(binary.LittleEndian.Uint64(data[:8]))), 9, nil
	case PrimitiveTime:
		if len(data) < 8 {
			return Null(), 0, errors.New("variant value: not enough data for time")
		}
		return Time(int64(binary.LittleEndian.Uint64(data[:8]))), 9, nil
	case PrimitiveTimestampNanos:
		if len(data) < 8 {
			return Null(), 0, errors.New("variant value: not enough data for timestamp nanos")
		}
		return TimestampNanos(int64(binary.LittleEndian.Uint64(data[:8]))), 9, nil
	case PrimitiveTimestampNTZNanos:
		if len(data) < 8 {
			return Null(), 0, errors.New("variant value: not enough data for timestamp_ntz nanos")
		}
		return TimestampNTZNanos(int64(binary.LittleEndian.Uint64(data[:8]))), 9, nil
	case PrimitiveUUID:
		if len(data) < 16 {
			return Null(), 0, errors.New("variant value: not enough data for uuid")
		}
		var u [16]byte
		copy(u[:], data[:16])
		return UUID(u), 17, nil
	case PrimitiveDecimal4:
		if len(data) < 5 {
			return Null(), 0, errors.New("variant value: not enough data for decimal4")
		}
		scale := data[0]
		val := int32(binary.LittleEndian.Uint32(data[1:5]))
		return Decimal4(val, scale), 6, nil
	case PrimitiveDecimal8:
		if len(data) < 9 {
			return Null(), 0, errors.New("variant value: not enough data for decimal8")
		}
		scale := data[0]
		val := int64(binary.LittleEndian.Uint64(data[1:9]))
		return Decimal8(val, scale), 10, nil
	case PrimitiveDecimal16:
		if len(data) < 17 {
			return Null(), 0, errors.New("variant value: not enough data for decimal16")
		}
		scale := data[0]
		var val [16]byte
		copy(val[:], data[1:17])
		return Decimal16(val, scale), 18, nil
	default:
		return Null(), 0, fmt.Errorf("variant value: unknown primitive type %d", pt)
	}
}

func decodeObject(m Metadata, header byte, data []byte) (Value, int, error) {
	// Object header byte layout (see encodeObject): bits 2-3 hold
	// field_offset_size_minus_one, bits 4-5 hold field_id_size_minus_one,
	// bit 6 holds is_large.
	offsetSzCode := (header >> 2) & 0x03
	fieldIDSizeCode := (header >> 4) & 0x03
	isLarge := (header >> 6) & 0x01

	fieldIDSize := offsetSize(fieldIDSizeCode)
	offsetSz := offsetSize(offsetSzCode)

	pos := 0

	// Read num_elements
	var numElements int
	if isLarge == 1 {
		if len(data) < 4 {
			return Null(), 0, errors.New("variant value: not enough data for object num_elements")
		}
		numElements = int(binary.LittleEndian.Uint32(data[:4]))
		pos += 4
	} else {
		if len(data) < 1 {
			return Null(), 0, errors.New("variant value: not enough data for object num_elements")
		}
		numElements = int(data[0])
		pos += 1
	}

	// Validate the declared element count against the available data
	// before allocating, to reject corrupt inputs with absurd sizes.
	// numElements < 0 happens only on 32-bit platforms, where a 4-byte
	// count above math.MaxInt32 overflows int.
	if remaining := len(data) - pos; numElements < 0 ||
		remaining/(fieldIDSize+offsetSz) < numElements {
		return Null(), 0, fmt.Errorf("variant value: object element count %d exceeds data", numElements)
	}

	// Read field IDs
	fieldIDs := make([]int, numElements)
	for i := range numElements {
		v, n, err := readUint(data[pos:], fieldIDSize)
		if err != nil {
			return Null(), 0, fmt.Errorf("variant value: reading object field id %d: %w", i, err)
		}
		fieldIDs[i] = v
		pos += n
	}

	// Read offsets (numElements+1)
	offsets := make([]int, numElements+1)
	for i := range numElements + 1 {
		v, n, err := readUint(data[pos:], offsetSz)
		if err != nil {
			return Null(), 0, fmt.Errorf("variant value: reading object offset %d: %w", i, err)
		}
		offsets[i] = v
		pos += n
	}

	// Value data starts at pos. The last offset is the total size of the
	// value data region. Fields must NOT be sliced as [offset[i],
	// offset[i+1]) — per VariantEncoding.md, "the actual value entries do
	// not need to be in any particular order. This implies that the
	// field_offset values may not be monotonically increasing." (Spark
	// writes such objects.) Each value is self-delimiting; the offset only
	// determines where it starts.
	valueDataStart := pos
	valueDataEnd := valueDataStart + offsets[numElements]
	// valueDataEnd < valueDataStart rejects a last offset that overflowed
	// int to a negative value on 32-bit platforms.
	if valueDataEnd > len(data) || valueDataEnd < valueDataStart {
		return Null(), 0, errors.New("variant value: object value data exceeds input")
	}

	fields := make([]Field, numElements)
	// VariantEncoding.md: "Field names are required to be unique for each
	// object. It is an error for an object to contain two fields with the
	// same name, whether or not they have distinct dictionary IDs." (The
	// last clause is why the check compares names, not field IDs.)
	//
	// The spec also requires fields to be listed in lexicographic order of
	// their names, so in well-formed input a duplicate is adjacent to its
	// twin and one comparison against the previous name detects it. The
	// map is only allocated for inputs that violate the sort order (which
	// are otherwise accepted, for compatibility with lenient writers).
	var seen map[string]struct{}
	for i := range numElements {
		name, err := m.Lookup(fieldIDs[i])
		if err != nil {
			return Null(), 0, fmt.Errorf("variant value: object field %d: %w", i, err)
		}
		if seen == nil && i > 0 && name <= fields[i-1].Name {
			if name == fields[i-1].Name {
				return Null(), 0, fmt.Errorf("variant value: duplicate object field %q", name)
			}
			seen = make(map[string]struct{}, numElements)
			for _, f := range fields[:i] {
				seen[f.Name] = struct{}{}
			}
		}
		if seen != nil {
			if _, dup := seen[name]; dup {
				return Null(), 0, fmt.Errorf("variant value: duplicate object field %q", name)
			}
			seen[name] = struct{}{}
		}

		valueStart := valueDataStart + offsets[i]
		if valueStart < valueDataStart || valueStart > valueDataEnd {
			return Null(), 0, fmt.Errorf("variant value: object field %d: invalid value offset", i)
		}

		v, _, err := decodeValue(m, data[valueStart:valueDataEnd])
		if err != nil {
			return Null(), 0, fmt.Errorf("variant value: object field %q: %w", name, err)
		}

		fields[i] = Field{Name: name, Value: v}
	}

	totalConsumed := 1 + valueDataEnd // +1 for header byte
	return MakeObject(fields), totalConsumed, nil
}

func decodeArray(m Metadata, header byte, data []byte) (Value, int, error) {
	offsetSzCode := (header >> 2) & 0x03
	isLarge := (header >> 4) & 0x01

	offsetSz := offsetSize(offsetSzCode)

	pos := 0

	// Read num_elements
	var numElements int
	if isLarge == 1 {
		if len(data) < 4 {
			return Null(), 0, errors.New("variant value: not enough data for array num_elements")
		}
		numElements = int(binary.LittleEndian.Uint32(data[:4]))
		pos += 4
	} else {
		if len(data) < 1 {
			return Null(), 0, errors.New("variant value: not enough data for array num_elements")
		}
		numElements = int(data[0])
		pos += 1
	}

	// Validate the declared element count against the available data
	// before allocating, to reject corrupt inputs with absurd sizes.
	// numElements < 0 happens only on 32-bit platforms, where a 4-byte
	// count above math.MaxInt32 overflows int; the comparison is phrased
	// as <= numElements rather than < numElements+1 (there are
	// numElements+1 offsets) so the addition cannot overflow the same way.
	if remaining := len(data) - pos; numElements < 0 ||
		remaining/offsetSz <= numElements {
		return Null(), 0, fmt.Errorf("variant value: array element count %d exceeds data", numElements)
	}

	// Read offsets (numElements+1)
	offsets := make([]int, numElements+1)
	for i := range numElements + 1 {
		v, n, err := readUint(data[pos:], offsetSz)
		if err != nil {
			return Null(), 0, fmt.Errorf("variant value: reading array offset %d: %w", i, err)
		}
		offsets[i] = v
		pos += n
	}

	// Value data starts at pos
	valueDataStart := pos

	elements := make([]Value, numElements)
	for i := range numElements {
		elemStart := valueDataStart + offsets[i]
		elemEnd := valueDataStart + offsets[i+1]
		// elemStart < 0 happens only on 32-bit platforms, where a 4-byte
		// offset above math.MaxInt32 overflows int; without the check the
		// slice below would panic. The remaining conditions imply
		// elemStart <= elemEnd <= len(data).
		if elemStart < 0 || elemEnd > len(data) || elemStart > elemEnd {
			return Null(), 0, fmt.Errorf("variant value: array element %d: invalid offset", i)
		}

		v, _, err := decodeValue(m, data[elemStart:elemEnd])
		if err != nil {
			return Null(), 0, fmt.Errorf("variant value: array element %d: %w", i, err)
		}

		elements[i] = v
	}

	totalConsumed := 1 + valueDataStart + offsets[numElements] // +1 for header byte
	return MakeArray(elements), totalConsumed, nil
}
