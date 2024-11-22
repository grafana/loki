// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[TextDimensionConfig] = (*TextDimensionConfigBuilder)(nil)

type TextDimensionConfigBuilder struct {
	internal *TextDimensionConfig
	errors   map[string]cog.BuildErrors
}

func NewTextDimensionConfigBuilder() *TextDimensionConfigBuilder {
	resource := &TextDimensionConfig{}
	builder := &TextDimensionConfigBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *TextDimensionConfigBuilder) Build() (TextDimensionConfig, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("TextDimensionConfig", err)...)
	}

	if len(errs) != 0 {
		return TextDimensionConfig{}, errs
	}

	return *builder.internal, nil
}

func (builder *TextDimensionConfigBuilder) Mode(mode TextDimensionMode) *TextDimensionConfigBuilder {
	builder.internal.Mode = mode

	return builder
}

// fixed: T -- will be added by each element
func (builder *TextDimensionConfigBuilder) Field(field string) *TextDimensionConfigBuilder {
	builder.internal.Field = &field

	return builder
}

func (builder *TextDimensionConfigBuilder) Fixed(fixed string) *TextDimensionConfigBuilder {
	builder.internal.Fixed = &fixed

	return builder
}

func (builder *TextDimensionConfigBuilder) applyDefaults() {
}
