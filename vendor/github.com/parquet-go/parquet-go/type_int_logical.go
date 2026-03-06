package parquet

import (
	"fmt"
	"math/bits"
	"reflect"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/format"
)

// Int constructs a leaf node of signed integer logical type of the given bit
// width.
//
// The bit width must be one of 8, 16, 32, 64, or the function will panic.
func Int(bitWidth int) Node {
	return Leaf(integerType(bitWidth, &signedIntTypes))
}

// Uint constructs a leaf node of unsigned integer logical type of the given
// bit width.
//
// The bit width must be one of 8, 16, 32, 64, or the function will panic.
func Uint(bitWidth int) Node {
	return Leaf(integerType(bitWidth, &unsignedIntTypes))
}

func integerType(bitWidth int, types *[4]intType) *intType {
	switch bitWidth {
	case 8:
		return &types[0]
	case 16:
		return &types[1]
	case 32:
		return &types[2]
	case 64:
		return &types[3]
	default:
		panic(fmt.Sprintf("cannot create a %d bits parquet integer node", bitWidth))
	}
}

var signedIntTypes = [...]intType{
	{BitWidth: 8, IsSigned: true},
	{BitWidth: 16, IsSigned: true},
	{BitWidth: 32, IsSigned: true},
	{BitWidth: 64, IsSigned: true},
}

var unsignedIntTypes = [...]intType{
	{BitWidth: 8, IsSigned: false},
	{BitWidth: 16, IsSigned: false},
	{BitWidth: 32, IsSigned: false},
	{BitWidth: 64, IsSigned: false},
}

var signedLogicalIntTypes = [...]format.LogicalType{
	{Integer: (*format.IntType)(&signedIntTypes[0])},
	{Integer: (*format.IntType)(&signedIntTypes[1])},
	{Integer: (*format.IntType)(&signedIntTypes[2])},
	{Integer: (*format.IntType)(&signedIntTypes[3])},
}

var unsignedLogicalIntTypes = [...]format.LogicalType{
	{Integer: (*format.IntType)(&unsignedIntTypes[0])},
	{Integer: (*format.IntType)(&unsignedIntTypes[1])},
	{Integer: (*format.IntType)(&unsignedIntTypes[2])},
	{Integer: (*format.IntType)(&unsignedIntTypes[3])},
}

type intType format.IntType

func (t *intType) baseType() Type {
	if t.IsSigned {
		if t.BitWidth == 64 {
			return int64Type{}
		} else {
			return int32Type{}
		}
	} else {
		if t.BitWidth == 64 {
			return uint64Type{}
		} else {
			return uint32Type{}
		}
	}
}

func (t *intType) String() string { return (*format.IntType)(t).String() }

func (t *intType) Kind() Kind { return t.baseType().Kind() }

func (t *intType) Length() int { return int(t.BitWidth) }

func (t *intType) EstimateSize(n int) int { return (int(t.BitWidth) / 8) * n }

func (t *intType) EstimateNumValues(n int) int { return n / (int(t.BitWidth) / 8) }

func (t *intType) Compare(a, b Value) int {
	// This code is similar to t.baseType().Compare(a,b) but comparison methods
	// tend to be invoked a lot (e.g. when sorting) so avoiding the interface
	// indirection in this case yields much better throughput in some cases.
	if t.BitWidth == 64 {
		i1 := a.int64()
		i2 := b.int64()
		if t.IsSigned {
			return compareInt64(i1, i2)
		} else {
			return compareUint64(uint64(i1), uint64(i2))
		}
	} else {
		i1 := a.int32()
		i2 := b.int32()
		if t.IsSigned {
			return compareInt32(i1, i2)
		} else {
			return compareUint32(uint32(i1), uint32(i2))
		}
	}
}

func (t *intType) ColumnOrder() *format.ColumnOrder { return t.baseType().ColumnOrder() }

func (t *intType) PhysicalType() *format.Type { return t.baseType().PhysicalType() }

func (t *intType) LogicalType() *format.LogicalType {
	switch t {
	case &signedIntTypes[0]:
		return &signedLogicalIntTypes[0]
	case &signedIntTypes[1]:
		return &signedLogicalIntTypes[1]
	case &signedIntTypes[2]:
		return &signedLogicalIntTypes[2]
	case &signedIntTypes[3]:
		return &signedLogicalIntTypes[3]
	case &unsignedIntTypes[0]:
		return &unsignedLogicalIntTypes[0]
	case &unsignedIntTypes[1]:
		return &unsignedLogicalIntTypes[1]
	case &unsignedIntTypes[2]:
		return &unsignedLogicalIntTypes[2]
	case &unsignedIntTypes[3]:
		return &unsignedLogicalIntTypes[3]
	default:
		return &format.LogicalType{Integer: (*format.IntType)(t)}
	}
}

func (t *intType) ConvertedType() *deprecated.ConvertedType {
	convertedType := bits.Len8(uint8(t.BitWidth)/8) - 1 // 8=>0, 16=>1, 32=>2, 64=>4
	if t.IsSigned {
		convertedType += int(deprecated.Int8)
	} else {
		convertedType += int(deprecated.Uint8)
	}
	return &convertedTypes[convertedType]
}

func (t *intType) NewColumnIndexer(sizeLimit int) ColumnIndexer {
	return t.baseType().NewColumnIndexer(sizeLimit)
}

func (t *intType) NewColumnBuffer(columnIndex, numValues int) ColumnBuffer {
	return t.baseType().NewColumnBuffer(columnIndex, numValues)
}

func (t *intType) NewDictionary(columnIndex, numValues int, data encoding.Values) Dictionary {
	return t.baseType().NewDictionary(columnIndex, numValues, data)
}

func (t *intType) NewPage(columnIndex, numValues int, data encoding.Values) Page {
	return t.baseType().NewPage(columnIndex, numValues, data)
}

func (t *intType) NewValues(values []byte, offsets []uint32) encoding.Values {
	return t.baseType().NewValues(values, offsets)
}

func (t *intType) Encode(dst []byte, src encoding.Values, enc encoding.Encoding) ([]byte, error) {
	return t.baseType().Encode(dst, src, enc)
}

func (t *intType) Decode(dst encoding.Values, src []byte, enc encoding.Encoding) (encoding.Values, error) {
	return t.baseType().Decode(dst, src, enc)
}

func (t *intType) EstimateDecodeSize(numValues int, src []byte, enc encoding.Encoding) int {
	return t.baseType().EstimateDecodeSize(numValues, src, enc)
}

func (t *intType) AssignValue(dst reflect.Value, src Value) error {
	if t.BitWidth == 64 {
		return int64Type{}.AssignValue(dst, src)
	} else {
		return int32Type{}.AssignValue(dst, src)
	}
}

func (t *intType) ConvertValue(val Value, typ Type) (Value, error) {
	if t.BitWidth == 64 {
		return int64Type{}.ConvertValue(val, typ)
	} else {
		return int32Type{}.ConvertValue(val, typ)
	}
}
