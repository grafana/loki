// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:generate mdatagen metadata.yaml

package confmap // import "go.opentelemetry.io/collector/confmap"

import (
	"encoding"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/knadh/koanf/maps"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/v2"

	encoder "go.opentelemetry.io/collector/confmap/internal/mapstructure"
)

const (
	// KeyDelimiter is used as the default key delimiter in the default koanf instance.
	KeyDelimiter = "::"
)

const (
	// MapstructureTag is the struct field tag used to record marshaling/unmarshaling settings.
	// See https://pkg.go.dev/github.com/go-viper/mapstructure/v2 for supported values.
	MapstructureTag = "mapstructure"
)

// New creates a new empty confmap.Conf instance.
func New() *Conf {
	return &Conf{k: koanf.New(KeyDelimiter)}
}

// NewFromStringMap creates a confmap.Conf from a map[string]any.
func NewFromStringMap(data map[string]any) *Conf {
	p := New()
	// Cannot return error because the koanf instance is empty.
	_ = p.k.Load(confmap.Provider(data, KeyDelimiter), nil)
	return p
}

// Conf represents the raw configuration map for the OpenTelemetry Collector.
// The confmap.Conf can be unmarshalled into the Collector's config using the "service" package.
type Conf struct {
	k *koanf.Koanf
	// If true, upon unmarshaling do not call the Unmarshal function on the struct
	// if it implements Unmarshaler and is the top-level struct.
	// This avoids running into an infinite recursion where Unmarshaler.Unmarshal and
	// Conf.Unmarshal would call each other.
	skipTopLevelUnmarshaler bool
}

// AllKeys returns all keys holding a value, regardless of where they are set.
// Nested keys are returned with a KeyDelimiter separator.
func (l *Conf) AllKeys() []string {
	return l.k.Keys()
}

type UnmarshalOption interface {
	apply(*unmarshalOption)
}

type unmarshalOption struct {
	ignoreUnused bool
}

// WithIgnoreUnused sets an option to ignore errors if existing
// keys in the original Conf were unused in the decoding process
// (extra keys).
func WithIgnoreUnused() UnmarshalOption {
	return unmarshalOptionFunc(func(uo *unmarshalOption) {
		uo.ignoreUnused = true
	})
}

type unmarshalOptionFunc func(*unmarshalOption)

func (fn unmarshalOptionFunc) apply(set *unmarshalOption) {
	fn(set)
}

// Unmarshal unmarshalls the config into a struct using the given options.
// Tags on the fields of the structure must be properly set.
func (l *Conf) Unmarshal(result any, opts ...UnmarshalOption) error {
	set := unmarshalOption{}
	for _, opt := range opts {
		opt.apply(&set)
	}
	return decodeConfig(l, result, !set.ignoreUnused, l.skipTopLevelUnmarshaler)
}

type marshalOption struct{}

type MarshalOption interface {
	apply(*marshalOption)
}

// Marshal encodes the config and merges it into the Conf.
func (l *Conf) Marshal(rawVal any, _ ...MarshalOption) error {
	enc := encoder.New(encoderConfig(rawVal))
	data, err := enc.Encode(rawVal)
	if err != nil {
		return err
	}
	out, ok := data.(map[string]any)
	if !ok {
		return errors.New("invalid config encoding")
	}
	return l.Merge(NewFromStringMap(out))
}

func (l *Conf) unsanitizedGet(key string) any {
	return l.k.Get(key)
}

// sanitize recursively removes expandedValue references from the given data.
// It uses the expandedValue.Value field to replace the expandedValue references.
func sanitize(a any) any {
	return sanitizeExpanded(a, false)
}

// sanitizeToStringMap recursively removes expandedValue references from the given data.
// It uses the expandedValue.Original field to replace the expandedValue references.
func sanitizeToStr(a any) any {
	return sanitizeExpanded(a, true)
}

