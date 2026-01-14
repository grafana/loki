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

func Geography(crs string, algorithm format.EdgeInterpolationAlgorithm) Node {
	return Leaf(&geographyType{CRS: crs, Algorithm: algorithm})
}

type geographyType format.GeographyType

func (t *geographyType) String() string { return (*format.GeographyType)(t).String() }

func (t *geographyType) Kind() Kind { return byteArrayType{}.Kind() }

func (t *geographyType) Length() int { return byteArrayType{}.Length() }

func (t *geographyType) EstimateSize(n int) int { return byteArrayType{}.EstimateSize(n) }

func (t *geographyType) EstimateNumValues(n int) int { return byteArrayType{}.EstimateNumValues(n) }

func (t *geographyType) Compare(a, b Value) int { return byteArrayType{}.Compare(a, b) }

func (t *geographyType) ColumnOrder() *format.ColumnOrder { return byteArrayType{}.ColumnOrder() }

func (t *geographyType) PhysicalType() *format.Type { return byteArrayType{}.PhysicalType() }

func (t *geographyType) LogicalType() *format.LogicalType {
	f := &format.LogicalType{Geography: &format.GeographyType{
		CRS:       t.CRS,
		Algorithm: t.Algorithm,
	}}
	if t.CRS == "" {
		f.Geography.CRS = format.GeometryDefaultCRS
	}
	if t.Algorithm != 0 {
		f.Geography.Algorithm = t.Algorithm
	}

	return f
}

func (t *geographyType) ConvertedType() *deprecated.ConvertedType {
	return nil
}

func (t *geographyType) NewColumnIndexer(sizeLimit int) ColumnIndexer {
	return byteArrayType{}.NewColumnIndexer(sizeLimit)
}

func (t *geographyType) NewDictionary(columnIndex, numValues int, data encoding.Values) Dictionary {
	return byteArrayType{}.NewDictionary(columnIndex, numValues, data)
}

func (t *geographyType) NewColumnBuffer(columnIndex, numValues int) ColumnBuffer {
	return byteArrayType{}.NewColumnBuffer(columnIndex, numValues)
}

func (t *geographyType) NewPage(columnIndex, numValues int, data encoding.Values) Page {
	return byteArrayType{}.NewPage(columnIndex, numValues, data)
}

func (t *geographyType) NewValues(values []byte, offsets []uint32) encoding.Values {
	return byteArrayType{}.NewValues(values, offsets)
}

func (t *geographyType) Encode(dst []byte, src encoding.Values, enc encoding.Encoding) ([]byte, error) {
	return byteArrayType{}.Encode(dst, src, enc)
}

func (t *geographyType) Decode(dst encoding.Values, src []byte, enc encoding.Encoding) (encoding.Values, error) {
	return byteArrayType{}.Decode(dst, src, enc)
}

func (t *geographyType) EstimateDecodeSize(numValues int, src []byte, enc encoding.Encoding) int {
	return byteArrayType{}.EstimateDecodeSize(numValues, src, enc)
}

func (t *geographyType) AssignValue(dst reflect.Value, src Value) error {
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

func (t *geographyType) ConvertValue(val Value, typ Type) (Value, error) {
	switch src := typ.(type) {
	case *geographyType:
		if src.LogicalType().Geography.CRS != t.CRS {
			return Value{}, errors.New("cannot convert between geography types with different CRS")
		}
		if src.LogicalType().Geography.Algorithm != t.LogicalType().Geography.Algorithm {
			return Value{}, errors.New("cannot convert between geography types with different Algorithm")
		}
		return val, nil
	case *geometryType:
		if src.LogicalType().Geometry.CRS != t.CRS {
			return Value{}, errors.New("cannot convert between geography and geometry types with different CRS")
		}
		return val, nil
	}
	return byteArrayType{}.ConvertValue(val, typ)
}
