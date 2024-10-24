// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[TableBarGaugeCellOptions] = (*TableBarGaugeCellOptionsBuilder)(nil)

// Gauge cell options
type TableBarGaugeCellOptionsBuilder struct {
	internal *TableBarGaugeCellOptions
	errors   map[string]cog.BuildErrors
}

func NewTableBarGaugeCellOptionsBuilder() *TableBarGaugeCellOptionsBuilder {
	resource := &TableBarGaugeCellOptions{}
	builder := &TableBarGaugeCellOptionsBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()
	builder.internal.Type = "gauge"

	return builder
}

func (builder *TableBarGaugeCellOptionsBuilder) Build() (TableBarGaugeCellOptions, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("TableBarGaugeCellOptions", err)...)
	}

	if len(errs) != 0 {
		return TableBarGaugeCellOptions{}, errs
	}

	return *builder.internal, nil
}

func (builder *TableBarGaugeCellOptionsBuilder) Mode(mode BarGaugeDisplayMode) *TableBarGaugeCellOptionsBuilder {
	builder.internal.Mode = &mode

	return builder
}

func (builder *TableBarGaugeCellOptionsBuilder) ValueDisplayMode(valueDisplayMode BarGaugeValueMode) *TableBarGaugeCellOptionsBuilder {
	builder.internal.ValueDisplayMode = &valueDisplayMode

	return builder
}

func (builder *TableBarGaugeCellOptionsBuilder) applyDefaults() {
}
