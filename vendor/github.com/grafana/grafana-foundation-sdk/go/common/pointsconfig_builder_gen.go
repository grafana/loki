// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[PointsConfig] = (*PointsConfigBuilder)(nil)

// TODO docs
type PointsConfigBuilder struct {
	internal *PointsConfig
	errors   map[string]cog.BuildErrors
}

func NewPointsConfigBuilder() *PointsConfigBuilder {
	resource := &PointsConfig{}
	builder := &PointsConfigBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *PointsConfigBuilder) Build() (PointsConfig, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("PointsConfig", err)...)
	}

	if len(errs) != 0 {
		return PointsConfig{}, errs
	}

	return *builder.internal, nil
}

func (builder *PointsConfigBuilder) ShowPoints(showPoints VisibilityMode) *PointsConfigBuilder {
	builder.internal.ShowPoints = &showPoints

	return builder
}

func (builder *PointsConfigBuilder) PointSize(pointSize float64) *PointsConfigBuilder {
	builder.internal.PointSize = &pointSize

	return builder
}

func (builder *PointsConfigBuilder) PointColor(pointColor string) *PointsConfigBuilder {
	builder.internal.PointColor = &pointColor

	return builder
}

func (builder *PointsConfigBuilder) PointSymbol(pointSymbol string) *PointsConfigBuilder {
	builder.internal.PointSymbol = &pointSymbol

	return builder
}

func (builder *PointsConfigBuilder) applyDefaults() {
}
