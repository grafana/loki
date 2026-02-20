// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package mapstructure // import "go.opentelemetry.io/collector/confmap/internal/mapstructure"

import (
	"encoding"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	yaml "go.yaml.in/yaml/v3"
)

const (
	tagNameMapStructure = "mapstructure"
	optionSeparator     = ","
	optionOmitEmpty     = "omitempty"
	optionSquash        = "squash"
	optionRemain        = "remain"
	optionSkip          = "-"
)

var errNonStringEncodedKey = errors.New("non string-encoded key")

// tagInfo stores the mapstructure tag details.
type tagInfo struct {
	name      string
	omitEmpty bool
	squash    bool
}

// An Encoder takes structured data and converts it into an
// interface following the mapstructure tags.
type Encoder struct {
	config *EncoderConfig
}

// EncoderConfig is the configuration used to create a new encoder.
type EncoderConfig struct {
	// EncodeHook, if set, is a way to provide custom encoding. It
	// will be called before structs and primitive types.
	EncodeHook mapstructure.DecodeHookFunc
}

// New returns a new encoder for the configuration.
func New(cfg *EncoderConfig) *Encoder {
	return &Encoder{config: cfg}
}

// Encode takes the input and uses reflection to encode it to
// an interface based on the mapstructure spec.
func (e *Encoder) Encode(input any) (any, error) {
	return e.encode(reflect.ValueOf(input))
}

// encode processes the value based on the reflect.Kind.
func (e *Encoder) encode(value reflect.Value) (any, error) {
	if value.IsValid() {
		switch value.Kind() {
		case reflect.Interface, reflect.Ptr:
			return e.encode(value.Elem())
		case reflect.Map:
			return e.encodeMap(value)
		case reflect.Slice:
			return e.encodeSlice(value)
		case reflect.Struct:
			return e.encodeStruct(value)
		default:
			return e.encodeHook(value)
		}
	}
	return nil, nil
}

// encodeHook calls the EncodeHook in the EncoderConfig with the value passed in.
// This is called before processing structs and for primitive data types.
func (e *Encoder) encodeHook(value reflect.Value) (any, error) {
	if e.config != nil && e.config.EncodeHook != nil {
		out, err := mapstructure.DecodeHookExec(e.config.EncodeHook, value, value)
		if err != nil {
			return nil, fmt.Errorf("error running encode hook: %w", err)
		}
		return out, nil
	}
	return value.Interface(), nil
}

// encodeStruct encodes the struct by iterating over the fields, getting the
// mapstructure tagInfo for each exported field, and encoding the value.
func (e *Encoder) encodeStruct(value reflect.Value) (any, error) {
	if value.Kind() != reflect.Struct {
		return nil, &reflect.ValueError{
			Method: "encodeStruct",
			Kind:   value.Kind(),
		}
	}
	out, err := e.encodeHook(value)
	if err != nil {
		return nil, err
	}
	value = reflect.ValueOf(out)
	// if the output of encodeHook is no longer a struct,
	// call encode against it.
	if value.Kind() != reflect.Struct {
		return e.encode(value)
	}
	result := make(map[string]any)
	for i := 0; i < value.NumField(); i++ {
		field := value.Field(i)
		if field.CanInterface() {
			info := getTagInfo(value.Type().Field(i))
			if (info.omitEmpty && field.IsZero()) || info.name == optionSkip {
				continue
			}
			encoded, err := e.encode(field)
			if err != nil {
				return nil, fmt.Errorf("error encoding field %q: %w", info.name, err)
			}
			if info.squash {
				if m, ok := encoded.(map[string]any); ok {
					maps.Copy(result, m)
				}
			} else {
				result[info.name] = encoded
			}
		}
	}
	return result, nil
}

// encodeSlice iterates over the slice and encodes each of the elements.
func (e *Encoder) encodeSlice(value reflect.Value) (any, error) {
	if value.Kind() != reflect.Slice {
		return nil, &reflect.ValueError{
			Method: "encodeSlice",
			Kind:   value.Kind(),
		}
	}

	if value.IsNil() {
		return []any(nil), nil
	}

	result := make([]any, value.Len())
	for i := 0; i < value.Len(); i++ {
		var err error
		if result[i], err = e.encode(value.Index(i)); err != nil {
			return nil, fmt.Errorf("error encoding element in slice at index %d: %w", i, err)
		}
	}
	return result, nil
}

