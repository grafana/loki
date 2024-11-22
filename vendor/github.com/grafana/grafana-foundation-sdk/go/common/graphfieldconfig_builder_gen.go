// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[GraphFieldConfig] = (*GraphFieldConfigBuilder)(nil)

// TODO docs
type GraphFieldConfigBuilder struct {
	internal *GraphFieldConfig
	errors   map[string]cog.BuildErrors
}

func NewGraphFieldConfigBuilder() *GraphFieldConfigBuilder {
	resource := &GraphFieldConfig{}
	builder := &GraphFieldConfigBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *GraphFieldConfigBuilder) Build() (GraphFieldConfig, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("GraphFieldConfig", err)...)
	}

	if len(errs) != 0 {
		return GraphFieldConfig{}, errs
	}

	return *builder.internal, nil
}

func (builder *GraphFieldConfigBuilder) DrawStyle(drawStyle GraphDrawStyle) *GraphFieldConfigBuilder {
	builder.internal.DrawStyle = &drawStyle

	return builder
}

func (builder *GraphFieldConfigBuilder) GradientMode(gradientMode GraphGradientMode) *GraphFieldConfigBuilder {
	builder.internal.GradientMode = &gradientMode

	return builder
}

func (builder *GraphFieldConfigBuilder) ThresholdsStyle(thresholdsStyle cog.Builder[GraphThresholdsStyleConfig]) *GraphFieldConfigBuilder {
	thresholdsStyleResource, err := thresholdsStyle.Build()
	if err != nil {
		builder.errors["thresholdsStyle"] = err.(cog.BuildErrors)
		return builder
	}
	builder.internal.ThresholdsStyle = &thresholdsStyleResource

	return builder
}

func (builder *GraphFieldConfigBuilder) Transform(transform GraphTransform) *GraphFieldConfigBuilder {
	builder.internal.Transform = &transform

	return builder
}

func (builder *GraphFieldConfigBuilder) LineColor(lineColor string) *GraphFieldConfigBuilder {
	builder.internal.LineColor = &lineColor

	return builder
}

func (builder *GraphFieldConfigBuilder) LineWidth(lineWidth float64) *GraphFieldConfigBuilder {
	builder.internal.LineWidth = &lineWidth

	return builder
}

func (builder *GraphFieldConfigBuilder) LineInterpolation(lineInterpolation LineInterpolation) *GraphFieldConfigBuilder {
	builder.internal.LineInterpolation = &lineInterpolation

	return builder
}

func (builder *GraphFieldConfigBuilder) LineStyle(lineStyle cog.Builder[LineStyle]) *GraphFieldConfigBuilder {
	lineStyleResource, err := lineStyle.Build()
	if err != nil {
		builder.errors["lineStyle"] = err.(cog.BuildErrors)
		return builder
	}
	builder.internal.LineStyle = &lineStyleResource

	return builder
}

func (builder *GraphFieldConfigBuilder) FillColor(fillColor string) *GraphFieldConfigBuilder {
	builder.internal.FillColor = &fillColor

	return builder
}

func (builder *GraphFieldConfigBuilder) FillOpacity(fillOpacity float64) *GraphFieldConfigBuilder {
	builder.internal.FillOpacity = &fillOpacity

	return builder
}

func (builder *GraphFieldConfigBuilder) ShowPoints(showPoints VisibilityMode) *GraphFieldConfigBuilder {
	builder.internal.ShowPoints = &showPoints

	return builder
}

func (builder *GraphFieldConfigBuilder) PointSize(pointSize float64) *GraphFieldConfigBuilder {
	builder.internal.PointSize = &pointSize

	return builder
}

func (builder *GraphFieldConfigBuilder) PointColor(pointColor string) *GraphFieldConfigBuilder {
	builder.internal.PointColor = &pointColor

	return builder
}

func (builder *GraphFieldConfigBuilder) AxisPlacement(axisPlacement AxisPlacement) *GraphFieldConfigBuilder {
	builder.internal.AxisPlacement = &axisPlacement

	return builder
}

func (builder *GraphFieldConfigBuilder) AxisColorMode(axisColorMode AxisColorMode) *GraphFieldConfigBuilder {
	builder.internal.AxisColorMode = &axisColorMode

	return builder
}

