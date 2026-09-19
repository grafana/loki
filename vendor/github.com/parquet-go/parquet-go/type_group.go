package parquet

import (
	"reflect"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/format"
)

type groupType struct{}

func (groupType) String() string { return "group" }

func (groupType) Kind() Kind {
	panic("cannot call Kind on parquet group")
}

func (groupType) Compare(Value, Value) int {
	panic("cannot compare values on parquet group")
}

func (groupType) NewColumnIndexer(int) ColumnIndexer {
	panic("cannot create column indexer from parquet group")
}

func (groupType) NewDictionary(int, int, encoding.Values) Dictionary {
	panic("cannot create dictionary from parquet group")
}

func (t groupType) NewColumnBuffer(int, int) ColumnBuffer {
	panic("cannot create column buffer from parquet group")
}

func (t groupType) NewPage(int, int, encoding.Values) Page {
	panic("cannot create page from parquet group")
}

func (t groupType) NewValues(_ []byte, _ []uint32) encoding.Values {
	panic("cannot create values from parquet group")
}

func (groupType) Encode(_ []byte, _ encoding.Values, _ encoding.Encoding) ([]byte, error) {
	panic("cannot encode parquet group")
}

func (groupType) Decode(_ encoding.Values, _ []byte, _ encoding.Encoding) (encoding.Values, error) {
	panic("cannot decode parquet group")
}

func (groupType) EstimateDecodeSize(_ int, _ []byte, _ encoding.Encoding) int {
	panic("cannot estimate decode size of parquet group")
}

func (groupType) AssignValue(reflect.Value, Value) error {
	panic("cannot assign value to a parquet group")
}

func (t groupType) ConvertValue(Value, Type) (Value, error) {
	panic("cannot convert value to a parquet group")
}

func (groupType) Length() int { return 0 }

func (groupType) EstimateSize(int) int { return 0 }

func (groupType) EstimateNumValues(int) int { return 0 }

func (groupType) ColumnOrder() *format.ColumnOrder { return nil }

func (groupType) PhysicalType() *format.Type { return nil }

func (groupType) LogicalType() *format.LogicalType { return nil }

func (groupType) ConvertedType() *deprecated.ConvertedType { return nil }
