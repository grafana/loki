// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[VizLegendOptions] = (*VizLegendOptionsBuilder)(nil)

// TODO docs
type VizLegendOptionsBuilder struct {
	internal *VizLegendOptions
	errors   map[string]cog.BuildErrors
}

func NewVizLegendOptionsBuilder() *VizLegendOptionsBuilder {
	resource := &VizLegendOptions{}
	builder := &VizLegendOptionsBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *VizLegendOptionsBuilder) Build() (VizLegendOptions, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("VizLegendOptions", err)...)
	}

	if len(errs) != 0 {
		return VizLegendOptions{}, errs
	}

	return *builder.internal, nil
}

func (builder *VizLegendOptionsBuilder) DisplayMode(displayMode LegendDisplayMode) *VizLegendOptionsBuilder {
	builder.internal.DisplayMode = displayMode

	return builder
}

func (builder *VizLegendOptionsBuilder) Placement(placement LegendPlacement) *VizLegendOptionsBuilder {
	builder.internal.Placement = placement

	return builder
}

func (builder *VizLegendOptionsBuilder) ShowLegend(showLegend bool) *VizLegendOptionsBuilder {
	builder.internal.ShowLegend = showLegend

	return builder
}

func (builder *VizLegendOptionsBuilder) AsTable(asTable bool) *VizLegendOptionsBuilder {
	builder.internal.AsTable = &asTable

	return builder
}

func (builder *VizLegendOptionsBuilder) IsVisible(isVisible bool) *VizLegendOptionsBuilder {
	builder.internal.IsVisible = &isVisible

	return builder
}

func (builder *VizLegendOptionsBuilder) SortBy(sortBy string) *VizLegendOptionsBuilder {
	builder.internal.SortBy = &sortBy

	return builder
}

func (builder *VizLegendOptionsBuilder) SortDesc(sortDesc bool) *VizLegendOptionsBuilder {
	builder.internal.SortDesc = &sortDesc

	return builder
}

func (builder *VizLegendOptionsBuilder) Width(width float64) *VizLegendOptionsBuilder {
	builder.internal.Width = &width

	return builder
}

func (builder *VizLegendOptionsBuilder) Calcs(calcs []string) *VizLegendOptionsBuilder {
	builder.internal.Calcs = calcs

	return builder
}

func (builder *VizLegendOptionsBuilder) applyDefaults() {
	builder.DisplayMode("list")
	builder.Placement("bottom")
	builder.Calcs([]string{})
}
