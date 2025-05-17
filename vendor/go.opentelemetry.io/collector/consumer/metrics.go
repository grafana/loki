// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package consumer // import "go.opentelemetry.io/collector/consumer"

import (
	"context"

	"go.opentelemetry.io/collector/consumer/internal"
	"go.opentelemetry.io/collector/pdata/pmetric"
)

// Metrics is an interface that receives pmetric.Metrics, processes it
// as needed, and sends it to the next processing node if any or to the destination.
type Metrics interface {
	internal.BaseConsumer
	// ConsumeMetrics processes the metrics. After the function returns, the metrics are no longer accessible,
	// and accessing them is considered undefined behavior.
	ConsumeMetrics(ctx context.Context, md pmetric.Metrics) error
}

// ConsumeMetricsFunc is a helper function that is similar to ConsumeMetrics.
type ConsumeMetricsFunc func(ctx context.Context, md pmetric.Metrics) error

// ConsumeMetrics calls f(ctx, md).
func (f ConsumeMetricsFunc) ConsumeMetrics(ctx context.Context, md pmetric.Metrics) error {
	return f(ctx, md)
}

type baseMetrics struct {
	*internal.BaseImpl
	ConsumeMetricsFunc
}

// NewMetrics returns a Metrics configured with the provided options.
func NewMetrics(consume ConsumeMetricsFunc, options ...Option) (Metrics, error) {
	if consume == nil {
		return nil, errNilFunc
	}
	return &baseMetrics{
		BaseImpl:           internal.NewBaseImpl(options...),
		ConsumeMetricsFunc: consume,
	}, nil
}
