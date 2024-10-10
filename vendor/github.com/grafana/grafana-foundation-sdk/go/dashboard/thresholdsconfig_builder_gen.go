// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package dashboard

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[ThresholdsConfig] = (*ThresholdsConfigBuilder)(nil)

// Thresholds configuration for the panel
type ThresholdsConfigBuilder struct {
	internal *ThresholdsConfig
	errors   map[string]cog.BuildErrors
}

func NewThresholdsConfigBuilder() *ThresholdsConfigBuilder {
	resource := &ThresholdsConfig{}
	builder := &ThresholdsConfigBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *ThresholdsConfigBuilder) Build() (ThresholdsConfig, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("ThresholdsConfig", err)...)
	}

	if len(errs) != 0 {
		return ThresholdsConfig{}, errs
	}

	return *builder.internal, nil
}

// Thresholds mode.
func (builder *ThresholdsConfigBuilder) Mode(mode ThresholdsMode) *ThresholdsConfigBuilder {
	builder.internal.Mode = mode

	return builder
}

// Must be sorted by 'value', first value is always -Infinity
func (builder *ThresholdsConfigBuilder) Steps(steps []Threshold) *ThresholdsConfigBuilder {
	builder.internal.Steps = steps

	return builder
}

func (builder *ThresholdsConfigBuilder) applyDefaults() {
}
