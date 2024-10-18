// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[TableColorTextCellOptions] = (*TableColorTextCellOptionsBuilder)(nil)

// Colored text cell options
type TableColorTextCellOptionsBuilder struct {
	internal *TableColorTextCellOptions
	errors   map[string]cog.BuildErrors
}

func NewTableColorTextCellOptionsBuilder() *TableColorTextCellOptionsBuilder {
	resource := &TableColorTextCellOptions{}
	builder := &TableColorTextCellOptionsBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()
	builder.internal.Type = "color-text"

	return builder
}

func (builder *TableColorTextCellOptionsBuilder) Build() (TableColorTextCellOptions, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("TableColorTextCellOptions", err)...)
	}

	if len(errs) != 0 {
		return TableColorTextCellOptions{}, errs
	}

	return *builder.internal, nil
}

func (builder *TableColorTextCellOptionsBuilder) WrapText(wrapText bool) *TableColorTextCellOptionsBuilder {
	builder.internal.WrapText = &wrapText

	return builder
}

func (builder *TableColorTextCellOptionsBuilder) applyDefaults() {
}
