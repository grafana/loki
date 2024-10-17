// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[HideSeriesConfig] = (*HideSeriesConfigBuilder)(nil)

// TODO docs
type HideSeriesConfigBuilder struct {
	internal *HideSeriesConfig
	errors   map[string]cog.BuildErrors
}

func NewHideSeriesConfigBuilder() *HideSeriesConfigBuilder {
	resource := &HideSeriesConfig{}
	builder := &HideSeriesConfigBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *HideSeriesConfigBuilder) Build() (HideSeriesConfig, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("HideSeriesConfig", err)...)
	}

	if len(errs) != 0 {
		return HideSeriesConfig{}, errs
	}

	return *builder.internal, nil
}

func (builder *HideSeriesConfigBuilder) Tooltip(tooltip bool) *HideSeriesConfigBuilder {
	builder.internal.Tooltip = tooltip

	return builder
}

func (builder *HideSeriesConfigBuilder) Legend(legend bool) *HideSeriesConfigBuilder {
	builder.internal.Legend = legend

	return builder
}

func (builder *HideSeriesConfigBuilder) Viz(viz bool) *HideSeriesConfigBuilder {
	builder.internal.Viz = viz

	return builder
}

func (builder *HideSeriesConfigBuilder) applyDefaults() {
}
