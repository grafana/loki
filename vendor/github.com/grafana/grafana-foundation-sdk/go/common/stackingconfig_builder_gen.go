// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[StackingConfig] = (*StackingConfigBuilder)(nil)

// TODO docs
type StackingConfigBuilder struct {
	internal *StackingConfig
	errors   map[string]cog.BuildErrors
}

func NewStackingConfigBuilder() *StackingConfigBuilder {
	resource := &StackingConfig{}
	builder := &StackingConfigBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *StackingConfigBuilder) Build() (StackingConfig, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("StackingConfig", err)...)
	}

	if len(errs) != 0 {
		return StackingConfig{}, errs
	}

	return *builder.internal, nil
}

func (builder *StackingConfigBuilder) Mode(mode StackingMode) *StackingConfigBuilder {
	builder.internal.Mode = &mode

	return builder
}

func (builder *StackingConfigBuilder) Group(group string) *StackingConfigBuilder {
	builder.internal.Group = &group

	return builder
}

func (builder *StackingConfigBuilder) applyDefaults() {
}
