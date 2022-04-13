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

package component // import "go.opentelemetry.io/collector/component"

import (
	"context"

	"go.opentelemetry.io/collector/component/componenterror"
	"go.opentelemetry.io/collector/config"
	"go.opentelemetry.io/collector/consumer"
)

// Processor defines the common functions that must be implemented by TracesProcessor
// and MetricsProcessor.
type Processor interface {
	Component
}

// TracesProcessor is a processor that can consume traces.
type TracesProcessor interface {
	Processor
	consumer.Traces
}

// MetricsProcessor is a processor that can consume metrics.
type MetricsProcessor interface {
	Processor
	consumer.Metrics
}

// LogsProcessor is a processor that can consume logs.
type LogsProcessor interface {
	Processor
	consumer.Logs
}

// ProcessorCreateSettings is passed to Create* functions in ProcessorFactory.
type ProcessorCreateSettings struct {
	TelemetrySettings

	// BuildInfo can be used by components for informational purposes
	BuildInfo BuildInfo
}

// ProcessorFactory is Factory interface for processors.
//
// This interface cannot be directly implemented. Implementations must
// use the NewProcessorFactory to implement it.
type ProcessorFactory interface {
	Factory

	// CreateDefaultConfig creates the default configuration for the Processor.
	// This method can be called multiple times depending on the pipeline
	// configuration and should not cause side-effects that prevent the creation
	// of multiple instances of the Processor.
	// The object returned by this method needs to pass the checks implemented by
	// 'configtest.CheckConfigStruct'. It is recommended to have these checks in the
	// tests of any implementation of the Factory interface.
	CreateDefaultConfig() config.Processor

	// CreateTracesProcessor creates a trace processor based on this config.
	// If the processor type does not support tracing or if the config is not valid,
	// an error will be returned instead.
	CreateTracesProcessor(
		ctx context.Context,
		set ProcessorCreateSettings,
		cfg config.Processor,
		nextConsumer consumer.Traces,
	) (TracesProcessor, error)

	// CreateMetricsProcessor creates a metrics processor based on this config.
	// If the processor type does not support metrics or if the config is not valid,
	// an error will be returned instead.
	CreateMetricsProcessor(
		ctx context.Context,
		set ProcessorCreateSettings,
		cfg config.Processor,
		nextConsumer consumer.Metrics,
	) (MetricsProcessor, error)

	// CreateLogsProcessor creates a processor based on the config.
	// If the processor type does not support logs or if the config is not valid,
	// an error will be returned instead.
	CreateLogsProcessor(
		ctx context.Context,
		set ProcessorCreateSettings,
		cfg config.Processor,
		nextConsumer consumer.Logs,
	) (LogsProcessor, error)
}

// ProcessorFactoryOption apply changes to ProcessorOptions.
type ProcessorFactoryOption func(o *processorFactory)

// ProcessorCreateDefaultConfigFunc is the equivalent of ProcessorFactory.CreateDefaultConfig().
type ProcessorCreateDefaultConfigFunc func() config.Processor

// CreateDefaultConfig implements ProcessorFactory.CreateDefaultConfig().
func (f ProcessorCreateDefaultConfigFunc) CreateDefaultConfig() config.Processor {
	return f()
}

// CreateTracesProcessorFunc is the equivalent of ProcessorFactory.CreateTracesProcessor().
type CreateTracesProcessorFunc func(context.Context, ProcessorCreateSettings, config.Processor, consumer.Traces) (TracesProcessor, error)

// CreateTracesProcessor implements ProcessorFactory.CreateTracesProcessor().
func (f CreateTracesProcessorFunc) CreateTracesProcessor(
	ctx context.Context,
	set ProcessorCreateSettings,
	cfg config.Processor,
	nextConsumer consumer.Traces) (TracesProcessor, error) {
	if f == nil {
		return nil, componenterror.ErrDataTypeIsNotSupported
	}
	return f(ctx, set, cfg, nextConsumer)
}

// CreateMetricsProcessorFunc is the equivalent of ProcessorFactory.CreateMetricsProcessor().
type CreateMetricsProcessorFunc func(context.Context, ProcessorCreateSettings, config.Processor, consumer.Metrics) (MetricsProcessor, error)

// CreateMetricsProcessor implements ProcessorFactory.CreateMetricsProcessor().
func (f CreateMetricsProcessorFunc) CreateMetricsProcessor(
	ctx context.Context,
	set ProcessorCreateSettings,
	cfg config.Processor,
	nextConsumer consumer.Metrics,
) (MetricsProcessor, error) {
	if f == nil {
		return nil, componenterror.ErrDataTypeIsNotSupported
	}
	return f(ctx, set, cfg, nextConsumer)
}

// CreateLogsProcessorFunc is the equivalent of ProcessorFactory.CreateLogsProcessor().
type CreateLogsProcessorFunc func(context.Context, ProcessorCreateSettings, config.Processor, consumer.Logs) (LogsProcessor, error)

// CreateLogsProcessor implements ProcessorFactory.CreateLogsProcessor().
func (f CreateLogsProcessorFunc) CreateLogsProcessor(
	ctx context.Context,
	set ProcessorCreateSettings,
	cfg config.Processor,
	nextConsumer consumer.Logs,
) (LogsProcessor, error) {
	if f == nil {
		return nil, componenterror.ErrDataTypeIsNotSupported
	}
	return f(ctx, set, cfg, nextConsumer)
}

type processorFactory struct {
	baseFactory
	ProcessorCreateDefaultConfigFunc
	CreateTracesProcessorFunc
	CreateMetricsProcessorFunc
	CreateLogsProcessorFunc
}

// WithTracesProcessor overrides the default "error not supported" implementation for CreateTracesProcessor.
func WithTracesProcessor(createTracesProcessor CreateTracesProcessorFunc) ProcessorFactoryOption {
	return func(o *processorFactory) {
		o.CreateTracesProcessorFunc = createTracesProcessor
	}
}

// WithMetricsProcessor overrides the default "error not supported" implementation for CreateMetricsProcessor.
func WithMetricsProcessor(createMetricsProcessor CreateMetricsProcessorFunc) ProcessorFactoryOption {
	return func(o *processorFactory) {
		o.CreateMetricsProcessorFunc = createMetricsProcessor
	}
}

// WithLogsProcessor overrides the default "error not supported" implementation for CreateLogsProcessor.
func WithLogsProcessor(createLogsProcessor CreateLogsProcessorFunc) ProcessorFactoryOption {
	return func(o *processorFactory) {
		o.CreateLogsProcessorFunc = createLogsProcessor
	}
}

// NewProcessorFactory returns a ProcessorFactory.
func NewProcessorFactory(cfgType config.Type, createDefaultConfig ProcessorCreateDefaultConfigFunc, options ...ProcessorFactoryOption) ProcessorFactory {
	f := &processorFactory{
		baseFactory:                      baseFactory{cfgType: cfgType},
		ProcessorCreateDefaultConfigFunc: createDefaultConfig,
	}
	for _, opt := range options {
		opt(f)
	}
	return f
}
