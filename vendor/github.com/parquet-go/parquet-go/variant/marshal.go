package variant

import (
	"fmt"
	"math"
	"reflect"
	"strings"

	"github.com/google/uuid"
)

// Marshal converts a Go value to variant binary encoding, returning the
// metadata and value byte slices.
func Marshal(v any) (metadata, value []byte, err error) {
	varVal, err := goToVariant(v)
	if err != nil {
		return nil, nil, err
	}
	var b MetadataBuilder
	valueBytes := Encode(&b, varVal)
	_, metadataBytes := b.Build()
	return metadataBytes, valueBytes, nil
}

// Unmarshal decodes variant binary data into a Go value.
func Unmarshal(metadata, value []byte) (any, error) {
	m, err := DecodeMetadata(metadata)
	if err != nil {
		return nil, fmt.Errorf("variant unmarshal: %w", err)
	}
	v, err := Decode(m, value)
	if err != nil {
		return nil, fmt.Errorf("variant unmarshal: %w", err)
	}
	return v.GoValue(), nil
}

// goToVariant converts a Go value to a variant Value.
func goToVariant(v any) (Value, error) {
	if v == nil {
		return Null(), nil
	}
	return goToVariantReflect(reflect.ValueOf(v))
}

func goToVariantReflect(rv reflect.Value) (Value, error) {
	if !rv.IsValid() {
		return Null(), nil
	}

	// Dereference pointers/interfaces
	for rv.Kind() == reflect.Ptr || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return Null(), nil
		}
		rv = rv.Elem()
	}

	// Check concrete types first
	iface := rv.Interface()
	switch val := iface.(type) {
	case uuid.UUID:
		return UUID(val), nil
	}

	switch rv.Kind() {
	case reflect.Bool:
		return Bool(rv.Bool()), nil
	case reflect.Int8:
		return Int8(int8(rv.Int())), nil
	case reflect.Int16:
		return Int16(int16(rv.Int())), nil
	case reflect.Int32:
		return Int32(int32(rv.Int())), nil
	case reflect.Int64, reflect.Int:
		return Int64(rv.Int()), nil
	case reflect.Uint8:
		return Int16(int16(rv.Uint())), nil
	case reflect.Uint16:
		return Int32(int32(rv.Uint())), nil
	case reflect.Uint32:
		return Int64(int64(rv.Uint())), nil
	case reflect.Uint64, reflect.Uint:
		u := rv.Uint()
		if u > math.MaxInt64 {
			return Null(), fmt.Errorf("variant marshal: uint64 value %d overflows int64", u)
		}
		return Int64(int64(u)), nil
	case reflect.Float32:
		return Float(float32(rv.Float())), nil
	case reflect.Float64:
		return Double(rv.Float()), nil
	case reflect.String:
		return String(rv.String()), nil
	case reflect.Slice:
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			// []byte
			return Binary(rv.Bytes()), nil
		}
		return goSliceToArray(rv)
	case reflect.Array:
		if rv.Type() == reflect.TypeFor[uuid.UUID]() {
			var u uuid.UUID
			reflect.ValueOf(&u).Elem().Set(rv)
			return UUID(u), nil
		}
		return goSliceToArray(rv)
	case reflect.Map:
		return goMapToObject(rv)
	case reflect.Struct:
		return goStructToObject(rv)
	default:
		return Null(), fmt.Errorf("variant marshal: unsupported type %s", rv.Type())
	}
}

func goSliceToArray(rv reflect.Value) (Value, error) {
	n := rv.Len()
	elements := make([]Value, n)
	for i := range n {
		v, err := goToVariantReflect(rv.Index(i))
		if err != nil {
			return Null(), err
		}
		elements[i] = v
	}
	return MakeArray(elements), nil
}

func goMapToObject(rv reflect.Value) (Value, error) {
	if rv.IsNil() {
		return Null(), nil
	}
	fields := make([]Field, 0, rv.Len())
	iter := rv.MapRange()
	for iter.Next() {
		key := iter.Key()
		if key.Kind() != reflect.String {
			return Null(), fmt.Errorf("variant marshal: map key must be string, got %s", key.Type())
		}
		val, err := goToVariantReflect(iter.Value())
		if err != nil {
			return Null(), err
		}
		fields = append(fields, Field{Name: key.String(), Value: val})
	}
	return MakeObject(fields), nil
}

func goStructToObject(rv reflect.Value) (Value, error) {
	rt := rv.Type()
	fields := make([]Field, 0, rt.NumField())
	for i := range rt.NumField() {
		sf := rt.Field(i)
		if !sf.IsExported() {
			continue
		}
		name := fieldName(sf)
		if name == "-" {
			continue
		}
		val, err := goToVariantReflect(rv.Field(i))
		if err != nil {
			return Null(), err
		}
		fields = append(fields, Field{Name: name, Value: val})
	}
	return MakeObject(fields), nil
}

// fieldName returns the name to use for a struct field, checking variant and
// json struct tags.
func fieldName(sf reflect.StructField) string {
	if tag, ok := sf.Tag.Lookup("variant"); ok {
		name, _, _ := strings.Cut(tag, ",")
		if name != "" {
			return name
		}
	}
	if tag, ok := sf.Tag.Lookup("json"); ok {
		name, _, _ := strings.Cut(tag, ",")
		if name != "" {
			return name
		}
	}
	return sf.Name
}
