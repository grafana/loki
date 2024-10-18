// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[TableColoredBackgroundCellOptions] = (*TableColoredBackgroundCellOptionsBuilder)(nil)

// Colored background cell options
type TableColoredBackgroundCellOptionsBuilder struct {
	internal *TableColoredBackgroundCellOptions
	errors   map[string]cog.BuildErrors
}

func NewTableColoredBackgroundCellOptionsBuilder() *TableColoredBackgroundCellOptionsBuilder {
	resource := &TableColoredBackgroundCellOptions{}
	builder := &TableColoredBackgroundCellOptionsBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()
	builder.internal.Type = "color-background"

	return builder
}

func (builder *TableColoredBackgroundCellOptionsBuilder) Build() (TableColoredBackgroundCellOptions, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("TableColoredBackgroundCellOptions", err)...)
	}

	if len(errs) != 0 {
		return TableColoredBackgroundCellOptions{}, errs
	}

	return *builder.internal, nil
}

func (builder *TableColoredBackgroundCellOptionsBuilder) Mode(mode TableCellBackgroundDisplayMode) *TableColoredBackgroundCellOptionsBuilder {
	builder.internal.Mode = &mode

	return builder
}

func (builder *TableColoredBackgroundCellOptionsBuilder) ApplyToRow(applyToRow bool) *TableColoredBackgroundCellOptionsBuilder {
	builder.internal.ApplyToRow = &applyToRow

	return builder
}

func (builder *TableColoredBackgroundCellOptionsBuilder) WrapText(wrapText bool) *TableColoredBackgroundCellOptionsBuilder {
	builder.internal.WrapText = &wrapText

	return builder
}

func (builder *TableColoredBackgroundCellOptionsBuilder) applyDefaults() {
}
