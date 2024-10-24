// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[TableSparklineCellOptions] = (*TableSparklineCellOptionsBuilder)(nil)

// Sparkline cell options
type TableSparklineCellOptionsBuilder struct {
	internal *TableSparklineCellOptions
	errors   map[string]cog.BuildErrors
}

func NewTableSparklineCellOptionsBuilder() *TableSparklineCellOptionsBuilder {
	resource := &TableSparklineCellOptions{}
	builder := &TableSparklineCellOptionsBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()
	builder.internal.Type = "sparkline"

	return builder
}

func (builder *TableSparklineCellOptionsBuilder) Build() (TableSparklineCellOptions, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("TableSparklineCellOptions", err)...)
	}

	if len(errs) != 0 {
		return TableSparklineCellOptions{}, errs
	}

	return *builder.internal, nil
}

func (builder *TableSparklineCellOptionsBuilder) DrawStyle(drawStyle GraphDrawStyle) *TableSparklineCellOptionsBuilder {
	builder.internal.DrawStyle = &drawStyle

	return builder
}

func (builder *TableSparklineCellOptionsBuilder) GradientMode(gradientMode GraphGradientMode) *TableSparklineCellOptionsBuilder {
	builder.internal.GradientMode = &gradientMode

	return builder
}

func (builder *TableSparklineCellOptionsBuilder) ThresholdsStyle(thresholdsStyle cog.Builder[GraphThresholdsStyleConfig]) *TableSparklineCellOptionsBuilder {
	thresholdsStyleResource, err := thresholdsStyle.Build()
	if err != nil {
		builder.errors["thresholdsStyle"] = err.(cog.BuildErrors)
		return builder
	}
	builder.internal.ThresholdsStyle = &thresholdsStyleResource

	return builder
}

func (builder *TableSparklineCellOptionsBuilder) Transform(transform GraphTransform) *TableSparklineCellOptionsBuilder {
	builder.internal.Transform = &transform

	return builder
}

func (builder *TableSparklineCellOptionsBuilder) LineColor(lineColor string) *TableSparklineCellOptionsBuilder {
	builder.internal.LineColor = &lineColor

	return builder
}

func (builder *TableSparklineCellOptionsBuilder) LineWidth(lineWidth float64) *TableSparklineCellOptionsBuilder {
	builder.internal.LineWidth = &lineWidth

	return builder
}

func (builder *TableSparklineCellOptionsBuilder) LineInterpolation(lineInterpolation LineInterpolation) *TableSparklineCellOptionsBuilder {
	builder.internal.LineInterpolation = &lineInterpolation

	return builder
}

func (builder *TableSparklineCellOptionsBuilder) LineStyle(lineStyle cog.Builder[LineStyle]) *TableSparklineCellOptionsBuilder {
	lineStyleResource, err := lineStyle.Build()
	if err != nil {
		builder.errors["lineStyle"] = err.(cog.BuildErrors)
		return builder
	}
	builder.internal.LineStyle = &lineStyleResource

	return builder
}

func (builder *TableSparklineCellOptionsBuilder) FillColor(fillColor string) *TableSparklineCellOptionsBuilder {
	builder.internal.FillColor = &fillColor

	return builder
}

func (builder *TableSparklineCellOptionsBuilder) FillOpacity(fillOpacity float64) *TableSparklineCellOptionsBuilder {
	builder.internal.FillOpacity = &fillOpacity

	return builder
}

func (builder *TableSparklineCellOptionsBuilder) ShowPoints(showPoints VisibilityMode) *TableSparklineCellOptionsBuilder {
	builder.internal.ShowPoints = &showPoints

	return builder
}

func (builder *TableSparklineCellOptionsBuilder) PointSize(pointSize float64) *TableSparklineCellOptionsBuilder {
	builder.internal.PointSize = &pointSize

	return builder
}

func (builder *TableSparklineCellOptionsBuilder) PointColor(pointColor string) *TableSparklineCellOptionsBuilder {
	builder.internal.PointColor = &pointColor

	return builder
}

func (builder *TableSparklineCellOptionsBuilder) AxisPlacement(axisPlacement AxisPlacement) *TableSparklineCellOptionsBuilder {
	builder.internal.AxisPlacement = &axisPlacement

	return builder
}

func (builder *TableSparklineCellOptionsBuilder) AxisColorMode(axisColorMode AxisColorMode) *TableSparklineCellOptionsBuilder {
	builder.internal.AxisColorMode = &axisColorMode

	return builder
}

func (builder *TableSparklineCellOptionsBuilder) AxisLabel(axisLabel string) *TableSparklineCellOptionsBuilder {
	builder.internal.AxisLabel = &axisLabel

	return builder
}

