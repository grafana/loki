// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package xprocessor // import "go.opentelemetry.io/collector/processor/xprocessor"

import (
	"context"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer/xconsumer"
	"go.opentelemetry.io/collector/internal/componentalias"
	"go.opentelemetry.io/collector/pipeline"
	"go.opentelemetry.io/collector/processor"
)

// Factory is a component.Factory interface for processors.
//
// This interface cannot be directly implemented. Implementations must
// use the NewFactory to implement it.
type Factory interface {
	processor.Factory

	// CreateProfiles creates a Profiles processor based on this config.
	// If the processor type does not support tracing or if the config is not valid,
	// an error will be returned instead.
	CreateProfiles(ctx context.Context, set processor.Settings, cfg component.Config, next xconsumer.Profiles) (Profiles, error)

	// ProfilesStability gets the stability level of the Profiles processor.
	ProfilesStability() component.StabilityLevel
}

// Profiles is a processor that can consume profiles.
type Profiles interface {
	component.Component
	xconsumer.Profiles
}

// CreateProfilesFunc is the equivalent of Factory.CreateProfiles().
type CreateProfilesFunc func(context.Context, processor.Settings, component.Config, xconsumer.Profiles) (Profiles, error)

// FactoryOption apply changes to ReceiverOptions.
type FactoryOption interface {
	// applyOption applies the option.
	applyOption(o *factory)
}

// factoryOptionFunc is a FactoryOption created through a function.
type factoryOptionFunc func(*factory)

func (f factoryOptionFunc) applyOption(o *factory) {
	f(o)
}

type factory struct {
	processor.Factory
	componentalias.TypeAliasHolder
	opts                   []processor.FactoryOption
	createProfilesFunc     CreateProfilesFunc
	profilesStabilityLevel component.StabilityLevel
}

func (f *factory) ProfilesStability() component.StabilityLevel {
	return f.profilesStabilityLevel
}

func (f *factory) CreateProfiles(ctx context.Context, set processor.Settings, cfg component.Config, next xconsumer.Profiles) (Profiles, error) {
	if f.createProfilesFunc == nil {
		return nil, pipeline.ErrSignalNotSupported
	}
	if err := componentalias.ValidateComponentType(f.Factory, set.ID); err != nil {
		return nil, err
	}
	return f.createProfilesFunc(ctx, set, cfg, next)
}

// WithTraces overrides the default "error not supported" implementation for CreateTraces and the default "undefined" stability level.
func WithTraces(createTraces processor.CreateTracesFunc, sl component.StabilityLevel) FactoryOption {
	return factoryOptionFunc(func(o *factory) {
		o.opts = append(o.opts, processor.WithTraces(createTraces, sl))
	})
}

// WithMetrics overrides the default "error not supported" implementation for CreateMetrics and the default "undefined" stability level.
func WithMetrics(createMetrics processor.CreateMetricsFunc, sl component.StabilityLevel) FactoryOption {
	return factoryOptionFunc(func(o *factory) {
		o.opts = append(o.opts, processor.WithMetrics(createMetrics, sl))
	})
}

// WithLogs overrides the default "error not supported" implementation for CreateLogs and the default "undefined" stability level.
func WithLogs(createLogs processor.CreateLogsFunc, sl component.StabilityLevel) FactoryOption {
	return factoryOptionFunc(func(o *factory) {
		o.opts = append(o.opts, processor.WithLogs(createLogs, sl))
	})
}

// WithProfiles overrides the default "error not supported" implementation for CreateProfiles and the default "undefined" stability level.
func WithProfiles(createProfiles CreateProfilesFunc, sl component.StabilityLevel) FactoryOption {
	return factoryOptionFunc(func(o *factory) {
		o.profilesStabilityLevel = sl
		o.createProfilesFunc = createProfiles
	})
}

// WithDeprecatedTypeAlias configures a deprecated type alias for the processor. Only one alias is supported per processor.
// When the alias is used in configuration, a deprecation warning is automatically logged.
func WithDeprecatedTypeAlias(alias component.Type) FactoryOption {
	return factoryOptionFunc(func(o *factory) {
		o.SetDeprecatedAlias(alias)
	})
}

// NewFactory creates a wrapped processor.Factory with experimental capabilities.
func NewFactory(cfgType component.Type, createDefaultConfig component.CreateDefaultConfigFunc, options ...FactoryOption) Factory {
	f := &factory{TypeAliasHolder: componentalias.NewTypeAliasHolder()}
	for _, opt := range options {
		opt.applyOption(f)
	}
	f.Factory = processor.NewFactory(cfgType, createDefaultConfig, f.opts...)
	f.Factory.(componentalias.TypeAliasHolder).SetDeprecatedAlias(f.DeprecatedAlias())
	return f
}
