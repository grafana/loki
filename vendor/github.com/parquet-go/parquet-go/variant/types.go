package variant

// BasicType represents the basic type encoded in bits 0-1 of a value header byte.
type BasicType byte

const (
	BasicPrimitive   BasicType = 0
	BasicShortString BasicType = 1
	BasicObject      BasicType = 2
	BasicArray       BasicType = 3
)

// PrimitiveType represents the primitive type encoded in bits 2-7 of a
// value header byte when BasicType is BasicPrimitive.
type PrimitiveType byte

const (
	PrimitiveNull         PrimitiveType = 0
	PrimitiveTrue         PrimitiveType = 1
	PrimitiveFalse        PrimitiveType = 2
	PrimitiveInt8         PrimitiveType = 3
	PrimitiveInt16        PrimitiveType = 4
	PrimitiveInt32        PrimitiveType = 5
	PrimitiveInt64        PrimitiveType = 6
	PrimitiveDouble       PrimitiveType = 7
	PrimitiveDecimal4     PrimitiveType = 8
	PrimitiveDecimal8     PrimitiveType = 9
	PrimitiveDecimal16    PrimitiveType = 10
	PrimitiveDate         PrimitiveType = 11
	PrimitiveTimestamp    PrimitiveType = 12
	PrimitiveTimestampNTZ PrimitiveType = 13
	PrimitiveFloat        PrimitiveType = 14
	PrimitiveBinary       PrimitiveType = 15
	PrimitiveString       PrimitiveType = 16
	PrimitiveTime         PrimitiveType = 17
	PrimitiveUUID         PrimitiveType = 20
)

// primitiveSize returns the byte size of a primitive value's data portion,
// or -1 for variable-length types.
func primitiveSize(p PrimitiveType) int {
	switch p {
	case PrimitiveNull, PrimitiveTrue, PrimitiveFalse:
		return 0
	case PrimitiveInt8:
		return 1
	case PrimitiveInt16:
		return 2
	case PrimitiveInt32, PrimitiveFloat, PrimitiveDate:
		return 4
	case PrimitiveInt64, PrimitiveDouble, PrimitiveTimestamp, PrimitiveTimestampNTZ, PrimitiveTime:
		return 8
	case PrimitiveDecimal4:
		return 5 // 1 scale + 4 value
	case PrimitiveDecimal8:
		return 9 // 1 scale + 8 value
	case PrimitiveDecimal16:
		return 17 // 1 scale + 16 value
	case PrimitiveBinary, PrimitiveString:
		return -1 // variable length
	case PrimitiveUUID:
		return 16
	default:
		return -1
	}
}

// offsetSize returns the byte count for encoding integers with the given
// offset size code (0-3 → 1,2,3,4 bytes).
func offsetSize(code byte) int {
	return int(code) + 1
}

// offsetSizeCode returns the smallest offset size code that can represent
// the given maximum value.
func offsetSizeCode(maxVal int) byte {
	switch {
	case maxVal <= 0xFF:
		return 0
	case maxVal <= 0xFFFF:
		return 1
	case maxVal <= 0xFFFFFF:
		return 2
	default:
		return 3
	}
}
