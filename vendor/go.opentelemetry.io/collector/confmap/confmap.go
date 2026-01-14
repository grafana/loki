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
