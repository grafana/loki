package parquet

import (
	"reflect"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/format"
)

// Map constructs a node of MAP logical type.
//
// https://github.com/apache/parquet-format/blob/master/LogicalTypes.md#maps
func Map(key, value Node) Node {
	return mapNode{Group{
		"key_value": Repeated(Group{
			"key":   Required(key),
			"value": value,
		}),
	}}
}

type mapNode struct{ Group }

func (mapNode) Type() Type { return &mapType{} }

type mapType format.MapType

func (t *mapType) String() string { return (*format.MapType)(t).String() }

func (t *mapType) Kind() Kind { panic("cannot call Kind on parquet MAP type") }

func (t *mapType) Length() int { return 0 }

func (t *mapType) EstimateSize(int) int { return 0 }

func (t *mapType) EstimateNumValues(int) int { return 0 }

func (t *mapType) Compare(Value, Value) int { panic("cannot compare values on parquet MAP type") }

func (t *mapType) ColumnOrder() *format.ColumnOrder { return nil }

func (t *mapType) PhysicalType() *format.Type { return nil }

func (t *mapType) LogicalType() *format.LogicalType {
	return &format.LogicalType{Map: (*format.MapType)(t)}
}

func (t *mapType) ConvertedType() *deprecated.ConvertedType {
	return &convertedTypes[deprecated.Map]
}

func (t *mapType) NewColumnIndexer(int) ColumnIndexer {
	panic("create create column indexer from parquet MAP type")
}

func (t *mapType) NewDictionary(int, int, encoding.Values) Dictionary {
	panic("cannot create dictionary from parquet MAP type")
}

func (t *mapType) NewColumnBuffer(int, int) ColumnBuffer {
	panic("cannot create column buffer from parquet MAP type")
}

func (t *mapType) NewPage(int, int, encoding.Values) Page {
	panic("cannot create page from parquet MAP type")
}

func (t *mapType) NewValues(values []byte, _ []uint32) encoding.Values {
	panic("cannot create values from parquet MAP type")
}

func (t *mapType) Encode(_ []byte, _ encoding.Values, _ encoding.Encoding) ([]byte, error) {
	panic("cannot encode parquet MAP type")
}

func (t *mapType) Decode(_ encoding.Values, _ []byte, _ encoding.Encoding) (encoding.Values, error) {
	panic("cannot decode parquet MAP type")
}

func (t *mapType) EstimateDecodeSize(_ int, _ []byte, _ encoding.Encoding) int {
	panic("cannot estimate decode size of parquet MAP type")
}

func (t *mapType) AssignValue(reflect.Value, Value) error {
	panic("cannot assign value to a parquet MAP type")
}

func (t *mapType) ConvertValue(Value, Type) (Value, error) {
	panic("cannot convert value to a parquet MAP type")
}
