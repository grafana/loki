// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[ColorDimensionConfig] = (*ColorDimensionConfigBuilder)(nil)

type ColorDimensionConfigBuilder struct {
	internal *ColorDimensionConfig
	errors   map[string]cog.BuildErrors
}

func NewColorDimensionConfigBuilder() *ColorDimensionConfigBuilder {
	resource := &ColorDimensionConfig{}
	builder := &ColorDimensionConfigBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *ColorDimensionConfigBuilder) Build() (ColorDimensionConfig, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("ColorDimensionConfig", err)...)
	}

	if len(errs) != 0 {
		return ColorDimensionConfig{}, errs
	}

	return *builder.internal, nil
}

// color value
func (builder *ColorDimensionConfigBuilder) Fixed(fixed string) *ColorDimensionConfigBuilder {
	builder.internal.Fixed = &fixed

	return builder
}

// fixed: T -- will be added by each element
func (builder *ColorDimensionConfigBuilder) Field(field string) *ColorDimensionConfigBuilder {
	builder.internal.Field = &field

	return builder
}

func (builder *ColorDimensionConfigBuilder) applyDefaults() {
}
