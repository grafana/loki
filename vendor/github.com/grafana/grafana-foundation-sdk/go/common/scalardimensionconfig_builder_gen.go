// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[ScalarDimensionConfig] = (*ScalarDimensionConfigBuilder)(nil)

type ScalarDimensionConfigBuilder struct {
	internal *ScalarDimensionConfig
	errors   map[string]cog.BuildErrors
}

func NewScalarDimensionConfigBuilder() *ScalarDimensionConfigBuilder {
	resource := &ScalarDimensionConfig{}
	builder := &ScalarDimensionConfigBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *ScalarDimensionConfigBuilder) Build() (ScalarDimensionConfig, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("ScalarDimensionConfig", err)...)
	}

	if len(errs) != 0 {
		return ScalarDimensionConfig{}, errs
	}

	return *builder.internal, nil
}

func (builder *ScalarDimensionConfigBuilder) Min(min float64) *ScalarDimensionConfigBuilder {
	builder.internal.Min = min

	return builder
}

func (builder *ScalarDimensionConfigBuilder) Max(max float64) *ScalarDimensionConfigBuilder {
	builder.internal.Max = max

	return builder
}

func (builder *ScalarDimensionConfigBuilder) Fixed(fixed float64) *ScalarDimensionConfigBuilder {
	builder.internal.Fixed = &fixed

	return builder
}

// fixed: T -- will be added by each element
func (builder *ScalarDimensionConfigBuilder) Field(field string) *ScalarDimensionConfigBuilder {
	builder.internal.Field = &field

	return builder
}

func (builder *ScalarDimensionConfigBuilder) Mode(mode ScalarDimensionMode) *ScalarDimensionConfigBuilder {
	builder.internal.Mode = &mode

	return builder
}

func (builder *ScalarDimensionConfigBuilder) applyDefaults() {
}
