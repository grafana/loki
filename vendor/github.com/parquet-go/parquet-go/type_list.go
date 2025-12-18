package parquet

import (
	"reflect"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/format"
)

// List constructs a node of LIST logical type.
//
// https://github.com/apache/parquet-format/blob/master/LogicalTypes.md#lists
func List(of Node) Node {
	return listNode{Group{"list": Repeated(Group{"element": of})}}
}

type listNode struct{ Group }

func (listNode) Type() Type { return &listType{} }

type listType format.ListType

func (t *listType) String() string { return (*format.ListType)(t).String() }

func (t *listType) Kind() Kind { panic("cannot call Kind on parquet LIST type") }

func (t *listType) Length() int { return 0 }

func (t *listType) EstimateSize(int) int { return 0 }

func (t *listType) EstimateNumValues(int) int { return 0 }

func (t *listType) Compare(Value, Value) int { panic("cannot compare values on parquet LIST type") }

func (t *listType) ColumnOrder() *format.ColumnOrder { return nil }

func (t *listType) PhysicalType() *format.Type { return nil }

func (t *listType) LogicalType() *format.LogicalType {
	return &format.LogicalType{List: (*format.ListType)(t)}
}

func (t *listType) ConvertedType() *deprecated.ConvertedType {
	return &convertedTypes[deprecated.List]
}

func (t *listType) NewColumnIndexer(int) ColumnIndexer {
	panic("create create column indexer from parquet LIST type")
}

func (t *listType) NewDictionary(int, int, encoding.Values) Dictionary {
	panic("cannot create dictionary from parquet LIST type")
}

func (t *listType) NewColumnBuffer(int, int) ColumnBuffer {
	panic("cannot create column buffer from parquet LIST type")
}

func (t *listType) NewPage(int, int, encoding.Values) Page {
	panic("cannot create page from parquet LIST type")
}

func (t *listType) NewValues(values []byte, _ []uint32) encoding.Values {
	panic("cannot create values from parquet LIST type")
}

func (t *listType) Encode(_ []byte, _ encoding.Values, _ encoding.Encoding) ([]byte, error) {
	panic("cannot encode parquet LIST type")
}

func (t *listType) Decode(_ encoding.Values, _ []byte, _ encoding.Encoding) (encoding.Values, error) {
	panic("cannot decode parquet LIST type")
}

func (t *listType) EstimateDecodeSize(_ int, _ []byte, _ encoding.Encoding) int {
	panic("cannot estimate decode size of parquet LIST type")
}

func (t *listType) AssignValue(reflect.Value, Value) error {
	panic("cannot assign value to a parquet LIST type")
}

func (t *listType) ConvertValue(Value, Type) (Value, error) {
	panic("cannot convert value to a parquet LIST type")
}
