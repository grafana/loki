// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package config // import "go.opentelemetry.io/collector/config"

import (
	"context"
	"fmt"
	"reflect"

	"github.com/knadh/koanf"
	"github.com/knadh/koanf/maps"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/mitchellh/mapstructure"
	"github.com/spf13/cast"
)

const (
	// KeyDelimiter is used as the default key delimiter in the default koanf instance.
	KeyDelimiter = "::"
)

// MapConverterFunc is a converter function for the config.Map that allows distributions
// (in the future components as well) to build backwards compatible config converters.
type MapConverterFunc func(context.Context, *Map) error

// NewMap creates a new empty config.Map instance.
func NewMap() *Map {
	return &Map{k: koanf.New(KeyDelimiter)}
}

// NewMapFromStringMap creates a config.Map from a map[string]interface{}.
func NewMapFromStringMap(data map[string]interface{}) *Map {
	p := NewMap()
	// Cannot return error because the koanf instance is empty.
	_ = p.k.Load(confmap.Provider(data, KeyDelimiter), nil)
	return p
}

// Map represents the raw configuration map for the OpenTelemetry Collector.
// The config.Map can be unmarshalled into the Collector's config using the "configunmarshaler" package.
type Map struct {
	k *koanf.Koanf
}

// AllKeys returns all keys holding a value, regardless of where they are set.
// Nested keys are returned with a KeyDelimiter separator.
func (l *Map) AllKeys() []string {
	return l.k.Keys()
}

// Unmarshal unmarshalls the config into a struct.
// Tags on the fields of the structure must be properly set.
func (l *Map) Unmarshal(rawVal interface{}) error {
	decoder, err := mapstructure.NewDecoder(decoderConfig(rawVal))
	if err != nil {
		return err
	}
	return decoder.Decode(l.ToStringMap())
}

// UnmarshalExact unmarshalls the config into a struct, erroring if a field is nonexistent.
func (l *Map) UnmarshalExact(rawVal interface{}) error {
	dc := decoderConfig(rawVal)
	dc.ErrorUnused = true
	decoder, err := mapstructure.NewDecoder(dc)
	if err != nil {
		return err
	}
	return decoder.Decode(l.ToStringMap())
}

// Get can retrieve any value given the key to use.
func (l *Map) Get(key string) interface{} {
	return l.k.Get(key)
}

// Set sets the value for the key.
func (l *Map) Set(key string, value interface{}) {
	// koanf doesn't offer a direct setting mechanism so merging is required.
	merged := koanf.New(KeyDelimiter)
	_ = merged.Load(confmap.Provider(map[string]interface{}{key: value}, KeyDelimiter), nil)
	// TODO (issue 4467): return this error on `Set`.
	_ = l.k.Merge(merged)
}

// IsSet checks to see if the key has been set in any of the data locations.
// IsSet is case-insensitive for a key.
func (l *Map) IsSet(key string) bool {
	return l.k.Exists(key)
}

// Merge merges the input given configuration into the existing config.
// Note that the given map may be modified.
func (l *Map) Merge(in *Map) error {
	return l.k.Merge(in.k)
}

// Sub returns new Parser instance representing a sub-config of this instance.
// It returns an error is the sub-config is not a map (use Get()) and an empty Parser if
// none exists.
func (l *Map) Sub(key string) (*Map, error) {
	data := l.Get(key)
	if data == nil {
		return NewMap(), nil
	}

	if reflect.TypeOf(data).Kind() == reflect.Map {
		return NewMapFromStringMap(cast.ToStringMap(data)), nil
	}

	return nil, fmt.Errorf("unexpected sub-config value kind for key:%s value:%v kind:%v)", key, data, reflect.TypeOf(data).Kind())
}

// ToStringMap creates a map[string]interface{} from a Parser.
func (l *Map) ToStringMap() map[string]interface{} {
	return maps.Unflatten(l.k.All(), KeyDelimiter)
}

// decoderConfig returns a default mapstructure.DecoderConfig capable of parsing time.Duration
// and weakly converting config field values to primitive types.  It also ensures that maps
// whose values are nil pointer structs resolved to the zero value of the target struct (see
// expandNilStructPointers). A decoder created from this mapstructure.DecoderConfig will decode
// its contents to the result argument.
func decoderConfig(result interface{}) *mapstructure.DecoderConfig {
	return &mapstructure.DecoderConfig{
		Result:           result,
		Metadata:         nil,
		TagName:          "mapstructure",
		WeaklyTypedInput: true,
		DecodeHook: mapstructure.ComposeDecodeHookFunc(
			expandNilStructPointersFunc,
			stringToSliceHookFunc,
			mapStringToMapComponentIDHookFunc,
			stringToTimeDurationHookFunc,
			textUnmarshallerHookFunc,
		),
	}
}

var (
	stringToSliceHookFunc        = mapstructure.StringToSliceHookFunc(",")
	stringToTimeDurationHookFunc = mapstructure.StringToTimeDurationHookFunc()
	textUnmarshallerHookFunc     = mapstructure.TextUnmarshallerHookFunc()

	componentIDType = reflect.TypeOf(NewComponentID("foo"))
)

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
//   thing:
//
// we want an unmarshaled Config to be equivalent to
// Config{Thing: &SomeStruct{}} instead of Config{Thing: nil}
var expandNilStructPointersFunc = func(from reflect.Value, to reflect.Value) (interface{}, error) {
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

// mapStringToMapComponentIDHookFunc returns a DecodeHookFunc that converts a map[string]interface{} to
// map[ComponentID]interface{}.
// This is needed in combination since the ComponentID.UnmarshalText may produce equal IDs for different strings,
// and an error needs to be returned in that case, otherwise the last equivalent ID overwrites the previous one.
var mapStringToMapComponentIDHookFunc = func(f reflect.Type, t reflect.Type, data interface{}) (interface{}, error) {
	if f.Kind() != reflect.Map || f.Key().Kind() != reflect.String {
		return data, nil
	}

	if t.Kind() != reflect.Map || t.Key() != componentIDType {
		return data, nil
	}

	m := make(map[ComponentID]interface{})
	for k, v := range data.(map[string]interface{}) {
		id, err := NewComponentIDFromString(k)
		if err != nil {
			return nil, err
		}
		if _, ok := m[id]; ok {
			return nil, fmt.Errorf("duplicate name %q after trimming spaces %v", k, id)
		}
		m[id] = v
	}
	return m, nil
}
