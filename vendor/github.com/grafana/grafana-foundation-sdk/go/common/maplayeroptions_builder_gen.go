// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[MapLayerOptions] = (*MapLayerOptionsBuilder)(nil)

type MapLayerOptionsBuilder struct {
	internal *MapLayerOptions
	errors   map[string]cog.BuildErrors
}

func NewMapLayerOptionsBuilder() *MapLayerOptionsBuilder {
	resource := &MapLayerOptions{}
	builder := &MapLayerOptionsBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *MapLayerOptionsBuilder) Build() (MapLayerOptions, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("MapLayerOptions", err)...)
	}

	if len(errs) != 0 {
		return MapLayerOptions{}, errs
	}

	return *builder.internal, nil
}

func (builder *MapLayerOptionsBuilder) Type(typeArg string) *MapLayerOptionsBuilder {
	builder.internal.Type = typeArg

	return builder
}

// configured unique display name
func (builder *MapLayerOptionsBuilder) Name(name string) *MapLayerOptionsBuilder {
	builder.internal.Name = name

	return builder
}

// Custom options depending on the type
func (builder *MapLayerOptionsBuilder) Config(config any) *MapLayerOptionsBuilder {
	builder.internal.Config = &config

	return builder
}

// Common method to define geometry fields
func (builder *MapLayerOptionsBuilder) Location(location cog.Builder[FrameGeometrySource]) *MapLayerOptionsBuilder {
	locationResource, err := location.Build()
	if err != nil {
		builder.errors["location"] = err.(cog.BuildErrors)
		return builder
	}
	builder.internal.Location = &locationResource

	return builder
}

// Defines a frame MatcherConfig that may filter data for the given layer
func (builder *MapLayerOptionsBuilder) FilterData(filterData any) *MapLayerOptionsBuilder {
	builder.internal.FilterData = &filterData

	return builder
}

// Common properties:
// https://openlayers.org/en/latest/apidoc/module-ol_layer_Base-BaseLayer.html
// Layer opacity (0-1)
func (builder *MapLayerOptionsBuilder) Opacity(opacity int64) *MapLayerOptionsBuilder {
	builder.internal.Opacity = &opacity

	return builder
}

// Check tooltip (defaults to true)
func (builder *MapLayerOptionsBuilder) Tooltip(tooltip bool) *MapLayerOptionsBuilder {
	builder.internal.Tooltip = &tooltip

	return builder
}

func (builder *MapLayerOptionsBuilder) applyDefaults() {
}
