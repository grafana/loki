// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:generate mdatagen metadata.yaml

package confmap // import "go.opentelemetry.io/collector/confmap"

import (
	"go.opentelemetry.io/collector/confmap/internal"
)

// KeyDelimiter is used as the default key delimiter in the default koanf instance.
var KeyDelimiter = internal.KeyDelimiter

// MapstructureTag is the struct field tag used to record marshaling/unmarshaling settings.
// See https://pkg.go.dev/github.com/go-viper/mapstructure/v2 for supported values.
var MapstructureTag = internal.MapstructureTag

// New creates a new empty confmap.Conf instance.
func New() *Conf {
	return internal.New()
}

// NewFromStringMap creates a confmap.Conf from a map[string]any.
func NewFromStringMap(data map[string]any) *Conf {
	return internal.NewFromStringMap(data)
}

// Conf represents the raw configuration map for the OpenTelemetry Collector.
// The confmap.Conf can be unmarshalled into the Collector's config using the "service" package.
type Conf = internal.Conf

type UnmarshalOption = internal.UnmarshalOption

// WithIgnoreUnused sets an option to ignore errors if existing
// keys in the original Conf were unused in the decoding process
// (extra keys).
func WithIgnoreUnused() UnmarshalOption {
	return internal.WithIgnoreUnused()
}

type MarshalOption = internal.MarshalOption

// Unmarshaler interface may be implemented by types to customize their behavior when being unmarshaled from a Conf.
// Only types with struct or pointer to struct kind are supported.
type Unmarshaler = internal.Unmarshaler

// Marshaler defines an optional interface for custom configuration marshaling.
// A configuration struct can implement this interface to override the default
// marshaling.
type Marshaler = internal.Marshaler

// ScalarValue provides access to a scalar configuration value and allows
// calling back into the confmap decoding/encoding machinery.
//
// This interface is only provided to methods used for [ScalarUnmarshaler] and
// [ScalarMarshaler] implementations and cannot be implemented by types outside
// the confmap package.
//
// Experimental: This interface is experimental, and behavior may change without
// backward compatibility until this notice is removed.
type ScalarValue = internal.ScalarValue

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
type ScalarUnmarshaler = internal.ScalarUnmarshaler

// ScalarMarshaler is an interface which may be implemented by wrapper types
// to customize their behavior when the type under the wrapper is a scalar value.
//
// Experimental: This interface is experimental, and behavior may change without
// backward compatibility until this notice is removed.
type ScalarMarshaler = internal.ScalarMarshaler

// ErrValueNotApplicable is returned when a value provided to a
// ScalarUnmarshaler or ScalarMarshaler is not handled by the interface's method
// call and should instead be handled by another mapstructure hook.
//
// Typically this should be used when a non-scalar value is received and should
// instead be handled by the regular Unmarshaler or Marshaler interfaces.
var ErrValueNotApplicable = internal.ErrValueNotApplicable