func sanitizeExpanded(a any, useOriginal bool) any {
	switch m := a.(type) {
	case map[string]any:
		c := maps.Copy(m)
		for k, v := range m {
			c[k] = sanitizeExpanded(v, useOriginal)
		}
		return c
	case []any:
		var newSlice []any
		for _, e := range m {
			newSlice = append(newSlice, sanitizeExpanded(e, useOriginal))
		}
		return newSlice
	case expandedValue:
		if useOriginal {
			return m.Original
		}
		return m.Value
	}
	return a
}

// Get can retrieve any value given the key to use.
func (l *Conf) Get(key string) any {
	val := l.unsanitizedGet(key)
	return sanitizeExpanded(val, false)
}

// IsSet checks to see if the key has been set in any of the data locations.
func (l *Conf) IsSet(key string) bool {
	return l.k.Exists(key)
}

// Merge merges the input given configuration into the existing config.
// Note that the given map may be modified.
func (l *Conf) Merge(in *Conf) error {
	return l.k.Merge(in.k)
}

// Sub returns new Conf instance representing a sub-config of this instance.
// It returns an error is the sub-config is not a map[string]any (use Get()), and an empty Map if none exists.
func (l *Conf) Sub(key string) (*Conf, error) {
	// Code inspired by the koanf "Cut" func, but returns an error instead of empty map for unsupported sub-config type.
	data := l.unsanitizedGet(key)
	if data == nil {
		return New(), nil
	}

	switch v := data.(type) {
	case map[string]any:
		return NewFromStringMap(v), nil
	case expandedValue:
		if m, ok := v.Value.(map[string]any); ok {
			return NewFromStringMap(m), nil
		}
	}

	return nil, fmt.Errorf("unexpected sub-config value kind for key:%s value:%v kind:%v", key, data, reflect.TypeOf(data).Kind())
}

func (l *Conf) toStringMapWithExpand() map[string]any {
	m := maps.Unflatten(l.k.All(), KeyDelimiter)
	return m
}

// ToStringMap creates a map[string]any from a Parser.
func (l *Conf) ToStringMap() map[string]any {
	return sanitize(l.toStringMapWithExpand()).(map[string]any)
}

// decodeConfig decodes the contents of the Conf into the result argument, using a
// mapstructure decoder with the following notable behaviors. Ensures that maps whose
// values are nil pointer structs resolved to the zero value of the target struct (see
// expandNilStructPointers). Converts string to []string by splitting on ','. Ensures
// uniqueness of component IDs (see mapKeyStringToMapKeyTextUnmarshalerHookFunc).
// Decodes time.Duration from strings. Allows custom unmarshaling for structs implementing
// encoding.TextUnmarshaler. Allows custom unmarshaling for structs implementing confmap.Unmarshaler.
func decodeConfig(m *Conf, result any, errorUnused bool, skipTopLevelUnmarshaler bool) error {
	dc := &mapstructure.DecoderConfig{
		ErrorUnused:      errorUnused,
		Result:           result,
		TagName:          MapstructureTag,
		WeaklyTypedInput: false,
		MatchName:        caseSensitiveMatchName,
		DecodeHook: mapstructure.ComposeDecodeHookFunc(
			useExpandValue(),
			expandNilStructPointersHookFunc(),
			mapstructure.StringToSliceHookFunc(","),
			mapKeyStringToMapKeyTextUnmarshalerHookFunc(),
			mapstructure.StringToTimeDurationHookFunc(),
			mapstructure.TextUnmarshallerHookFunc(),
			unmarshalerHookFunc(result, skipTopLevelUnmarshaler),
			// after the main unmarshaler hook is called,
			// we unmarshal the embedded structs if present to merge with the result:
			unmarshalerEmbeddedStructsHookFunc(),
			zeroSliceHookFunc(),
		),
	}
	decoder, err := mapstructure.NewDecoder(dc)
	if err != nil {
		return err
	}
	if err = decoder.Decode(m.toStringMapWithExpand()); err != nil {
		if strings.HasPrefix(err.Error(), "error decoding ''") {
			return errors.Unwrap(err)
		}
		return err
	}
	return nil
}

