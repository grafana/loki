// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[ScaleDimensionConfig] = (*ScaleDimensionConfigBuilder)(nil)

type ScaleDimensionConfigBuilder struct {
	internal *ScaleDimensionConfig
	errors   map[string]cog.BuildErrors
}

func NewScaleDimensionConfigBuilder() *ScaleDimensionConfigBuilder {
	resource := &ScaleDimensionConfig{}
	builder := &ScaleDimensionConfigBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *ScaleDimensionConfigBuilder) Build() (ScaleDimensionConfig, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("ScaleDimensionConfig", err)...)
	}

	if len(errs) != 0 {
		return ScaleDimensionConfig{}, errs
	}

	return *builder.internal, nil
}

func (builder *ScaleDimensionConfigBuilder) Min(min float64) *ScaleDimensionConfigBuilder {
	builder.internal.Min = min

	return builder
}

func (builder *ScaleDimensionConfigBuilder) Max(max float64) *ScaleDimensionConfigBuilder {
	builder.internal.Max = max

	return builder
}

func (builder *ScaleDimensionConfigBuilder) Fixed(fixed float64) *ScaleDimensionConfigBuilder {
	builder.internal.Fixed = &fixed

	return builder
}

// fixed: T -- will be added by each element
func (builder *ScaleDimensionConfigBuilder) Field(field string) *ScaleDimensionConfigBuilder {
	builder.internal.Field = &field

	return builder
}

// | *"linear"
func (builder *ScaleDimensionConfigBuilder) Mode(mode ScaleDimensionMode) *ScaleDimensionConfigBuilder {
	builder.internal.Mode = &mode

	return builder
}

func (builder *ScaleDimensionConfigBuilder) applyDefaults() {
}
