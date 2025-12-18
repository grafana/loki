package parquet

import (
	"reflect"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/format"
)

// Variant constructs a node of unshredded VARIANT logical type. It is a group with
// two required fields, "metadata" and "value", both byte arrays.
//
// Experimental: The specification for variants is still being developed and the type
// is not fully adopted. Support for this type is subject to change.
//
// Initial support does not attempt to process the variant data. So reading and writing
// data of this type behaves as if it were just a group with two byte array fields, as
// if the logical type annotation were absent. This may change in the future.
//
// https://github.com/apache/parquet-format/blob/master/LogicalTypes.md#variant
func Variant() Node {
	return variantNode{Group{"metadata": Required(Leaf(ByteArrayType)), "value": Required(Leaf(ByteArrayType))}}
}

// TODO: add ShreddedVariant(Node) function, to create a shredded variant
//  where the argument defines the type/structure of the shredded value(s).

type variantNode struct{ Group }

func (variantNode) Type() Type { return &variantType{} }

type variantType format.VariantType

func (t *variantType) String() string { return (*format.VariantType)(t).String() }

func (t *variantType) Kind() Kind { panic("cannot call Kind on parquet VARIANT type") }

func (t *variantType) Length() int { return 0 }

func (t *variantType) EstimateSize(int) int { return 0 }

func (t *variantType) EstimateNumValues(int) int { return 0 }

func (t *variantType) Compare(Value, Value) int {
	panic("cannot compare values on parquet VARIANT type")
}

func (t *variantType) ColumnOrder() *format.ColumnOrder { return nil }

func (t *variantType) PhysicalType() *format.Type { return nil }

func (t *variantType) LogicalType() *format.LogicalType {
	return &format.LogicalType{Variant: (*format.VariantType)(t)}
}

func (t *variantType) ConvertedType() *deprecated.ConvertedType { return nil }

func (t *variantType) NewColumnIndexer(int) ColumnIndexer {
	panic("create create column indexer from parquet VARIANT type")
}

func (t *variantType) NewDictionary(int, int, encoding.Values) Dictionary {
	panic("cannot create dictionary from parquet VARIANT type")
}

func (t *variantType) NewColumnBuffer(int, int) ColumnBuffer {
	panic("cannot create column buffer from parquet VARIANT type")
}

func (t *variantType) NewPage(int, int, encoding.Values) Page {
	panic("cannot create page from parquet VARIANT type")
}

func (t *variantType) NewValues(values []byte, _ []uint32) encoding.Values {
	panic("cannot create values from parquet VARIANT type")
}

func (t *variantType) Encode(_ []byte, _ encoding.Values, _ encoding.Encoding) ([]byte, error) {
	panic("cannot encode parquet VARIANT type")
}

func (t *variantType) Decode(_ encoding.Values, _ []byte, _ encoding.Encoding) (encoding.Values, error) {
	panic("cannot decode parquet VARIANT type")
}

func (t *variantType) EstimateDecodeSize(_ int, _ []byte, _ encoding.Encoding) int {
	panic("cannot estimate decode size of parquet VARIANT type")
}

func (t *variantType) AssignValue(reflect.Value, Value) error {
	panic("cannot assign value to a parquet VARIANT type")
}

func (t *variantType) ConvertValue(Value, Type) (Value, error) {
	panic("cannot convert value to a parquet VARIANT type")
}
