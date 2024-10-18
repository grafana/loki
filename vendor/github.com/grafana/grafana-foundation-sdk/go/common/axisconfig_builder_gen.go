// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[AxisConfig] = (*AxisConfigBuilder)(nil)

// TODO docs
type AxisConfigBuilder struct {
	internal *AxisConfig
	errors   map[string]cog.BuildErrors
}

func NewAxisConfigBuilder() *AxisConfigBuilder {
	resource := &AxisConfig{}
	builder := &AxisConfigBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *AxisConfigBuilder) Build() (AxisConfig, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("AxisConfig", err)...)
	}

	if len(errs) != 0 {
		return AxisConfig{}, errs
	}

	return *builder.internal, nil
}

func (builder *AxisConfigBuilder) AxisPlacement(axisPlacement AxisPlacement) *AxisConfigBuilder {
	builder.internal.AxisPlacement = &axisPlacement

	return builder
}

func (builder *AxisConfigBuilder) AxisColorMode(axisColorMode AxisColorMode) *AxisConfigBuilder {
	builder.internal.AxisColorMode = &axisColorMode

	return builder
}

func (builder *AxisConfigBuilder) AxisLabel(axisLabel string) *AxisConfigBuilder {
	builder.internal.AxisLabel = &axisLabel

	return builder
}

func (builder *AxisConfigBuilder) AxisWidth(axisWidth float64) *AxisConfigBuilder {
	builder.internal.AxisWidth = &axisWidth

	return builder
}

func (builder *AxisConfigBuilder) AxisSoftMin(axisSoftMin float64) *AxisConfigBuilder {
	builder.internal.AxisSoftMin = &axisSoftMin

	return builder
}

func (builder *AxisConfigBuilder) AxisSoftMax(axisSoftMax float64) *AxisConfigBuilder {
	builder.internal.AxisSoftMax = &axisSoftMax

	return builder
}

func (builder *AxisConfigBuilder) AxisGridShow(axisGridShow bool) *AxisConfigBuilder {
	builder.internal.AxisGridShow = &axisGridShow

	return builder
}

func (builder *AxisConfigBuilder) ScaleDistribution(scaleDistribution cog.Builder[ScaleDistributionConfig]) *AxisConfigBuilder {
	scaleDistributionResource, err := scaleDistribution.Build()
	if err != nil {
		builder.errors["scaleDistribution"] = err.(cog.BuildErrors)
		return builder
	}
	builder.internal.ScaleDistribution = &scaleDistributionResource

	return builder
}

func (builder *AxisConfigBuilder) AxisCenteredZero(axisCenteredZero bool) *AxisConfigBuilder {
	builder.internal.AxisCenteredZero = &axisCenteredZero

	return builder
}

func (builder *AxisConfigBuilder) AxisBorderShow(axisBorderShow bool) *AxisConfigBuilder {
	builder.internal.AxisBorderShow = &axisBorderShow

	return builder
}

func (builder *AxisConfigBuilder) applyDefaults() {
}
