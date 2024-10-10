// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[OptionsWithLegend] = (*OptionsWithLegendBuilder)(nil)

// TODO docs
type OptionsWithLegendBuilder struct {
	internal *OptionsWithLegend
	errors   map[string]cog.BuildErrors
}

func NewOptionsWithLegendBuilder() *OptionsWithLegendBuilder {
	resource := &OptionsWithLegend{}
	builder := &OptionsWithLegendBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *OptionsWithLegendBuilder) Build() (OptionsWithLegend, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("OptionsWithLegend", err)...)
	}

	if len(errs) != 0 {
		return OptionsWithLegend{}, errs
	}

	return *builder.internal, nil
}

func (builder *OptionsWithLegendBuilder) Legend(legend cog.Builder[VizLegendOptions]) *OptionsWithLegendBuilder {
	legendResource, err := legend.Build()
	if err != nil {
		builder.errors["legend"] = err.(cog.BuildErrors)
		return builder
	}
	builder.internal.Legend = legendResource

	return builder
}

func (builder *OptionsWithLegendBuilder) applyDefaults() {
}
