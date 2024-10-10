// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[StackableFieldConfig] = (*StackableFieldConfigBuilder)(nil)

// TODO docs
type StackableFieldConfigBuilder struct {
	internal *StackableFieldConfig
	errors   map[string]cog.BuildErrors
}

func NewStackableFieldConfigBuilder() *StackableFieldConfigBuilder {
	resource := &StackableFieldConfig{}
	builder := &StackableFieldConfigBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *StackableFieldConfigBuilder) Build() (StackableFieldConfig, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("StackableFieldConfig", err)...)
	}

	if len(errs) != 0 {
		return StackableFieldConfig{}, errs
	}

	return *builder.internal, nil
}

func (builder *StackableFieldConfigBuilder) Stacking(stacking cog.Builder[StackingConfig]) *StackableFieldConfigBuilder {
	stackingResource, err := stacking.Build()
	if err != nil {
		builder.errors["stacking"] = err.(cog.BuildErrors)
		return builder
	}
	builder.internal.Stacking = &stackingResource

	return builder
}

func (builder *StackableFieldConfigBuilder) applyDefaults() {
}