// encoderConfig returns a default encoder.EncoderConfig that includes
// an EncodeHook that handles both TextMarshaller and Marshaler
// interfaces.
func encoderConfig(rawVal any) *encoder.EncoderConfig {
	return &encoder.EncoderConfig{
		EncodeHook: mapstructure.ComposeDecodeHookFunc(
			encoder.YamlMarshalerHookFunc(),
			encoder.TextMarshalerHookFunc(),
			marshalerHookFunc(rawVal),
		),
	}
}

// case-sensitive version of the callback to be used in the MatchName property
// of the DecoderConfig. The default for MatchEqual is to use strings.EqualFold,
// which is case-insensitive.
func caseSensitiveMatchName(a, b string) bool {
	return a == b
}

func castTo(exp expandedValue, useOriginal bool) any {
	// If the target field is a string, use `exp.Original` or fail if not available.
	if useOriginal {
		return exp.Original
	}
	// Otherwise, use the parsed value (previous behavior).
	return exp.Value
}

// Check if a reflect.Type is of the form T, where:
// X is any type or interface
// T = string | map[X]T | []T | [n]T
func isStringyStructure(t reflect.Type) bool {
	if t.Kind() == reflect.String {
		return true
	}
	if t.Kind() == reflect.Map {
		return isStringyStructure(t.Elem())
	}
	if t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		return isStringyStructure(t.Elem())
	}
	return false
}

// When a value has been loaded from an external source via a provider, we keep both the
// parsed value and the original string value. This allows us to expand the value to its
// original string representation when decoding into a string field, and use the original otherwise.
func useExpandValue() mapstructure.DecodeHookFuncType {
	return func(
		_ reflect.Type,
		to reflect.Type,
		data any,
	) (any, error) {
		if exp, ok := data.(expandedValue); ok {
			v := castTo(exp, to.Kind() == reflect.String)
			// See https://github.com/open-telemetry/opentelemetry-collector/issues/10949
			// If the `to.Kind` is not a string, then expandValue's original value is useless and
			// the casted-to value will be nil. In that scenario, we need to use the default value of `to`'s kind.
			if v == nil {
				return reflect.Zero(to).Interface(), nil
			}
			return v, nil
		}

		switch to.Kind() {
		case reflect.Array, reflect.Slice, reflect.Map:
			if isStringyStructure(to) {
				// If the target field is a stringy structure, sanitize to use the original string value everywhere.
				return sanitizeToStr(data), nil
			}
			// Otherwise, sanitize to use the parsed value everywhere.
			return sanitize(data), nil
		}
		return data, nil
	}
}

// In cases where a config has a mapping of something to a struct pointers
// we want nil values to resolve to a pointer to the zero value of the
// underlying struct just as we want nil values of a mapping of something
// to a struct to resolve to the zero value of that struct.
//
// e.g. given a config type:
// type Config struct { Thing *SomeStruct `mapstructure:"thing"` }
//
// and yaml of:
// config:
//
//	thing:
//
// we want an unmarshaled Config to be equivalent to
// Config{Thing: &SomeStruct{}} instead of Config{Thing: nil}
func expandNilStructPointersHookFunc() mapstructure.DecodeHookFuncValue {
	return func(from reflect.Value, to reflect.Value) (any, error) {
		// ensure we are dealing with map to map comparison
		if from.Kind() == reflect.Map && to.Kind() == reflect.Map {
			toElem := to.Type().Elem()
			// ensure that map values are pointers to a struct
			// (that may be nil and require manual setting w/ zero value)
			if toElem.Kind() == reflect.Ptr && toElem.Elem().Kind() == reflect.Struct {
				fromRange := from.MapRange()
				for fromRange.Next() {
					fromKey := fromRange.Key()
					fromValue := fromRange.Value()
					// ensure that we've run into a nil pointer instance
					if fromValue.IsNil() {
						newFromValue := reflect.New(toElem.Elem())
						from.SetMapIndex(fromKey, newFromValue)
					}
				}
			}
		}
		return from.Interface(), nil
	}
}

