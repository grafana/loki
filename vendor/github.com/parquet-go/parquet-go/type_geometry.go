package parquet

import (
	"errors"
	"reflect"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/format"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/wkb"
)

func Geometry(crs string) Node { return Leaf(&geometryType{CRS: crs}) }

type geometryType format.GeometryType

var geometryDefaultCRSLogicType = format.GeometryType{
	CRS: format.GeometryDefaultCRS,
}

func (t *geometryType) String() string { return (*format.GeometryType)(t).String() }

func (t *geometryType) Kind() Kind { return byteArrayType{}.Kind() }

func (t *geometryType) Length() int { return byteArrayType{}.Length() }

func (t *geometryType) EstimateSize(n int) int { return byteArrayType{}.EstimateSize(n) }

func (t *geometryType) EstimateNumValues(n int) int { return byteArrayType{}.EstimateNumValues(n) }

func (t *geometryType) Compare(a, b Value) int { return byteArrayType{}.Compare(a, b) }

func (t *geometryType) ColumnOrder() *format.ColumnOrder { return byteArrayType{}.ColumnOrder() }

func (t *geometryType) PhysicalType() *format.Type { return byteArrayType{}.PhysicalType() }

func (t *geometryType) LogicalType() *format.LogicalType {
	if t.CRS == "" {
		return &format.LogicalType{Geometry: &geometryDefaultCRSLogicType}
	}
	return &format.LogicalType{Geometry: (*format.GeometryType)(t)}
}

func (t *geometryType) ConvertedType() *deprecated.ConvertedType {
	return nil
}

func (t *geometryType) NewColumnIndexer(sizeLimit int) ColumnIndexer {
	return byteArrayType{}.NewColumnIndexer(sizeLimit)
}

func (t *geometryType) NewDictionary(columnIndex, numValues int, data encoding.Values) Dictionary {
	return byteArrayType{}.NewDictionary(columnIndex, numValues, data)
}

func (t *geometryType) NewColumnBuffer(columnIndex, numValues int) ColumnBuffer {
	return byteArrayType{}.NewColumnBuffer(columnIndex, numValues)
}

func (t *geometryType) NewPage(columnIndex, numValues int, data encoding.Values) Page {
	return byteArrayType{}.NewPage(columnIndex, numValues, data)
}

func (t *geometryType) NewValues(values []byte, offsets []uint32) encoding.Values {
	return byteArrayType{}.NewValues(values, offsets)
}

func (t *geometryType) Encode(dst []byte, src encoding.Values, enc encoding.Encoding) ([]byte, error) {
	return byteArrayType{}.Encode(dst, src, enc)
}

func (t *geometryType) Decode(dst encoding.Values, src []byte, enc encoding.Encoding) (encoding.Values, error) {
	return byteArrayType{}.Decode(dst, src, enc)
}

func (t *geometryType) EstimateDecodeSize(numValues int, src []byte, enc encoding.Encoding) int {
	return byteArrayType{}.EstimateDecodeSize(numValues, src, enc)
}

func (t *geometryType) AssignValue(dst reflect.Value, src Value) error {
	switch dst.Type() {
	case reflect.TypeOf(geom.T(nil)):
		if src.IsNull() {
			dst.Set(reflect.Zero(dst.Type()))
			return nil
		}

		data := src.Bytes()
		g, err := wkb.Unmarshal(data)
		if err != nil {
			return err
		}
		dst.Set(reflect.ValueOf(g))
		return nil
	case reflect.TypeOf((*geom.T)(nil)).Elem():
		if src.IsNull() {
			dst.Set(reflect.Zero(dst.Type()))
			return nil
		}

		data := src.Bytes()
		g, err := wkb.Unmarshal(data)
		if err != nil {
			return err
		}
		dst.Set(reflect.ValueOf(g))
		return nil
	default:
		return byteArrayType{}.AssignValue(dst, src)
	}
}

func (t *geometryType) ConvertValue(val Value, typ Type) (Value, error) {
	switch src := typ.(type) {
	case *geometryType:
		if src.LogicalType().Geometry.CRS != t.CRS {
			return Value{}, errors.New("cannot convert between geometry types with different CRS")
		}
		return val, nil
	case *geographyType:
		if src.LogicalType().Geography.CRS != t.CRS {
			return Value{}, errors.New("cannot convert between geography and geometry types with different CRS")
		}
		return val, nil
	}
	return byteArrayType{}.ConvertValue(val, typ)
}
