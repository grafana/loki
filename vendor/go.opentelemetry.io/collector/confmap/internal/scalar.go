// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal // import "go.opentelemetry.io/collector/confmap/internal"

import (
	"errors"
	"reflect"

	"github.com/go-viper/mapstructure/v2"
)

// ErrValueNotApplicable is returned when a value provided to a
// ScalarUnmarshaler or ScalarMarshaler is not handled by the interface's method
// call and should instead be handled by another mapstructure hook.
//
// Typically this should be used when a non-scalar value is received and should
// instead be handled by the regular Unmarshaler or Marshaler interfaces.
var ErrValueNotApplicable = errors.New("the provided value is not applicable for handling by this type")

// ScalarValue provides access to a scalar configuration value and allows
// calling back into the confmap decoding/encoding machinery.
//
// Experimental: This interface is experimental, and behavior may change without
// backward compatibility until this notice is removed.
type ScalarValue interface {
	GetRaw() any

	Unmarshal(result any, opts ...UnmarshalOption) error

	Marshal(value any, opts ...MarshalOption) error

	// Seal the interface so it can't be implemented outside this package.
	_unexported()
}

// ScalarUnmarshaler is an interface which may be implemented by wrapper types
// to customize their behavior when the type under the wrapper is a scalar
// value.
//
// This should be used for types like `Wrapper[T]` where T is a scalar type, and
// the wrapper type needs to implement custom logic for unmarshaling from a
// scalar value (e.g. `5` for `Wrapper[int]`) into the wrapper type (e.g.
// `Wrapper[int]{inner: 5}`).
//
// Experimental: This interface is experimental, and behavior may change without
// backward compatibility until this notice is removed.
type ScalarUnmarshaler interface {
	// UnmarshalScalar allows a type to unmarshal itself from a scalar value.
	UnmarshalScalar(ScalarValue) error
}

// ScalarMarshaler is an interface which may be implemented by wrapper types
// to customize their behavior when the type under the wrapper is a scalar value.
//
// Experimental: This interface is experimental, and behavior may change without
// backward compatibility until this notice is removed.
type ScalarMarshaler interface {
	// MarshalScalar allows a type to marshal itself to a scalar value.
	MarshalScalar(ScalarValue) error
}

var _ ScalarValue = (*scalarValue)(nil)

type scalarValue struct {
	val any
}

func (s *scalarValue) GetRaw() any {
	return s.val
}

func (s *scalarValue) Unmarshal(result any, opts ...UnmarshalOption) error {
	settings := ApplyUnmarshalOptions(nil, opts)
	return Decode(s.val, result, *settings, false)
}

func (s *scalarValue) Marshal(value any, opts ...MarshalOption) error {
	if value == nil {
		// If we receive a nil value, we encode it as nil map, which is how
		// mapstructure represents null values. We still pass it through the
		// confmap machinery to give it the same handling as other values.
		value = map[string]any(nil)
	}

	settings := ApplyMarshalOptions(nil, opts)
	data, err := Encode(value, *settings)
	if err != nil {
		return err
	}
	s.val = data

	return nil
}

func (s *scalarValue) _unexported() {}

// scalarUnmarshalerHookFunc handles decoding for types implementing the
// ScalarUnmarshaler interface.
func scalarUnmarshalerHookFunc() mapstructure.DecodeHookFuncValue {
	return safeWrapDecodeHookFunc(func(from, to reflect.Value) (any, error) {
		if !to.CanAddr() {
			return from.Interface(), nil
		}

		toPtr := to.Addr().Interface()

		unmarshaler, ok := toPtr.(ScalarUnmarshaler)
		if !ok {
			return from.Interface(), nil
		}

		val := from.Interface()

		if from.Kind() == reflect.Map {
			// Non-nil maps shouldn't be handled by this hook as they indicate
			// struct-typed input.
			if !from.IsNil() {
				return from.Interface(), nil
			}

			// Simplify nil value handling by making the value an any-typed nil
			// value instead of a nil map.
			val = nil
		}

		sv := &scalarValue{val: val}

		if err := unmarshaler.UnmarshalScalar(sv); err != nil {
			if errors.Is(err, ErrValueNotApplicable) {
				return from.Interface(), nil
			}

			return nil, err
		}

		return unmarshaler, nil
	})
}

// scalarMarshalerHookFunc handles encoding for types implementing the
// ScalarMarshaler interface.
func scalarMarshalerHookFunc() mapstructure.DecodeHookFuncValue {
	return safeWrapDecodeHookFunc(func(from, _ reflect.Value) (any, error) {
		marshaler, ok := from.Interface().(ScalarMarshaler)
		if !ok {
			return from.Interface(), nil
		}

		res := &scalarValue{}
		if err := marshaler.MarshalScalar(res); err != nil {
			if errors.Is(err, ErrValueNotApplicable) {
				return from.Interface(), nil
			}

			return nil, err
		}

		return res.GetRaw(), nil
	})
}