// mapKeyStringToMapKeyTextUnmarshalerHookFunc returns a DecodeHookFuncType that checks that a conversion from
// map[string]any to map[encoding.TextUnmarshaler]any does not overwrite keys,
// when UnmarshalText produces equal elements from different strings (e.g. trims whitespaces).
//
// This is needed in combination with ComponentID, which may produce equal IDs for different strings,
// and an error needs to be returned in that case, otherwise the last equivalent ID overwrites the previous one.
func mapKeyStringToMapKeyTextUnmarshalerHookFunc() mapstructure.DecodeHookFuncType {
	return func(from reflect.Type, to reflect.Type, data any) (any, error) {
		if from.Kind() != reflect.Map || from.Key().Kind() != reflect.String {
			return data, nil
		}

		if to.Kind() != reflect.Map {
			return data, nil
		}

		// Checks that the key type of to implements the TextUnmarshaler interface.
		if _, ok := reflect.New(to.Key()).Interface().(encoding.TextUnmarshaler); !ok {
			return data, nil
		}

		// Create a map with key value of to's key to bool.
		fieldNameSet := reflect.MakeMap(reflect.MapOf(to.Key(), reflect.TypeOf(true)))
		for k := range data.(map[string]any) {
			// Create a new value of the to's key type.
			tKey := reflect.New(to.Key())

			// Use tKey to unmarshal the key of the map.
			if err := tKey.Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(k)); err != nil {
				return nil, err
			}
			// Checks if the key has already been decoded in a previous iteration.
			if fieldNameSet.MapIndex(reflect.Indirect(tKey)).IsValid() {
				return nil, fmt.Errorf("duplicate name %q after unmarshaling %v", k, tKey)
			}
			fieldNameSet.SetMapIndex(reflect.Indirect(tKey), reflect.ValueOf(true))
		}
		return data, nil
	}
}

// unmarshalerEmbeddedStructsHookFunc provides a mechanism for embedded structs to define their own unmarshal logic,
// by implementing the Unmarshaler interface.
func unmarshalerEmbeddedStructsHookFunc() mapstructure.DecodeHookFuncValue {
	return func(from reflect.Value, to reflect.Value) (any, error) {
		if to.Type().Kind() != reflect.Struct {
			return from.Interface(), nil
		}
		fromAsMap, ok := from.Interface().(map[string]any)
		if !ok {
			return from.Interface(), nil
		}
		for i := 0; i < to.Type().NumField(); i++ {
			// embedded structs passed in via `squash` cannot be pointers. We just check if they are structs:
			f := to.Type().Field(i)
			if f.IsExported() && slices.Contains(strings.Split(f.Tag.Get(MapstructureTag), ","), "squash") {
				if unmarshaler, ok := to.Field(i).Addr().Interface().(Unmarshaler); ok {
					c := NewFromStringMap(fromAsMap)
					c.skipTopLevelUnmarshaler = true
					if err := unmarshaler.Unmarshal(c); err != nil {
						return nil, err
					}
					// the struct we receive from this unmarshaling only contains fields related to the embedded struct.
					// we merge this partially unmarshaled struct with the rest of the result.
					// note we already unmarshaled the main struct earlier, and therefore merge with it.
					conf := New()
					if err := conf.Marshal(unmarshaler); err != nil {
						return nil, err
					}
					resultMap := conf.ToStringMap()
					for k, v := range resultMap {
						fromAsMap[k] = v
					}
				}
			}
		}
		return fromAsMap, nil
	}
}

// Provides a mechanism for individual structs to define their own unmarshal logic,
// by implementing the Unmarshaler interface, unless skipTopLevelUnmarshaler is
// true and the struct matches the top level object being unmarshaled.
func unmarshalerHookFunc(result any, skipTopLevelUnmarshaler bool) mapstructure.DecodeHookFuncValue {
	return func(from reflect.Value, to reflect.Value) (any, error) {
		if !to.CanAddr() {
			return from.Interface(), nil
		}

		toPtr := to.Addr().Interface()
		// Need to ignore the top structure to avoid running into an infinite recursion
		// where Unmarshaler.Unmarshal and Conf.Unmarshal would call each other.
		if toPtr == result && skipTopLevelUnmarshaler {
			return from.Interface(), nil
		}

		unmarshaler, ok := toPtr.(Unmarshaler)
		if !ok {
			return from.Interface(), nil
		}

		if _, ok = from.Interface().(map[string]any); !ok {
			return from.Interface(), nil
		}

		// Use the current object if not nil (to preserve other configs in the object), otherwise zero initialize.
		if to.Addr().IsNil() {
			unmarshaler = reflect.New(to.Type()).Interface().(Unmarshaler)
		}

		c := NewFromStringMap(from.Interface().(map[string]any))
		c.skipTopLevelUnmarshaler = true
		if err := unmarshaler.Unmarshal(c); err != nil {
			return nil, err
		}

		return unmarshaler, nil
	}
}

