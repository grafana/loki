// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[BarConfig] = (*BarConfigBuilder)(nil)

// TODO docs
type BarConfigBuilder struct {
	internal *BarConfig
	errors   map[string]cog.BuildErrors
}

func NewBarConfigBuilder() *BarConfigBuilder {
	resource := &BarConfig{}
	builder := &BarConfigBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *BarConfigBuilder) Build() (BarConfig, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("BarConfig", err)...)
	}

	if len(errs) != 0 {
		return BarConfig{}, errs
	}

	return *builder.internal, nil
}

func (builder *BarConfigBuilder) BarAlignment(barAlignment BarAlignment) *BarConfigBuilder {
	builder.internal.BarAlignment = &barAlignment

	return builder
}

func (builder *BarConfigBuilder) BarWidthFactor(barWidthFactor float64) *BarConfigBuilder {
	builder.internal.BarWidthFactor = &barWidthFactor

	return builder
}

func (builder *BarConfigBuilder) BarMaxWidth(barMaxWidth float64) *BarConfigBuilder {
	builder.internal.BarMaxWidth = &barMaxWidth

	return builder
}

func (builder *BarConfigBuilder) applyDefaults() {
}
