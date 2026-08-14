package variant

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Marshal converts a Go value to variant binary encoding, returning the
// metadata and value byte slices.
func Marshal(v any) (metadata, value []byte, err error) {
	var b MetadataBuilder
	enc := getEncoder(&b)

	start, end, err := enc.encodeReflect(reflect.ValueOf(v))
	if err != nil {
		releaseEncoder(enc)
		return nil, nil, err
	}
	valueBytes := make([]byte, end-start)
	copy(valueBytes, enc.scratch[start:end])

	releaseEncoder(enc)

	_, metadataBytes := b.Build()
	return metadataBytes, valueBytes, nil
}

// ValueOf converts a Go value to a variant Value without encoding it. It is
// useful for callers that need to inspect or partially encode the value,
// such as shredded variant writers.
func ValueOf(v any) (Value, error) {
	return goToVariant(v)
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

// goToVariant converts a Go value to a variant Value. Marshal encodes
// through the streaming encoder instead (see encodeReflect); this tree
// walk backs ValueOf, whose callers need the Value tree to make per-field
// decisions before encoding. The two must map Go types identically.
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

	for rv.Kind() == reflect.Ptr || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return Null(), nil
		}
		rv = rv.Elem()
	}

	// uuid.UUID and time.Time must be recognized before the Kind switch:
	// uuid.UUID is a [16]byte array and time.Time a struct with no exported
	// fields, so the generic cases would mishandle them.
	iface := rv.Interface()
	switch val := iface.(type) {
	case uuid.UUID:
		return UUID(val), nil
	case time.Time:
		return timeToVariant(val), nil
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
			return Binary(rv.Bytes()), nil
		}
		return goSliceToArray(rv)
	case reflect.Array:
		// Byte arrays (including [16]byte) map to Binary. uuid.UUID is
		// handled above via its concrete type; without a logical UUID
		// annotation a fixed-length byte array is just bytes.
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			b := make([]byte, rv.Len())
			for i := range b {
				b[i] = byte(rv.Index(i).Uint())
			}
			return Binary(b), nil
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

// timeToVariant converts a time.Time to a timestamp variant value. The
// timestamp is stored with time zone (isAdjustedToUTC), which is the natural
// mapping for time.Time since it represents an instant. Nanosecond precision
// is used when the value has sub-microsecond components and fits in the
// nanosecond timestamp range; microsecond precision otherwise.
func timeToVariant(t time.Time) Value {
	if t.Nanosecond()%1000 != 0 && t.Year() >= 1678 && t.Year() <= 2261 {
		return TimestampNanos(t.UnixNano())
	}
	return Timestamp(t.UnixMicro())
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
