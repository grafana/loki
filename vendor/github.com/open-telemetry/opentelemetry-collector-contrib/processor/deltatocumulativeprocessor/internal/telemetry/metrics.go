// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package telemetry // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/deltatocumulativeprocessor/internal/telemetry"

import (
	"context"
	"errors"
	"reflect"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/deltatocumulativeprocessor/internal/metadata"
)

func New(set component.TelemetrySettings) (Metrics, error) {
	zero := func() int { return -1 }
	m := Metrics{
		tracked: &zero,
	}

	telb, err := metadata.NewTelemetryBuilder(set)
	if err != nil {
		return Metrics{}, err
	}
	err = telb.RegisterDeltatocumulativeStreamsTrackedCallback(func(_ context.Context, observer metric.Int64Observer) error {
		observer.Observe(int64((*m.tracked)()))
		return nil
	})
	if err != nil {
		return Metrics{}, err
	}
	m.TelemetryBuilder = telb

	return m, nil
}

type Metrics struct {
	*metadata.TelemetryBuilder

	tracked *func() int
}

func (m *Metrics) Datapoints() Counter {
	return Counter{Int64Counter: m.DeltatocumulativeDatapoints}
}

func (m *Metrics) WithTracked(streams func() int) {
	*m.tracked = streams
}

func Error(msg string) attribute.KeyValue {
	return attribute.String("error", msg)
}

func Cause(err error) attribute.KeyValue {
	for {
		uw := errors.Unwrap(err)
		if uw == nil {
			break
		}
		err = uw
	}

	return Error(reflect.TypeOf(err).String())
}

type Counter struct{ metric.Int64Counter }

func (c Counter) Inc(ctx context.Context, attrs ...attribute.KeyValue) {
	c.Add(ctx, 1, metric.WithAttributes(attrs...))
}
