// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[LineConfig] = (*LineConfigBuilder)(nil)

// TODO docs
type LineConfigBuilder struct {
	internal *LineConfig
	errors   map[string]cog.BuildErrors
}

func NewLineConfigBuilder() *LineConfigBuilder {
	resource := &LineConfig{}
	builder := &LineConfigBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *LineConfigBuilder) Build() (LineConfig, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("LineConfig", err)...)
	}

	if len(errs) != 0 {
		return LineConfig{}, errs
	}

	return *builder.internal, nil
}

func (builder *LineConfigBuilder) LineColor(lineColor string) *LineConfigBuilder {
	builder.internal.LineColor = &lineColor

	return builder
}

func (builder *LineConfigBuilder) LineWidth(lineWidth float64) *LineConfigBuilder {
	builder.internal.LineWidth = &lineWidth

	return builder
}

func (builder *LineConfigBuilder) LineInterpolation(lineInterpolation LineInterpolation) *LineConfigBuilder {
	builder.internal.LineInterpolation = &lineInterpolation

	return builder
}

func (builder *LineConfigBuilder) LineStyle(lineStyle cog.Builder[LineStyle]) *LineConfigBuilder {
	lineStyleResource, err := lineStyle.Build()
	if err != nil {
		builder.errors["lineStyle"] = err.(cog.BuildErrors)
		return builder
	}
	builder.internal.LineStyle = &lineStyleResource

	return builder
}

// Indicate if null values should be treated as gaps or connected.
// When the value is a number, it represents the maximum delta in the
// X axis that should be considered connected.  For timeseries, this is milliseconds
func (builder *LineConfigBuilder) SpanNulls(spanNulls BoolOrFloat64) *LineConfigBuilder {
	builder.internal.SpanNulls = &spanNulls

	return builder
}

func (builder *LineConfigBuilder) applyDefaults() {
}