func (builder *TableSparklineCellOptionsBuilder) AxisWidth(axisWidth float64) *TableSparklineCellOptionsBuilder {
	builder.internal.AxisWidth = &axisWidth

	return builder
}

func (builder *TableSparklineCellOptionsBuilder) AxisSoftMin(axisSoftMin float64) *TableSparklineCellOptionsBuilder {
	builder.internal.AxisSoftMin = &axisSoftMin

	return builder
}

func (builder *TableSparklineCellOptionsBuilder) AxisSoftMax(axisSoftMax float64) *TableSparklineCellOptionsBuilder {
	builder.internal.AxisSoftMax = &axisSoftMax

	return builder
}

func (builder *TableSparklineCellOptionsBuilder) AxisGridShow(axisGridShow bool) *TableSparklineCellOptionsBuilder {
	builder.internal.AxisGridShow = &axisGridShow

	return builder
}

func (builder *TableSparklineCellOptionsBuilder) ScaleDistribution(scaleDistribution cog.Builder[ScaleDistributionConfig]) *TableSparklineCellOptionsBuilder {
	scaleDistributionResource, err := scaleDistribution.Build()
	if err != nil {
		builder.errors["scaleDistribution"] = err.(cog.BuildErrors)
		return builder
	}
	builder.internal.ScaleDistribution = &scaleDistributionResource

	return builder
}

func (builder *TableSparklineCellOptionsBuilder) AxisCenteredZero(axisCenteredZero bool) *TableSparklineCellOptionsBuilder {
	builder.internal.AxisCenteredZero = &axisCenteredZero

	return builder
}

func (builder *TableSparklineCellOptionsBuilder) BarAlignment(barAlignment BarAlignment) *TableSparklineCellOptionsBuilder {
	builder.internal.BarAlignment = &barAlignment

	return builder
}

func (builder *TableSparklineCellOptionsBuilder) BarWidthFactor(barWidthFactor float64) *TableSparklineCellOptionsBuilder {
	builder.internal.BarWidthFactor = &barWidthFactor

	return builder
}

func (builder *TableSparklineCellOptionsBuilder) Stacking(stacking cog.Builder[StackingConfig]) *TableSparklineCellOptionsBuilder {
	stackingResource, err := stacking.Build()
	if err != nil {
		builder.errors["stacking"] = err.(cog.BuildErrors)
		return builder
	}
	builder.internal.Stacking = &stackingResource

	return builder
}

func (builder *TableSparklineCellOptionsBuilder) HideFrom(hideFrom cog.Builder[HideSeriesConfig]) *TableSparklineCellOptionsBuilder {
	hideFromResource, err := hideFrom.Build()
	if err != nil {
		builder.errors["hideFrom"] = err.(cog.BuildErrors)
		return builder
	}
	builder.internal.HideFrom = &hideFromResource

	return builder
}

func (builder *TableSparklineCellOptionsBuilder) HideValue(hideValue bool) *TableSparklineCellOptionsBuilder {
	builder.internal.HideValue = &hideValue

	return builder
}

func (builder *TableSparklineCellOptionsBuilder) InsertNulls(insertNulls BoolOrFloat64) *TableSparklineCellOptionsBuilder {
	builder.internal.InsertNulls = &insertNulls

	return builder
}

// Indicate if null values should be treated as gaps or connected.
// When the value is a number, it represents the maximum delta in the
// X axis that should be considered connected.  For timeseries, this is milliseconds
func (builder *TableSparklineCellOptionsBuilder) SpanNulls(spanNulls BoolOrFloat64) *TableSparklineCellOptionsBuilder {
	builder.internal.SpanNulls = &spanNulls

	return builder
}

func (builder *TableSparklineCellOptionsBuilder) FillBelowTo(fillBelowTo string) *TableSparklineCellOptionsBuilder {
	builder.internal.FillBelowTo = &fillBelowTo

	return builder
}

func (builder *TableSparklineCellOptionsBuilder) PointSymbol(pointSymbol string) *TableSparklineCellOptionsBuilder {
	builder.internal.PointSymbol = &pointSymbol

	return builder
}

func (builder *TableSparklineCellOptionsBuilder) AxisBorderShow(axisBorderShow bool) *TableSparklineCellOptionsBuilder {
	builder.internal.AxisBorderShow = &axisBorderShow

	return builder
}

func (builder *TableSparklineCellOptionsBuilder) BarMaxWidth(barMaxWidth float64) *TableSparklineCellOptionsBuilder {
	builder.internal.BarMaxWidth = &barMaxWidth

	return builder
}

func (builder *TableSparklineCellOptionsBuilder) applyDefaults() {
}
