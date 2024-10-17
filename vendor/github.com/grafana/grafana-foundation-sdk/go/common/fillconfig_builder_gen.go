// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[FillConfig] = (*FillConfigBuilder)(nil)

// TODO docs
type FillConfigBuilder struct {
	internal *FillConfig
	errors   map[string]cog.BuildErrors
}

func NewFillConfigBuilder() *FillConfigBuilder {
	resource := &FillConfig{}
	builder := &FillConfigBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *FillConfigBuilder) Build() (FillConfig, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("FillConfig", err)...)
	}

	if len(errs) != 0 {
		return FillConfig{}, errs
	}

	return *builder.internal, nil
}

func (builder *FillConfigBuilder) FillColor(fillColor string) *FillConfigBuilder {
	builder.internal.FillColor = &fillColor

	return builder
}

func (builder *FillConfigBuilder) FillOpacity(fillOpacity float64) *FillConfigBuilder {
	builder.internal.FillOpacity = &fillOpacity

	return builder
}

func (builder *FillConfigBuilder) FillBelowTo(fillBelowTo string) *FillConfigBuilder {
	builder.internal.FillBelowTo = &fillBelowTo

	return builder
}

func (builder *FillConfigBuilder) applyDefaults() {
}