// marshalerHookFunc returns a DecodeHookFuncValue that checks structs that aren't
// the original to see if they implement the Marshaler interface.
func marshalerHookFunc(orig any) mapstructure.DecodeHookFuncValue {
	origType := reflect.TypeOf(orig)
	return func(from reflect.Value, _ reflect.Value) (any, error) {
		if from.Kind() != reflect.Struct {
			return from.Interface(), nil
		}

		// ignore original to avoid infinite loop.
		if from.Type() == origType && reflect.DeepEqual(from.Interface(), orig) {
			return from.Interface(), nil
		}
		marshaler, ok := from.Interface().(Marshaler)
		if !ok {
			return from.Interface(), nil
		}
		conf := New()
		if err := marshaler.Marshal(conf); err != nil {
			return nil, err
		}
		return conf.ToStringMap(), nil
	}
}

// Unmarshaler interface may be implemented by types to customize their behavior when being unmarshaled from a Conf.
type Unmarshaler interface {
	// Unmarshal a Conf into the struct in a custom way.
	// The Conf for this specific component may be nil or empty if no config available.
	// This method should only be called by decoding hooks when calling Conf.Unmarshal.
	Unmarshal(component *Conf) error
}

// Marshaler defines an optional interface for custom configuration marshaling.
// A configuration struct can implement this interface to override the default
// marshaling.
type Marshaler interface {
	// Marshal the config into a Conf in a custom way.
	// The Conf will be empty and can be merged into.
	Marshal(component *Conf) error
}

// This hook is used to solve the issue: https://github.com/open-telemetry/opentelemetry-collector/issues/4001
// We adopt the suggestion provided in this issue: https://github.com/mitchellh/mapstructure/issues/74#issuecomment-279886492
// We should empty every slice before unmarshalling unless user provided slice is nil.
// Assume that we had a struct with a field of type slice called `keys`, which has default values of ["a", "b"]
//
//	type Config struct {
//	  Keys []string `mapstructure:"keys"`
//	}
//
// The configuration provided by users may have following cases
// 1. configuration have `keys` field and have a non-nil values for this key, the output should be overridden
//   - for example, input is {"keys", ["c"]}, then output is Config{ Keys: ["c"]}
//
// 2. configuration have `keys` field and have an empty slice for this key, the output should be overridden by empty slices
//   - for example, input is {"keys", []}, then output is Config{ Keys: []}
//
// 3. configuration have `keys` field and have nil value for this key, the output should be default config
//   - for example, input is {"keys": nil}, then output is Config{ Keys: ["a", "b"]}
//
// 4. configuration have no `keys` field specified, the output should be default config
//   - for example, input is {}, then output is Config{ Keys: ["a", "b"]}
func zeroSliceHookFunc() mapstructure.DecodeHookFuncValue {
	return func(from reflect.Value, to reflect.Value) (any, error) {
		if to.CanSet() && to.Kind() == reflect.Slice && from.Kind() == reflect.Slice {
			to.Set(reflect.MakeSlice(to.Type(), from.Len(), from.Cap()))
		}

		return from.Interface(), nil
	}
}

type moduleFactory[T any, S any] interface {
	Create(s S) T
}

type createConfmapFunc[T any, S any] func(s S) T

type confmapModuleFactory[T any, S any] struct {
	f createConfmapFunc[T, S]
}

func (c confmapModuleFactory[T, S]) Create(s S) T {
	return c.f(s)
}

func newConfmapModuleFactory[T any, S any](f createConfmapFunc[T, S]) moduleFactory[T, S] {
	return confmapModuleFactory[T, S]{
		f: f,
	}
}
