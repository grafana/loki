// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[TableFieldOptions] = (*TableFieldOptionsBuilder)(nil)

// Field options for each field within a table (e.g 10, "The String", 64.20, etc.)
// Generally defines alignment, filtering capabilties, display options, etc.
type TableFieldOptionsBuilder struct {
	internal *TableFieldOptions
	errors   map[string]cog.BuildErrors
}

func NewTableFieldOptionsBuilder() *TableFieldOptionsBuilder {
	resource := &TableFieldOptions{}
	builder := &TableFieldOptionsBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *TableFieldOptionsBuilder) Build() (TableFieldOptions, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("TableFieldOptions", err)...)
	}

	if len(errs) != 0 {
		return TableFieldOptions{}, errs
	}

	return *builder.internal, nil
}

func (builder *TableFieldOptionsBuilder) Width(width float64) *TableFieldOptionsBuilder {
	builder.internal.Width = &width

	return builder
}

func (builder *TableFieldOptionsBuilder) MinWidth(minWidth float64) *TableFieldOptionsBuilder {
	builder.internal.MinWidth = &minWidth

	return builder
}

func (builder *TableFieldOptionsBuilder) Align(align FieldTextAlignment) *TableFieldOptionsBuilder {
	builder.internal.Align = align

	return builder
}

// This field is deprecated in favor of using cellOptions
func (builder *TableFieldOptionsBuilder) DisplayMode(displayMode TableCellDisplayMode) *TableFieldOptionsBuilder {
	builder.internal.DisplayMode = &displayMode

	return builder
}

func (builder *TableFieldOptionsBuilder) CellOptions(cellOptions TableCellOptions) *TableFieldOptionsBuilder {
	builder.internal.CellOptions = &cellOptions

	return builder
}

// ?? default is missing or false ??
func (builder *TableFieldOptionsBuilder) Hidden(hidden bool) *TableFieldOptionsBuilder {
	builder.internal.Hidden = &hidden

	return builder
}

func (builder *TableFieldOptionsBuilder) Inspect(inspect bool) *TableFieldOptionsBuilder {
	builder.internal.Inspect = inspect

	return builder
}

func (builder *TableFieldOptionsBuilder) Filterable(filterable bool) *TableFieldOptionsBuilder {
	builder.internal.Filterable = &filterable

	return builder
}

// Hides any header for a column, useful for columns that show some static content or buttons.
func (builder *TableFieldOptionsBuilder) HideHeader(hideHeader bool) *TableFieldOptionsBuilder {
	builder.internal.HideHeader = &hideHeader

	return builder
}

func (builder *TableFieldOptionsBuilder) applyDefaults() {
	builder.Align("auto")
	builder.Inspect(false)
}
