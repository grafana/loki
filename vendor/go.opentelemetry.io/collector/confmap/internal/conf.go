// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal // import "go.opentelemetry.io/collector/confmap/internal"

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/knadh/koanf/maps"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/v2"

	encoder "go.opentelemetry.io/collector/confmap/internal/mapstructure"
)

const (
	// KeyDelimiter is used as the default key delimiter in the default koanf instance.
	KeyDelimiter = "::"
)

// Conf represents the raw configuration map for the OpenTelemetry Collector.
// The confmap.Conf can be unmarshalled into the Collector's config using the "service" package.
type Conf struct {
	k *koanf.Koanf
	// If true, upon unmarshaling do not call the Unmarshal function on the struct
	// if it implements Unmarshaler and is the top-level struct.
	// This avoids running into an infinite recursion where Unmarshaler.Unmarshal and
	// Conf.Unmarshal would call each other.
	skipTopLevelUnmarshaler bool
	// isNil is true if this Conf was created from a nil field, as opposed to an empty map.
	// AllKeys must return an empty slice if this is true.
	isNil bool
}

// New creates a new empty confmap.Conf instance.
func New() *Conf {
	return &Conf{k: koanf.New(KeyDelimiter), isNil: false}
}

// NewFromStringMap creates a confmap.Conf from a map[string]any.
func NewFromStringMap(data map[string]any) *Conf {
	p := New()
	if data == nil {
		p.isNil = true
	} else {
		// Cannot return error because the koanf instance is empty.
		_ = p.k.Load(confmap.Provider(data, KeyDelimiter), nil)
	}
	return p
}

// Unmarshal unmarshalls the config into a struct using the given options.
// Tags on the fields of the structure must be properly set.
func (l *Conf) Unmarshal(result any, opts ...UnmarshalOption) error {
	set := UnmarshalOptions{}
	for _, opt := range opts {
		opt.apply(&set)
	}
	return Decode(l.toStringMapWithExpand(), result, set, l.skipTopLevelUnmarshaler)
}

// Marshal encodes the config and merges it into the Conf.
func (l *Conf) Marshal(rawVal any, opts ...MarshalOption) error {
	set := MarshalOptions{}
	for _, opt := range opts {
		opt.apply(&set)
	}
	enc := encoder.New(EncoderConfig(rawVal, set))
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

// AllKeys returns all keys holding a value, regardless of where they are set.
// Nested keys are returned with a KeyDelimiter separator.
func (l *Conf) AllKeys() []string {
	return l.k.Keys()
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
	if EnableMergeAppendOption.IsEnabled() {
		// only use MergeAppend when EnableMergeAppendOption featuregate is enabled.
		return l.mergeAppend(in)
	}
	l.isNil = l.isNil && in.isNil
	return l.k.Merge(in.k)
}

// Delete a path from the Conf.
// If the path exists, deletes it and returns true.
// If the path does not exist, does nothing and returns false.
func (l *Conf) Delete(key string) bool {
	wasSet := l.IsSet(key)
	l.k.Delete(key)
	return wasSet
}

// mergeAppend merges the input given configuration into the existing config.
// Note that the given map may be modified.
// Additionally, mergeAppend performs deduplication when merging lists.
// For example, if listA = [extension1, extension2] and listB = [extension1, extension3],
// the resulting list will be [extension1, extension2, extension3].
func (l *Conf) mergeAppend(in *Conf) error {
	err := l.k.Load(confmap.Provider(in.ToStringMap(), ""), nil, koanf.WithMergeFunc(mergeAppend))
	if err != nil {
		return err
	}
	l.isNil = l.isNil && in.isNil
	return nil
}

// Sub returns new Conf instance representing a sub-config of this instance.
// It returns an error is the sub-config is not a map[string]any (use Get()), and an empty Map if none exists.
func (l *Conf) Sub(key string) (*Conf, error) {
	// Code inspired by the koanf "Cut" func, but returns an error instead of empty map for unsupported sub-config type.
	data := l.unsanitizedGet(key)
	if data == nil {
		c := New()
		c.isNil = true
		return c, nil
	}

	switch v := data.(type) {
	case map[string]any:
		return NewFromStringMap(v), nil
	case ExpandedValue:
		if m, ok := v.Value.(map[string]any); ok {
			return NewFromStringMap(m), nil
		} else if v.Value == nil {
			// If the value is nil, return a new empty Conf.
			c := New()
			c.isNil = true
			return c, nil
		}
		// override data with the original value to make the error message more informative.
		data = v.Value
	}

	return nil, fmt.Errorf("unexpected sub-config value kind for key:%s value:%v kind:%v", key, data, reflect.TypeOf(data).Kind())
}

func (l *Conf) toStringMapWithExpand() map[string]any {
	if l.isNil {
		return nil
	}
	m := maps.Unflatten(l.k.All(), KeyDelimiter)
	return m
}

// ToStringMap creates a map[string]any from a Conf.
// Values with multiple representations
// are normalized with the YAML parsed representation.
//
// For example, for a Conf created from `foo: ${env:FOO}` and `FOO=123`
// ToStringMap will return `map[string]any{"foo": 123}`.
//
// For any map `m`, `NewFromStringMap(m).ToStringMap() == m`.
// In particular, if the Conf was created from a nil value,
// ToStringMap will return map[string]any(nil).
func (l *Conf) ToStringMap() map[string]any {
	return sanitize(l.toStringMapWithExpand()).(map[string]any)
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
		// If the value is nil, return nil.
		var newSlice []any
		if m == nil {
			return newSlice
		}
		newSlice = make([]any, 0, len(m))
		for _, e := range m {
			newSlice = append(newSlice, sanitizeExpanded(e, useOriginal))
		}
		return newSlice
	case ExpandedValue:
		if useOriginal {
			return m.Original
		}
		return m.Value
	}
	return a
}

type UnsanitizedGetter struct {
	Conf *Conf
}

func (ug *UnsanitizedGetter) UnsanitizedGet(key string) any {
	return ug.Conf.unsanitizedGet(key)
}
