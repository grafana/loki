// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package componentattribute // import "go.opentelemetry.io/collector/internal/telemetry/componentattribute"

import (
	"slices"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type meterProviderWithAttributes struct {
	metric.MeterProvider
	attrs []attribute.KeyValue
}

// MeterProviderWithAttributes creates a MeterProvider with a new set of injected instrumentation scope attributes.
func MeterProviderWithAttributes(mp metric.MeterProvider, attrs attribute.Set) metric.MeterProvider {
	if mpwa, ok := mp.(meterProviderWithAttributes); ok {
		mp = mpwa.MeterProvider
	}
	return meterProviderWithAttributes{
		MeterProvider: mp,
		attrs:         attrs.ToSlice(),
	}
}

func (mpwa meterProviderWithAttributes) Meter(name string, opts ...metric.MeterOption) metric.Meter {
	conf := metric.NewMeterConfig(opts...)
	attrSet := conf.InstrumentationAttributes()
	// prepend our attributes so they can be overwritten
	newAttrs := append(slices.Clone(mpwa.attrs), attrSet.ToSlice()...)
	// append our attribute set option to overwrite the old one
	opts = append(opts, metric.WithInstrumentationAttributes(newAttrs...))
	return mpwa.MeterProvider.Meter(name, opts...)
}
