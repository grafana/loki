package variant

import (
	"fmt"
	"reflect"
	"strings"
	"time"
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
