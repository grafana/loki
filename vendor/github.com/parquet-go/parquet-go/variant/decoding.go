package variant

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// Decode decodes a variant value from its binary representation using the
// given metadata dictionary.
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
		v := Value{basic: BasicShortString, str: string(data[1 : 1+length])}
		return v, 1 + length, nil
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
		length := int(binary.LittleEndian.Uint32(data[:4]))
		if len(data) < 4+length {
			return Null(), 0, fmt.Errorf("variant value: string length %d exceeds data", length)
		}
		return String(string(data[4 : 4+length])), 5 + length, nil
	case PrimitiveBinary:
		if len(data) < 4 {
			return Null(), 0, errors.New("variant value: not enough data for binary length")
		}
		length := int(binary.LittleEndian.Uint32(data[:4]))
		if len(data) < 4+length {
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
	fieldIDSizeCode := (header >> 2) & 0x03
	offsetSzCode := (header >> 4) & 0x03
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

	// Value data starts at pos
	valueDataStart := pos

	fields := make([]Field, numElements)
	for i := range numElements {
		name, err := m.Lookup(fieldIDs[i])
		if err != nil {
			return Null(), 0, fmt.Errorf("variant value: object field %d: %w", i, err)
		}

		valueStart := valueDataStart + offsets[i]
		valueEnd := valueDataStart + offsets[i+1]
		if valueStart > len(data) || valueEnd > len(data) || valueStart > valueEnd {
			return Null(), 0, fmt.Errorf("variant value: object field %d: invalid value offset", i)
		}

		v, _, err := decodeValue(m, data[valueStart:valueEnd])
		if err != nil {
			return Null(), 0, fmt.Errorf("variant value: object field %q: %w", name, err)
		}

		fields[i] = Field{Name: name, Value: v}
	}

	totalConsumed := 1 + valueDataStart + offsets[numElements] // +1 for header byte
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
		if elemStart > len(data) || elemEnd > len(data) || elemStart > elemEnd {
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
