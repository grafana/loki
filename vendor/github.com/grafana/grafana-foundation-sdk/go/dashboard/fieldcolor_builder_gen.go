// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package dashboard

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[FieldColor] = (*FieldColorBuilder)(nil)

// Map a field to a color.
type FieldColorBuilder struct {
	internal *FieldColor
	errors   map[string]cog.BuildErrors
}

func NewFieldColorBuilder() *FieldColorBuilder {
	resource := &FieldColor{}
	builder := &FieldColorBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *FieldColorBuilder) Build() (FieldColor, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("FieldColor", err)...)
	}

	if len(errs) != 0 {
		return FieldColor{}, errs
	}

	return *builder.internal, nil
}

// The main color scheme mode.
func (builder *FieldColorBuilder) Mode(mode FieldColorModeId) *FieldColorBuilder {
	builder.internal.Mode = mode

	return builder
}

// The fixed color value for fixed or shades color modes.
func (builder *FieldColorBuilder) FixedColor(fixedColor string) *FieldColorBuilder {
	builder.internal.FixedColor = &fixedColor

	return builder
}

// Some visualizations need to know how to assign a series color from by value color schemes.
func (builder *FieldColorBuilder) SeriesBy(seriesBy FieldColorSeriesByMode) *FieldColorBuilder {
	builder.internal.SeriesBy = &seriesBy

	return builder
}

func (builder *FieldColorBuilder) applyDefaults() {
}