// encodeMap encodes a map by encoding the key and value. Returns errNonStringEncodedKey
// if the key is not encoded into a string.
func (e *Encoder) encodeMap(value reflect.Value) (any, error) {
	if value.Kind() != reflect.Map {
		return nil, &reflect.ValueError{
			Method: "encodeMap",
			Kind:   value.Kind(),
		}
	}

	var result map[string]any
	if value.Len() > 0 || !value.IsNil() {
		result = make(map[string]any)
	}

	iterator := value.MapRange()
	for iterator.Next() {
		encoded, err := e.encode(iterator.Key())
		if err != nil {
			return nil, fmt.Errorf("error encoding key: %w", err)
		}

		v := reflect.ValueOf(encoded)
		var key string

		switch v.Kind() {
		case reflect.String:
			key = v.String()
		default:
			return nil, fmt.Errorf("%w, key: %q, kind: %v, type: %T", errNonStringEncodedKey, iterator.Key().Interface(), iterator.Key().Kind(), encoded)
		}

		if _, ok := result[key]; ok {
			return nil, fmt.Errorf("duplicate key %q while encoding", key)
		}
		if result[key], err = e.encode(iterator.Value()); err != nil {
			return nil, fmt.Errorf("error encoding map value for key %q: %w", key, err)
		}
	}
	return result, nil
}

// getTagInfo looks up the mapstructure tag and uses that if available.
// Uses the lowercase field if not found. Checks for omitempty and squash.
func getTagInfo(field reflect.StructField) *tagInfo {
	info := tagInfo{}
	if tag, ok := field.Tag.Lookup(tagNameMapStructure); ok {
		options := strings.Split(tag, optionSeparator)
		info.name = options[0]
		if len(options) > 1 {
			for _, option := range options[1:] {
				switch option {
				case optionOmitEmpty:
					info.omitEmpty = true
				case optionSquash, optionRemain:
					info.squash = true
				}
			}
		}
	} else {
		info.name = strings.ToLower(field.Name)
	}
	return &info
}

// TextMarshalerHookFunc returns a DecodeHookFuncValue that checks
// for the encoding.TextMarshaler interface and calls the MarshalText
// function if found.
func TextMarshalerHookFunc() mapstructure.DecodeHookFuncValue {
	return func(from, _ reflect.Value) (any, error) {
		marshaler, ok := from.Interface().(encoding.TextMarshaler)
		if !ok {
			return from.Interface(), nil
		}
		out, err := marshaler.MarshalText()
		if err != nil {
			return nil, err
		}
		return string(out), nil
	}
}

// YamlMarshalerHookFunc returns a DecodeHookFuncValue that checks for structs
// that have yaml tags but no mapstructure tags. If found, it will convert the struct
// to map[string]any using the yaml package, which respects the yaml tags. Ultimately,
// this allows mapstructure to later marshal the map[string]any in a generic way.
func YamlMarshalerHookFunc() mapstructure.DecodeHookFuncValue {
	return func(from, _ reflect.Value) (any, error) {
		if from.Kind() == reflect.Struct {
			for i := 0; i < from.NumField(); i++ {
				if _, ok := from.Type().Field(i).Tag.Lookup("mapstructure"); ok {
					// The struct has at least one mapstructure tag so don't do anything.
					return from.Interface(), nil
				}

				if _, ok := from.Type().Field(i).Tag.Lookup("yaml"); ok {
					// The struct has at least one yaml tag, so convert it to map[string]any using yaml.
					yamlBytes, err := yaml.Marshal(from.Interface())
					if err != nil {
						return nil, err
					}
					var m map[string]any
					err = yaml.Unmarshal(yamlBytes, &m)
					if err != nil {
						return nil, err
					}
					return m, nil
				}
			}
		}
		return from.Interface(), nil
	}
}
