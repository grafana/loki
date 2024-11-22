// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[VizTooltipOptions] = (*VizTooltipOptionsBuilder)(nil)

// TODO docs
type VizTooltipOptionsBuilder struct {
	internal *VizTooltipOptions
	errors   map[string]cog.BuildErrors
}

func NewVizTooltipOptionsBuilder() *VizTooltipOptionsBuilder {
	resource := &VizTooltipOptions{}
	builder := &VizTooltipOptionsBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *VizTooltipOptionsBuilder) Build() (VizTooltipOptions, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("VizTooltipOptions", err)...)
	}

	if len(errs) != 0 {
		return VizTooltipOptions{}, errs
	}

	return *builder.internal, nil
}

func (builder *VizTooltipOptionsBuilder) Mode(mode TooltipDisplayMode) *VizTooltipOptionsBuilder {
	builder.internal.Mode = mode

	return builder
}

func (builder *VizTooltipOptionsBuilder) Sort(sort SortOrder) *VizTooltipOptionsBuilder {
	builder.internal.Sort = sort

	return builder
}

func (builder *VizTooltipOptionsBuilder) MaxWidth(maxWidth float64) *VizTooltipOptionsBuilder {
	builder.internal.MaxWidth = &maxWidth

	return builder
}

func (builder *VizTooltipOptionsBuilder) MaxHeight(maxHeight float64) *VizTooltipOptionsBuilder {
	builder.internal.MaxHeight = &maxHeight

	return builder
}

func (builder *VizTooltipOptionsBuilder) applyDefaults() {
}