func (builder *GraphFieldConfigBuilder) AxisLabel(axisLabel string) *GraphFieldConfigBuilder {
	builder.internal.AxisLabel = &axisLabel

	return builder
}

func (builder *GraphFieldConfigBuilder) AxisWidth(axisWidth float64) *GraphFieldConfigBuilder {
	builder.internal.AxisWidth = &axisWidth

	return builder
}

func (builder *GraphFieldConfigBuilder) AxisSoftMin(axisSoftMin float64) *GraphFieldConfigBuilder {
	builder.internal.AxisSoftMin = &axisSoftMin

	return builder
}

func (builder *GraphFieldConfigBuilder) AxisSoftMax(axisSoftMax float64) *GraphFieldConfigBuilder {
	builder.internal.AxisSoftMax = &axisSoftMax

	return builder
}

func (builder *GraphFieldConfigBuilder) AxisGridShow(axisGridShow bool) *GraphFieldConfigBuilder {
	builder.internal.AxisGridShow = &axisGridShow

	return builder
}

func (builder *GraphFieldConfigBuilder) ScaleDistribution(scaleDistribution cog.Builder[ScaleDistributionConfig]) *GraphFieldConfigBuilder {
	scaleDistributionResource, err := scaleDistribution.Build()
	if err != nil {
		builder.errors["scaleDistribution"] = err.(cog.BuildErrors)
		return builder
	}
	builder.internal.ScaleDistribution = &scaleDistributionResource

	return builder
}

func (builder *GraphFieldConfigBuilder) AxisCenteredZero(axisCenteredZero bool) *GraphFieldConfigBuilder {
	builder.internal.AxisCenteredZero = &axisCenteredZero

	return builder
}

func (builder *GraphFieldConfigBuilder) BarAlignment(barAlignment BarAlignment) *GraphFieldConfigBuilder {
	builder.internal.BarAlignment = &barAlignment

	return builder
}

func (builder *GraphFieldConfigBuilder) BarWidthFactor(barWidthFactor float64) *GraphFieldConfigBuilder {
	builder.internal.BarWidthFactor = &barWidthFactor

	return builder
}

func (builder *GraphFieldConfigBuilder) Stacking(stacking cog.Builder[StackingConfig]) *GraphFieldConfigBuilder {
	stackingResource, err := stacking.Build()
	if err != nil {
		builder.errors["stacking"] = err.(cog.BuildErrors)
		return builder
	}
	builder.internal.Stacking = &stackingResource

	return builder
}

func (builder *GraphFieldConfigBuilder) HideFrom(hideFrom cog.Builder[HideSeriesConfig]) *GraphFieldConfigBuilder {
	hideFromResource, err := hideFrom.Build()
	if err != nil {
		builder.errors["hideFrom"] = err.(cog.BuildErrors)
		return builder
	}
	builder.internal.HideFrom = &hideFromResource

	return builder
}

func (builder *GraphFieldConfigBuilder) InsertNulls(insertNulls BoolOrFloat64) *GraphFieldConfigBuilder {
	builder.internal.InsertNulls = &insertNulls

	return builder
}

// Indicate if null values should be treated as gaps or connected.
// When the value is a number, it represents the maximum delta in the
// X axis that should be considered connected.  For timeseries, this is milliseconds
func (builder *GraphFieldConfigBuilder) SpanNulls(spanNulls BoolOrFloat64) *GraphFieldConfigBuilder {
	builder.internal.SpanNulls = &spanNulls

	return builder
}

func (builder *GraphFieldConfigBuilder) FillBelowTo(fillBelowTo string) *GraphFieldConfigBuilder {
	builder.internal.FillBelowTo = &fillBelowTo

	return builder
}

func (builder *GraphFieldConfigBuilder) PointSymbol(pointSymbol string) *GraphFieldConfigBuilder {
	builder.internal.PointSymbol = &pointSymbol

	return builder
}

func (builder *GraphFieldConfigBuilder) AxisBorderShow(axisBorderShow bool) *GraphFieldConfigBuilder {
	builder.internal.AxisBorderShow = &axisBorderShow

	return builder
}

func (builder *GraphFieldConfigBuilder) BarMaxWidth(barMaxWidth float64) *GraphFieldConfigBuilder {
	builder.internal.BarMaxWidth = &barMaxWidth

	return builder
}

func (builder *GraphFieldConfigBuilder) applyDefaults() {
}
