package parquet

import (
	"reflect"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/format"
)

type nullType format.NullType

func (t *nullType) String() string { return (*format.NullType)(t).String() }

func (t *nullType) Kind() Kind { return -1 }

func (t *nullType) Length() int { return 0 }

func (t *nullType) EstimateSize(int) int { return 0 }

func (t *nullType) EstimateNumValues(int) int { return 0 }

func (t *nullType) Compare(Value, Value) int { panic("cannot compare values on parquet NULL type") }

func (t *nullType) ColumnOrder() *format.ColumnOrder { return nil }

func (t *nullType) PhysicalType() *format.Type { return nil }

func (t *nullType) LogicalType() *format.LogicalType {
	return &format.LogicalType{Unknown: (*format.NullType)(t)}
}

func (t *nullType) ConvertedType() *deprecated.ConvertedType { return nil }

func (t *nullType) NewColumnIndexer(int) ColumnIndexer {
	panic("create create column indexer from parquet NULL type")
}

func (t *nullType) NewDictionary(columnIndex, numValues int, data encoding.Values) Dictionary {
	return newNullDictionary(t, makeColumnIndex(columnIndex), makeNumValues(numValues), data)
}

func (t *nullType) NewColumnBuffer(int, int) ColumnBuffer {
	panic("cannot create column buffer from parquet NULL type")
}

func (t *nullType) NewPage(columnIndex, numValues int, _ encoding.Values) Page {
	return newNullPage(t, makeColumnIndex(columnIndex), makeNumValues(numValues))
}

func (t *nullType) NewValues(_ []byte, _ []uint32) encoding.Values {
	return encoding.Values{}
}

func (t *nullType) Encode(dst []byte, _ encoding.Values, _ encoding.Encoding) ([]byte, error) {
	return dst[:0], nil
}

func (t *nullType) Decode(dst encoding.Values, _ []byte, _ encoding.Encoding) (encoding.Values, error) {
	return dst, nil
}

func (t *nullType) EstimateDecodeSize(_ int, _ []byte, _ encoding.Encoding) int {
	return 0
}

func (t *nullType) AssignValue(reflect.Value, Value) error {
	return nil
}

func (t *nullType) ConvertValue(val Value, _ Type) (Value, error) {
	return val, nil
}
