// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[OptionsWithTextFormatting] = (*OptionsWithTextFormattingBuilder)(nil)

// TODO docs
type OptionsWithTextFormattingBuilder struct {
	internal *OptionsWithTextFormatting
	errors   map[string]cog.BuildErrors
}

func NewOptionsWithTextFormattingBuilder() *OptionsWithTextFormattingBuilder {
	resource := &OptionsWithTextFormatting{}
	builder := &OptionsWithTextFormattingBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *OptionsWithTextFormattingBuilder) Build() (OptionsWithTextFormatting, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("OptionsWithTextFormatting", err)...)
	}

	if len(errs) != 0 {
		return OptionsWithTextFormatting{}, errs
	}

	return *builder.internal, nil
}

func (builder *OptionsWithTextFormattingBuilder) Text(text cog.Builder[VizTextDisplayOptions]) *OptionsWithTextFormattingBuilder {
	textResource, err := text.Build()
	if err != nil {
		builder.errors["text"] = err.(cog.BuildErrors)
		return builder
	}
	builder.internal.Text = &textResource

	return builder
}

func (builder *OptionsWithTextFormattingBuilder) applyDefaults() {
}
