// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[OptionsWithTooltip] = (*OptionsWithTooltipBuilder)(nil)

// TODO docs
type OptionsWithTooltipBuilder struct {
	internal *OptionsWithTooltip
	errors   map[string]cog.BuildErrors
}

func NewOptionsWithTooltipBuilder() *OptionsWithTooltipBuilder {
	resource := &OptionsWithTooltip{}
	builder := &OptionsWithTooltipBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *OptionsWithTooltipBuilder) Build() (OptionsWithTooltip, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("OptionsWithTooltip", err)...)
	}

	if len(errs) != 0 {
		return OptionsWithTooltip{}, errs
	}

	return *builder.internal, nil
}

func (builder *OptionsWithTooltipBuilder) Tooltip(tooltip cog.Builder[VizTooltipOptions]) *OptionsWithTooltipBuilder {
	tooltipResource, err := tooltip.Build()
	if err != nil {
		builder.errors["tooltip"] = err.(cog.BuildErrors)
		return builder
	}
	builder.internal.Tooltip = tooltipResource

	return builder
}

func (builder *OptionsWithTooltipBuilder) applyDefaults() {
}
