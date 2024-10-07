// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[HideableFieldConfig] = (*HideableFieldConfigBuilder)(nil)

// TODO docs
type HideableFieldConfigBuilder struct {
	internal *HideableFieldConfig
	errors   map[string]cog.BuildErrors
}

func NewHideableFieldConfigBuilder() *HideableFieldConfigBuilder {
	resource := &HideableFieldConfig{}
	builder := &HideableFieldConfigBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *HideableFieldConfigBuilder) Build() (HideableFieldConfig, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("HideableFieldConfig", err)...)
	}

	if len(errs) != 0 {
		return HideableFieldConfig{}, errs
	}

	return *builder.internal, nil
}

func (builder *HideableFieldConfigBuilder) HideFrom(hideFrom cog.Builder[HideSeriesConfig]) *HideableFieldConfigBuilder {
	hideFromResource, err := hideFrom.Build()
	if err != nil {
		builder.errors["hideFrom"] = err.(cog.BuildErrors)
		return builder
	}
	builder.internal.HideFrom = &hideFromResource

	return builder
}

func (builder *HideableFieldConfigBuilder) applyDefaults() {
}
