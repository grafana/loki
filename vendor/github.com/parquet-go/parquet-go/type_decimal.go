package parquet

import (
	"log"
	"math"
	"math/big"
	"reflect"
	"strconv"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/format"
)

// Decimal constructs a leaf node of decimal logical type with the given
// scale, precision, and underlying type.
//
// https://github.com/apache/parquet-format/blob/master/LogicalTypes.md#decimal
func Decimal(scale, precision int, typ Type) Node {
	switch typ.Kind() {
	case Int32:
		if precision < 1 || precision > 9 {
			panic("DECIMAL annotated with Int32 must have precision >= 1 and <= 9, got " + strconv.Itoa(precision))
		}
	case Int64:
		if precision < 1 || precision > 18 {
			panic("DECIMAL annotated with Int32 must have precision >= 1 and <= 9, got " + strconv.Itoa(precision))
		}
		if precision < 10 {
			log.Printf("WARNING: DECIMAL annotated with Int64 should have a precision >= 10, got %d", precision)
		}
	case ByteArray, FixedLenByteArray:
	default:
		panic("DECIMAL node must annotate Int32, Int64, ByteArray or FixedLenByteArray but got " + typ.String())
	}
	return Leaf(&decimalType{
		decimal: format.DecimalType{
			Scale:     int32(scale),
			Precision: int32(precision),
		},
		Type: typ,
	})
}

type decimalType struct {
	decimal format.DecimalType
	Type
}

func (t *decimalType) String() string { return t.decimal.String() }

func (t *decimalType) LogicalType() *format.LogicalType {
	return &format.LogicalType{Decimal: &t.decimal}
}

func (t *decimalType) ConvertedType() *deprecated.ConvertedType {
	return &convertedTypes[deprecated.Decimal]
}

func (t *decimalType) AssignValue(dst reflect.Value, src Value) error {
	switch t.Type {
	case Int32Type:
		switch dst.Kind() {
		case reflect.Int32:
			dst.SetInt(int64(src.int32()))
		default:
			dst.Set(reflect.ValueOf(float32(src.int32()) / float32(math.Pow10(int(t.decimal.Scale)))))
		}
	case Int64Type:
		switch dst.Kind() {
		case reflect.Int64:
			dst.SetInt(src.int64())
		default:
			dst.Set(reflect.ValueOf(float64(src.int64()) / math.Pow10(int(t.decimal.Scale))))
		}
	default:
		// ByteArray and FixedLenByteArray
		if t.Type.Kind() != ByteArray && t.Type.Kind() != FixedLenByteArray {
			return nil
		}
		data := src.ByteArray()
		val := new(big.Int)
		if len(data) > 0 && data[0]&0x80 != 0 {
			// Negative number: convert from two's complement
			tmp := make([]byte, len(data))
			for i, b := range data {
				tmp[i] = ^b
			}
			val.SetBytes(tmp)
			val.Add(val, big.NewInt(1))
			val.Neg(val)
		} else {
			val.SetBytes(data)
		}
		// Use enough precision to represent the decimal value accurately
		// precision * log2(10) ≈ precision * 3.32, round up generously
		prec := max(uint(t.decimal.Precision)*4+64, 192)
		f := new(big.Float).SetPrec(prec).SetInt(val)
		scaleFactor := new(big.Float).SetPrec(prec)
		scaleFactor.SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(t.decimal.Scale)), nil))
		f.Quo(f, scaleFactor)
		dst.Set(reflect.ValueOf(f))
	}
	return nil
}
