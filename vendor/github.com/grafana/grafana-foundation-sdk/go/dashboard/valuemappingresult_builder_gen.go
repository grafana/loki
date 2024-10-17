// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package dashboard

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[ValueMappingResult] = (*ValueMappingResultBuilder)(nil)

// Result used as replacement with text and color when the value matches
type ValueMappingResultBuilder struct {
	internal *ValueMappingResult
	errors   map[string]cog.BuildErrors
}

func NewValueMappingResultBuilder() *ValueMappingResultBuilder {
	resource := &ValueMappingResult{}
	builder := &ValueMappingResultBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *ValueMappingResultBuilder) Build() (ValueMappingResult, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("ValueMappingResult", err)...)
	}

	if len(errs) != 0 {
		return ValueMappingResult{}, errs
	}

	return *builder.internal, nil
}

// Text to display when the value matches
func (builder *ValueMappingResultBuilder) Text(text string) *ValueMappingResultBuilder {
	builder.internal.Text = &text

	return builder
}

// Text to use when the value matches
func (builder *ValueMappingResultBuilder) Color(color string) *ValueMappingResultBuilder {
	builder.internal.Color = &color

	return builder
}

// Icon to display when the value matches. Only specific visualizations.
func (builder *ValueMappingResultBuilder) Icon(icon string) *ValueMappingResultBuilder {
	builder.internal.Icon = &icon

	return builder
}

// Position in the mapping array. Only used internally.
func (builder *ValueMappingResultBuilder) Index(index int32) *ValueMappingResultBuilder {
	builder.internal.Index = &index

	return builder
}

func (builder *ValueMappingResultBuilder) applyDefaults() {
}
